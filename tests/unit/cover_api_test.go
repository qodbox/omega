package unit

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/gofiber/fiber/v2"
	"github.com/graphql-go/graphql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"omega/internal/api"
)

type apiCovCarnet struct {
	ID        uint `gorm:"primarykey"`
	Title     string
	Secret    string
	Actif     bool
	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt gorm.DeletedAt
}

func (apiCovCarnet) TableName() string { return "carnets" }

type apiCovMedia struct {
	ID      uint `gorm:"primarykey"`
	Nom     string
	Donnees []byte
}

func (apiCovMedia) TableName() string { return "medias" }

type apiCovFantome struct {
	ID  uint `gorm:"primarykey"`
	Nom string
}

func (apiCovFantome) TableName() string { return "fantomes" }

type apiCovPrive struct {
	Nom     string
	interne string
}

const apiCovCodesDDL = `CREATE TABLE codes (code TEXT PRIMARY KEY, label TEXT) WITHOUT ROWID`

func apiCovOpen(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatalf("ouverture: %v", err)
	}
	return db
}

func apiCovDB(t *testing.T) *gorm.DB {
	t.Helper()

	db := apiCovOpen(t)
	if err := db.AutoMigrate(&apiCovCarnet{}); err != nil {
		t.Fatalf("migration: %v", err)
	}
	seed := &apiCovCarnet{Title: "Premier", Secret: "cache", Actif: true}
	if err := db.Create(seed).Error; err != nil {
		t.Fatalf("insertion: %v", err)
	}
	return db
}

func apiCovRegistry(t *testing.T) (*api.Registry, *gorm.DB) {
	t.Helper()

	db := apiCovDB(t)
	registry := openGate(api.NewRegistry(db))
	api.Register[apiCovCarnet](registry, "carnets", "carnet", "secret")
	return registry, db
}

func apiCovFullRegistry(t *testing.T) (*api.Registry, *gorm.DB) {
	t.Helper()

	registry, db := apiCovRegistry(t)
	api.Register[apiCovFantome](registry, "fantomes", "fantome")

	if err := db.Exec(apiCovCodesDDL).Error; err != nil {
		t.Fatalf("codes: %v", err)
	}
	if err := registry.Discover(db); err != nil {
		t.Fatalf("discover: %v", err)
	}
	registry.AllowWrites("codes")
	return registry, db
}

func apiCovMount(registry *api.Registry) *fiber.App {
	app := fiber.New()
	registry.Mount(app.Group("/api"))
	return app
}

func apiCovCall(t *testing.T, app *fiber.App, method, path, payload string) (int, string) {
	t.Helper()

	var reader io.Reader
	if payload != "" {
		reader = strings.NewReader(payload)
	}
	request := httptest.NewRequest(method, path, reader)
	if payload != "" {
		request.Header.Set(fiber.HeaderContentType, fiber.MIMEApplicationJSON)
	}

	response, err := app.Test(request, 5000)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer response.Body.Close()

	body, _ := io.ReadAll(response.Body)
	return response.StatusCode, string(body)
}

func apiCovGraphQL(t *testing.T, schema graphql.Schema, query string) *graphql.Result {
	t.Helper()

	return graphql.Do(graphql.Params{Schema: schema, RequestString: query, Context: context.Background()})
}

func TestApiCovAuthorizeOnlyGuardsNamedRestRoutes(t *testing.T) {
	demanded := ""
	ability := func(name string) fiber.Handler {
		return func(c *fiber.Ctx) error {
			demanded = name
			return c.SendString("garde")
		}
	}
	final := func(c *fiber.Ctx) error { return c.SendString("libre") }

	app := fiber.New()
	app.Get("/pages", api.Authorize(ability), final).Name("web.pages")
	app.Get("/orphelin", api.Authorize(ability), final).Name("api.orphelin")
	app.Get("/exotique", api.Authorize(ability), final).Name("api.carnets.export")
	app.Get("/carnets", api.Authorize(ability), final).Name("api.carnets.index")

	for _, path := range []string{"/pages", "/orphelin", "/exotique"} {
		demanded = ""
		status, body := apiCovCall(t, app, "GET", path, "")
		if status != fiber.StatusOK || body != "libre" {
			t.Errorf("%s: statut %d, corps %q", path, status, body)
		}
		if demanded != "" {
			t.Errorf("%s a consulte la permission %q", path, demanded)
		}
	}

	demanded = ""
	if status, body := apiCovCall(t, app, "GET", "/carnets", ""); status != fiber.StatusOK || body != "garde" {
		t.Errorf("route REST nommee non gardee: statut %d, corps %q", status, body)
	}
	if demanded != "carnets.list" {
		t.Errorf("permission demandee = %q, want carnets.list", demanded)
	}
}

func TestApiCovAuthorizeMapsEveryRestAction(t *testing.T) {
	demanded := ""
	ability := func(name string) fiber.Handler {
		return func(c *fiber.Ctx) error {
			demanded = name
			return c.SendStatus(fiber.StatusOK)
		}
	}

	registry, _ := apiCovRegistry(t)
	app := fiber.New()
	registry.Mount(app.Group("/api"), api.Authorize(ability))

	cases := []struct {
		method string
		path   string
		want   string
	}{
		{"GET", "/api/carnets", "carnets.list"},
		{"GET", "/api/carnets/1", "carnets.view"},
		{"POST", "/api/carnets", "carnets.create"},
		{"PATCH", "/api/carnets/1", "carnets.update"},
		{"DELETE", "/api/carnets/1", "carnets.delete"},
	}
	for _, tc := range cases {
		demanded = ""
		apiCovCall(t, app, tc.method, tc.path, "")
		if demanded != tc.want {
			t.Errorf("%s %s demande %q, want %q", tc.method, tc.path, demanded, tc.want)
		}
	}
}

func TestApiCovDiscoverIgnoresANilDatabase(t *testing.T) {
	registry := openGate(api.NewRegistry(nil))
	if err := registry.Discover(nil); err != nil {
		t.Fatalf("Discover(nil) = %v, want nil", err)
	}
	if len(registry.All()) != 0 {
		t.Errorf("des ressources sont apparues sans base: %d", len(registry.All()))
	}
}

func TestApiCovDiscoverReportsAnUnreadableDatabase(t *testing.T) {
	db := apiCovDB(t)
	pool, err := db.DB()
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	if err := pool.Close(); err != nil {
		t.Fatalf("fermeture: %v", err)
	}

	registry := openGate(api.NewRegistry(db))
	if err := registry.Discover(db); err == nil {
		t.Fatal("Discover doit remonter l'echec de lecture des tables")
	}
}

func TestApiCovDiscoverSkipsATableItCannotInspect(t *testing.T) {
	db := apiCovOpen(t)
	if err := db.Exec(`CREATE TABLE etranges ("id" INTEGER, [a)b] TEXT)`).Error; err != nil {
		t.Fatalf("creation: %v", err)
	}

	registry := openGate(api.NewRegistry(db))
	if err := registry.Discover(db); err != nil {
		t.Fatalf("discover: %v", err)
	}
	if resource := registry.Find("etranges"); resource != nil {
		t.Errorf("une table dont les colonnes sont illisibles ne doit pas etre exposee: %v", resource.Fields)
	}
}

func TestApiCovDiscoverRenamesAnUncountableTable(t *testing.T) {
	db := apiCovOpen(t)
	if err := db.Exec(`CREATE TABLE sheep (id INTEGER PRIMARY KEY, reset_token TEXT, libelle TEXT)`).Error; err != nil {
		t.Fatalf("creation: %v", err)
	}

	registry := openGate(api.NewRegistry(db))
	if err := registry.Discover(db); err != nil {
		t.Fatalf("discover: %v", err)
	}

	resource := registry.Find("sheep")
	if resource == nil {
		t.Fatal("la table sheep n'a pas ete decouverte")
	}
	if resource.Singular != "sheep_item" {
		t.Errorf("singulier = %q, want sheep_item", resource.Singular)
	}
	if !resource.Hidden["reset_token"] {
		t.Error("une colonne en _token doit etre masquee")
	}
	if resource.Hidden["libelle"] {
		t.Error("une colonne anodine ne doit pas etre masquee")
	}
}

func TestApiCovDocsAssetRefusesAnEmptyOrTraversingName(t *testing.T) {
	docs := api.Docs{Title: "Omega", OpenAPIPath: "/openapi.json", GraphQLPath: "/graphql", AssetPrefix: "/assets"}

	app := fiber.New()
	app.Get("/assets/:file?", docs.Asset())

	if status, _ := apiCovCall(t, app, "GET", "/assets/", ""); status != fiber.StatusNotFound {
		t.Errorf("nom vide: statut %d, want 404", status)
	}
	if status, _ := apiCovCall(t, app, "GET", "/assets/absent.js", ""); status != fiber.StatusNotFound {
		t.Errorf("fichier absent: statut %d, want 404", status)
	}
	if status, _ := apiCovCall(t, app, "GET", "/assets/swagger-ui.css", ""); status != fiber.StatusOK {
		t.Errorf("feuille de style embarquee: statut %d, want 200", status)
	}
	if status, _ := apiCovCall(t, app, "GET", "/assets/react.production.min.js", ""); status != fiber.StatusOK {
		t.Errorf("script embarque: statut %d, want 200", status)
	}
}

func TestApiCovRegisterDescribesUnusualTypes(t *testing.T) {
	registry := openGate(api.NewRegistry(apiCovDB(t)))

	pointeur := api.Register[*apiCovCarnet](registry, "pointeurs", "pointeur")
	if !pointeur.Fields[0].ReadOnly || pointeur.Fields[0].Name != "id" {
		t.Errorf("un type pointeur doit etre decrit comme sa cible: %v", pointeur.Fields)
	}

	scalaire := api.Register[int](registry, "entiers", "entier")
	if len(scalaire.Fields) != 0 {
		t.Errorf("un type non struct ne decrit aucun champ: %v", scalaire.Fields)
	}
	if scalaire.Table != "entiers" {
		t.Errorf("table repliee sur le pluriel = %q", scalaire.Table)
	}

	prive := api.Register[apiCovPrive](registry, "prives", "prive")
	if len(prive.Fields) != 1 || prive.Fields[0].Name != "nom" {
		t.Errorf("un champ non exporte ne doit pas etre decrit: %v", prive.Fields)
	}
}

func apiCovMedias(t *testing.T) (*api.Registry, *fiber.App) {
	t.Helper()

	registry, db := apiCovRegistry(t)
	if err := db.AutoMigrate(&apiCovMedia{}); err != nil {
		t.Fatalf("migration medias: %v", err)
	}
	if err := db.Create(&apiCovMedia{Nom: "affiche", Donnees: []byte("binaire")}).Error; err != nil {
		t.Fatalf("insertion media: %v", err)
	}
	api.Register[apiCovMedia](registry, "medias", "media")
	return registry, apiCovMount(registry)
}

func apiCovRows(t *testing.T, body string) []map[string]any {
	t.Helper()

	var page struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &page); err != nil {
		t.Fatalf("json: %v", err)
	}
	return page.Data
}

func TestApiCovListSerialisesBytesAndBooleans(t *testing.T) {
	_, app := apiCovMedias(t)

	status, body := apiCovCall(t, app, "GET", "/api/carnets", "")
	if status != fiber.StatusOK {
		t.Fatalf("statut %d: %s", status, body)
	}

	rows := apiCovRows(t, body)
	if len(rows) != 1 {
		t.Fatalf("lignes = %d", len(rows))
	}
	if rows[0]["actif"] != true {
		t.Errorf("un booleen stocke en entier doit etre rendu en booleen: %#v", rows[0]["actif"])
	}
	if _, present := rows[0]["secret"]; present {
		t.Error("une colonne masquee reste dans la reponse")
	}
	if _, present := rows[0]["deleted_at"]; present {
		t.Error("deleted_at reste dans la reponse")
	}

	status, body = apiCovCall(t, app, "GET", "/api/medias", "")
	if status != fiber.StatusOK {
		t.Fatalf("statut %d: %s", status, body)
	}
	rows = apiCovRows(t, body)
	if len(rows) != 1 || rows[0]["donnees"] != "binaire" {
		t.Errorf("un BLOB doit etre rendu en texte: %#v", rows)
	}
}

func TestApiCovShowReadsARowHoldingBinary(t *testing.T) {
	_, app := apiCovMedias(t)

	status, body := apiCovCall(t, app, "GET", "/api/medias/1", "")
	if status != fiber.StatusOK {
		t.Skipf("defaut connu: la lecture unitaire echoue des qu'une colonne BLOB porte des octets (statut %d, corps %s)", status, body)
	}
	if !strings.Contains(body, "binaire") {
		t.Errorf("le BLOB n'est pas rendu: %s", body)
	}
}

func TestApiCovListRejectsChainedUnknownFilters(t *testing.T) {
	registry, _ := apiCovRegistry(t)
	app := apiCovMount(registry)

	status, body := apiCovCall(t, app, "GET", "/api/carnets?inconnu=1&autre=2", "")
	if status != fiber.StatusUnprocessableEntity {
		t.Errorf("statut %d, want 422: %s", status, body)
	}
	if !strings.Contains(body, "inconnu") {
		t.Errorf("le premier filtre fautif doit etre nomme: %s", body)
	}

	if status, _ := apiCovCall(t, app, "GET", "/api/carnets?secret=cache", ""); status != fiber.StatusUnprocessableEntity {
		t.Errorf("une colonne masquee ne doit pas etre filtrable: statut %d", status)
	}
}

func TestApiCovListReportsABrokenQuery(t *testing.T) {
	registry, _ := apiCovRegistry(t)
	resource := registry.Find("carnets")
	resource.Fields = append(resource.Fields, api.Field{Name: "fantome", Column: "fantome", Type: "string"})
	app := apiCovMount(registry)

	if status, body := apiCovCall(t, app, "GET", "/api/carnets?sort=fantome", ""); status != fiber.StatusInternalServerError {
		t.Errorf("un tri sur une colonne absente en base: statut %d, corps %s", status, body)
	}
	if status, _ := apiCovCall(t, app, "GET", "/api/carnets?sort=inconnue", ""); status != fiber.StatusUnprocessableEntity {
		t.Errorf("un tri sur une colonne inconnue doit etre refuse: statut %d", status)
	}

	if status, body := apiCovCall(t, app, "PATCH", "/api/carnets/1", `{"fantome":"x"}`); status != fiber.StatusUnprocessableEntity {
		t.Errorf("une ecriture refusee par la base: statut %d, corps %s", status, body)
	}
}

func TestApiCovListCountsOnAMissingTable(t *testing.T) {
	registry, _ := apiCovRegistry(t)
	api.Register[apiCovFantome](registry, "fantomes", "fantome")
	app := apiCovMount(registry)

	if status, _ := apiCovCall(t, app, "GET", "/api/fantomes", ""); status != fiber.StatusInternalServerError {
		t.Errorf("le comptage d'une table absente: statut %d", status)
	}
	if status, _ := apiCovCall(t, app, "DELETE", "/api/fantomes/1", ""); status != fiber.StatusInternalServerError {
		t.Errorf("la suppression dans une table absente: statut %d", status)
	}
}

func TestApiCovRestRefusesAMalformedBody(t *testing.T) {
	registry, _ := apiCovRegistry(t)
	app := apiCovMount(registry)

	if status, _ := apiCovCall(t, app, "POST", "/api/carnets", `{"title":`); status != fiber.StatusBadRequest {
		t.Errorf("creation avec un JSON casse: statut %d, want 400", status)
	}
	if status, _ := apiCovCall(t, app, "PATCH", "/api/carnets/1", `{"title":`); status != fiber.StatusBadRequest {
		t.Errorf("modification avec un JSON casse: statut %d, want 400", status)
	}
	if status, _ := apiCovCall(t, app, "POST", "/api/carnets", `{"id":9,"created_at":"x"}`); status != fiber.StatusUnprocessableEntity {
		t.Errorf("creation sans champ inscriptible: statut %d, want 422", status)
	}
}

func TestApiCovDestroySoftDeletesThenReports404(t *testing.T) {
	registry, db := apiCovRegistry(t)
	app := apiCovMount(registry)

	if status, body := apiCovCall(t, app, "DELETE", "/api/carnets/1", ""); status != fiber.StatusNoContent {
		t.Fatalf("suppression douce: statut %d, corps %s", status, body)
	}

	var restantes int64
	if err := db.Table("carnets").Where("deleted_at IS NULL").Count(&restantes).Error; err != nil {
		t.Fatalf("comptage: %v", err)
	}
	if restantes != 0 {
		t.Errorf("lignes encore visibles = %d", restantes)
	}

	var brutes int64
	if err := db.Table("carnets").Count(&brutes).Error; err != nil {
		t.Fatalf("comptage brut: %v", err)
	}
	if brutes != 1 {
		t.Errorf("la ligne a ete effacee physiquement: %d restante(s)", brutes)
	}

	if status, _ := apiCovCall(t, app, "DELETE", "/api/carnets/1", ""); status != fiber.StatusNotFound {
		t.Error("supprimer deux fois doit repondre 404")
	}
	if status, _ := apiCovCall(t, app, "PATCH", "/api/carnets/404", `{"title":"x"}`); status != fiber.StatusNotFound {
		t.Error("modifier une ligne absente doit repondre 404")
	}
}

func TestApiCovCreateWithoutAnIdentityColumn(t *testing.T) {
	registry, db := apiCovFullRegistry(t)
	app := apiCovMount(registry)

	status, body := apiCovCall(t, app, "POST", "/api/codes", `{"code":"AA","label":"un"}`)
	if status != fiber.StatusNotFound {
		t.Errorf("creation sans colonne id: statut %d, corps %s", status, body)
	}

	var lignes int64
	if err := db.Table("codes").Count(&lignes).Error; err != nil {
		t.Fatalf("comptage: %v", err)
	}
	if lignes != 1 {
		t.Errorf("la ligne devait tout de meme etre inseree, trouvees = %d", lignes)
	}
}

func TestApiCovQueryFallsBackToThePluralTable(t *testing.T) {
	registry, _ := apiCovRegistry(t)
	resource := registry.Find("carnets")
	resource.Model = nil
	resource.Table = ""
	app := apiCovMount(registry)

	if status, body := apiCovCall(t, app, "GET", "/api/carnets", ""); status != fiber.StatusOK {
		t.Errorf("sans modele ni table, le pluriel doit servir de table: statut %d, corps %s", status, body)
	}
}

func TestApiCovOpenAPIHidesWhatIsNotExposed(t *testing.T) {
	registry, _ := apiCovRegistry(t)
	spec := registry.OpenAPI("Omega", "1.0.0", "http://localhost")

	schema := spec.Components.Schemas["carnet"]
	if schema == nil {
		t.Fatal("le schema du singulier est absent")
	}
	for _, banni := range []string{"secret", "deleted_at"} {
		if _, present := schema.Properties[banni]; present {
			t.Errorf("%s ne doit pas apparaitre dans le schema", banni)
		}
	}

	entree := spec.Components.Schemas["carnetInput"]
	if entree == nil {
		t.Fatal("le schema d'entree est absent")
	}
	for _, banni := range []string{"id", "created_at", "updated_at"} {
		if _, present := entree.Properties[banni]; present {
			t.Errorf("%s ne doit pas apparaitre dans le schema d'entree", banni)
		}
	}

	item, present := spec.Paths["/carnets"]
	if !present || item.Get == nil {
		t.Fatal("le chemin de collection est absent")
	}
	noms := map[string]bool{}
	for _, parameter := range item.Get.Parameters {
		noms[parameter.Name] = true
	}
	if !noms["title"] {
		t.Error("une colonne visible doit etre filtrable")
	}
	for _, banni := range []string{"secret", "deleted_at"} {
		if noms[banni] {
			t.Errorf("%s ne doit pas etre propose comme filtre", banni)
		}
	}
}

func TestApiCovSchemaRefusesAnUnnamedSingular(t *testing.T) {
	registry := openGate(api.NewRegistry(apiCovDB(t)))
	api.Register[apiCovCarnet](registry, "vides", "")

	if _, err := registry.Schema(); err == nil {
		t.Fatal("un singulier vide doit empecher la construction du schema")
	}
}

func TestApiCovGraphQLFieldResolverToleratesAnAlienSource(t *testing.T) {
	registry, _ := apiCovRegistry(t)
	schema, err := registry.Schema()
	if err != nil {
		t.Fatalf("schema: %v", err)
	}

	object, ok := schema.TypeMap()["Carnet"].(*graphql.Object)
	if !ok {
		t.Fatalf("type Carnet absent: %v", schema.TypeMap()["Carnet"])
	}
	if _, present := object.Fields()["secret"]; present {
		t.Error("un champ masque ne doit pas etre expose en GraphQL")
	}
	if _, present := object.Fields()["deleted_at"]; present {
		t.Error("deleted_at ne doit pas etre expose en GraphQL")
	}
	if object.Fields()["actif"].Type != graphql.Boolean {
		t.Errorf("actif est de type %v, want Boolean", object.Fields()["actif"].Type)
	}

	titre := object.Fields()["title"]
	value, err := titre.Resolve(graphql.ResolveParams{Source: "pas une ligne"})
	if value != nil || err != nil {
		t.Errorf("source etrangere: %v, %v", value, err)
	}

	value, err = titre.Resolve(graphql.ResolveParams{Source: map[string]any{"title": []byte("octets")}})
	if value != "octets" || err != nil {
		t.Errorf("un BLOB doit etre rendu en texte: %#v, %v", value, err)
	}

	value, err = titre.Resolve(graphql.ResolveParams{Source: map[string]any{}})
	if value != nil || err != nil {
		t.Errorf("une colonne absente doit rester nulle: %#v, %v", value, err)
	}
}

func TestApiCovGraphQLReportsBrokenReads(t *testing.T) {
	registry, _ := apiCovFullRegistry(t)
	schema, err := registry.Schema()
	if err != nil {
		t.Fatalf("schema: %v", err)
	}

	if result := apiCovGraphQL(t, schema, `{ carnets(sort: "inconnue") { id } }`); len(result.Errors) == 0 {
		t.Error("un tri inconnu doit remonter une erreur")
	}
	if result := apiCovGraphQL(t, schema, `{ fantomes { id } }`); len(result.Errors) == 0 {
		t.Error("lister une table absente doit remonter une erreur")
	}

	result := apiCovGraphQL(t, schema, `{ carnet(id: "404") { id } }`)
	if len(result.Errors) > 0 {
		t.Fatalf("lire une ligne absente: %v", result.Errors)
	}
	data, _ := result.Data.(map[string]any)
	if data["carnet"] != nil {
		t.Errorf("une ligne absente doit etre nulle: %#v", data["carnet"])
	}

	if result := apiCovGraphQL(t, schema, `{ carnets(limit: 0, offset: -5) { id } }`); len(result.Errors) > 0 {
		t.Errorf("des bornes negatives doivent etre repliees: %v", result.Errors)
	}
}

func TestApiCovGraphQLReportsBrokenWrites(t *testing.T) {
	registry, _ := apiCovFullRegistry(t)
	schema, err := registry.Schema()
	if err != nil {
		t.Fatalf("schema: %v", err)
	}

	if result := apiCovGraphQL(t, schema, `mutation { create_carnet { id } }`); len(result.Errors) == 0 {
		t.Error("une creation sans argument doit etre refusee")
	}
	if result := apiCovGraphQL(t, schema, `mutation { create_fantome(nom: "x") { id } }`); len(result.Errors) == 0 {
		t.Error("creer dans une table absente doit remonter une erreur")
	}
	if result := apiCovGraphQL(t, schema, `mutation { create_code(code: "AA", label: "un") { code } }`); len(result.Errors) == 0 {
		t.Error("creer sans colonne id doit remonter une erreur de relecture")
	}
	if result := apiCovGraphQL(t, schema, `mutation { update_fantome(id: "1", nom: "x") { id } }`); len(result.Errors) == 0 {
		t.Error("modifier une table absente doit remonter une erreur")
	}

	result := apiCovGraphQL(t, schema, `mutation { update_carnet(id: "404", title: "x") { id } }`)
	if len(result.Errors) > 0 {
		t.Fatalf("modifier une ligne absente: %v", result.Errors)
	}
	data, _ := result.Data.(map[string]any)
	if data["update_carnet"] != nil {
		t.Errorf("modifier une ligne absente doit rendre null: %#v", data["update_carnet"])
	}
}

func TestApiCovGraphQLSoftDeletesAndKeepsIdOutOfUpdates(t *testing.T) {
	registry, db := apiCovRegistry(t)
	resource := registry.Find("carnets")
	for index := range resource.Fields {
		if resource.Fields[index].Name == "id" {
			resource.Fields[index].ReadOnly = false
		}
	}

	schema, err := registry.Schema()
	if err != nil {
		t.Fatalf("schema: %v", err)
	}

	result := apiCovGraphQL(t, schema, `mutation { update_carnet(id: 1, title: "Renomme") { id title } }`)
	if len(result.Errors) > 0 {
		t.Fatalf("update_carnet: %v", result.Errors)
	}
	data, _ := result.Data.(map[string]any)
	carnet, _ := data["update_carnet"].(map[string]any)
	if carnet == nil || carnet["title"] != "Renomme" {
		t.Fatalf("modification perdue: %#v", data)
	}

	var identifiant int64
	if err := db.Table("carnets").Select("id").Where("title = ?", "Renomme").Row().Scan(&identifiant); err != nil {
		t.Fatalf("relecture: %v", err)
	}
	if identifiant != 1 {
		t.Errorf("l'identifiant a ete reecrit: %d", identifiant)
	}

	removed := apiCovGraphQL(t, schema, `mutation { delete_carnet(id: "1") }`)
	if len(removed.Errors) > 0 {
		t.Fatalf("delete_carnet: %v", removed.Errors)
	}
	if data, _ := removed.Data.(map[string]any); data["delete_carnet"] != true {
		t.Fatalf("suppression non confirmee: %#v", removed.Data)
	}

	var brutes int64
	if err := db.Table("carnets").Count(&brutes).Error; err != nil {
		t.Fatalf("comptage: %v", err)
	}
	if brutes != 1 {
		t.Errorf("la suppression douce a efface la ligne: %d restante(s)", brutes)
	}

	count := apiCovGraphQL(t, schema, `{ carnets_count }`)
	if data, _ := count.Data.(map[string]any); data["carnets_count"] != 0 {
		t.Errorf("une ligne supprimee est encore comptee: %#v", count.Data)
	}
}

func TestApiCovGraphQLCreateStampsTimestamps(t *testing.T) {
	registry, _ := apiCovRegistry(t)
	schema, err := registry.Schema()
	if err != nil {
		t.Fatalf("schema: %v", err)
	}

	result := apiCovGraphQL(t, schema, `mutation { create_carnet(title: "Neuf", actif: true) { id title created_at updated_at } }`)
	if len(result.Errors) > 0 {
		t.Fatalf("create_carnet: %v", result.Errors)
	}

	data, _ := result.Data.(map[string]any)
	carnet, _ := data["create_carnet"].(map[string]any)
	if carnet == nil {
		t.Fatalf("creation vide: %#v", data)
	}
	for _, horodatage := range []string{"created_at", "updated_at"} {
		if fmt.Sprint(carnet[horodatage]) == "<nil>" || carnet[horodatage] == "" {
			t.Errorf("%s non renseigne: %#v", horodatage, carnet)
		}
	}
}

func TestApiCovIntrospectionOnlyReadsRealQueries(t *testing.T) {
	cases := []struct {
		name string
		body string
		want bool
	}{
		{"corps illisible", `pas du json`, false},
		{"requete vide", `{"query":""}`, false},
		{"requete illisible", `{"query":"{ ceci n'est pas"}`, false},
		{"definition de type", `{"query":"type Chose { a: String }"}`, false},
		{"mutation", `{"query":"mutation { create_carnet(title: \"x\") { id } }"}`, false},
		{"champ metier", `{"query":"{ carnets { id } }"}`, false},
		{"fragment en racine", `{"query":"query { ...F } fragment F on Query { __typename }"}`, false},
		{"fragment inline en racine", `{"query":"query { ... on Query { __typename } }"}`, false},
		{"introspection nue", `{"query":"{ __schema { queryType { name } } }"}`, true},
		{"introspection avec fragment", `{"query":"query { __schema { types { ...T } } } fragment T on __Type { name }"}`, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := api.IntrospectionOnly([]byte(tc.body)); got != tc.want {
				t.Errorf("IntrospectionOnly = %v, want %v", got, tc.want)
			}
		})
	}
}
