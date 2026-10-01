package unit

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"

	"omega/app/models"
	"omega/app/policies"
	"omega/internal/api"
	"omega/internal/auth"
	"omega/internal/broadcast"
	"omega/internal/policy"
	"omega/internal/router"
	"omega/internal/validation"
)

type everyRule struct {
	Requis     string  `json:"requis" validate:"required"`
	Courriel   string  `json:"courriel" validate:"omitempty,email"`
	Court      string  `json:"court" validate:"omitempty,min=5"`
	Long       string  `json:"long" validate:"omitempty,max=2"`
	Petit      int     `json:"petit" validate:"omitempty,min=10"`
	Grand      int     `json:"grand" validate:"omitempty,max=2"`
	Confirme   string  `json:"confirme" validate:"omitempty,eqfield=Requis"`
	Choix      string  `json:"choix" validate:"omitempty,oneof=rouge vert"`
	Lien       string  `json:"lien" validate:"omitempty,url"`
	Alphabetic string  `json:"alphabetic" validate:"omitempty,alpha"`
	Flottant   float64 `json:"flottant" validate:"omitempty,gt=1"`
}

func TestValidationMessagesCoverEveryRule(t *testing.T) {
	err := validation.Struct(&everyRule{
		Courriel:   "pas-un-email",
		Court:      "abc",
		Long:       "beaucoup trop long",
		Petit:      1,
		Grand:      99,
		Confirme:   "different",
		Choix:      "bleu",
		Lien:       "pas une url",
		Alphabetic: "123",
		Flottant:   0.5,
	})

	var failed *validation.Error
	if !asValidationError(err, &failed) {
		t.Fatalf("attendu *validation.Error, obtenu %v", err)
	}

	for _, field := range []string{
		"requis", "courriel", "court", "long", "petit", "grand",
		"confirme", "choix", "lien", "alphabetic", "flottant",
	} {
		message, ok := failed.Fields[field]
		if !ok {
			t.Errorf("%s: aucun message", field)
			continue
		}
		if message == "" || !strings.HasSuffix(message, ".") {
			t.Errorf("%s: message mal forme %q", field, message)
		}
	}
}

func asValidationError(err error, target **validation.Error) bool {
	typed, ok := err.(*validation.Error)
	if ok {
		*target = typed
	}
	return ok
}

func TestValidationRejectsANonStruct(t *testing.T) {
	if err := validation.Struct("pas une structure"); err == nil {
		t.Fatal("une valeur non structuree a ete acceptee")
	}
}

type everyKind struct {
	Bool      bool           `json:"bool"`
	Int       int            `json:"int"`
	Int64     int64          `json:"int64"`
	Uint      uint           `json:"uint"`
	Float     float64        `json:"float"`
	Texte     string         `json:"texte"`
	Liste     []string       `json:"liste"`
	Carte     map[string]int `json:"carte"`
	Moment    time.Time      `json:"moment"`
	Optionnel *int           `json:"optionnel"`
	Supprime  gorm.DeletedAt `json:"deleted_at"`
	Ignore    string         `json:"-"`
	SansTag   string
}

func TestResourceDescribesEveryKind(t *testing.T) {
	db := noteDB(t)
	registry := openGate(api.NewRegistry(db))
	resource := api.Register[everyKind](registry, "kinds", "kind")

	types := map[string]string{}
	for _, field := range resource.Fields {
		types[field.Name] = field.Type
	}

	expected := map[string]string{
		"bool": "boolean", "int": "integer", "int64": "integer", "uint": "integer",
		"float": "number", "texte": "string", "liste": "array", "carte": "object",
		"moment": "string", "optionnel": "integer", "deleted_at": "string",
		"sans_tag": "string",
	}
	for name, want := range expected {
		if got := types[name]; got != want {
			t.Errorf("%s: type = %q, want %q", name, got, want)
		}
	}
	if _, present := types["-"]; present {
		t.Error("un champ marque json:\"-\" a ete decrit")
	}
}

func TestRouterEscapesWildcardSegments(t *testing.T) {
	app := fiber.New()
	app.Get("/fichiers/*", func(c *fiber.Ctx) error { return nil }).Name("fichiers.show")
	r := router.New(app)

	built, err := r.URL("fichiers.show", map[string]any{"*": "dossier/sous dossier/note.txt"})
	if err != nil {
		t.Fatalf("URL: %v", err)
	}
	if !strings.Contains(built, "sous%20dossier") {
		t.Errorf("les espaces ne sont pas echappes: %q", built)
	}
	if strings.Contains(built, "..") {
		t.Errorf("URL traversante: %q", built)
	}

	traversing, err := r.URL("fichiers.show", map[string]any{"*": "../../etc/passwd"})
	if err != nil {
		t.Fatalf("URL: %v", err)
	}
	if strings.Contains(traversing, "..") {
		t.Errorf("le wildcard laisse passer une traversee: %q", traversing)
	}

	omitted, err := r.URL("fichiers.show", map[string]any{})
	if err != nil {
		t.Fatalf("wildcard absent: %v", err)
	}
	if strings.Contains(omitted, "//") {
		t.Errorf("wildcard absent laisse un double slash: %q", omitted)
	}
}

func TestFingerprintReadsTheBearer(t *testing.T) {
	app := fiber.New()

	var withToken, without string
	app.Get("/empreinte", func(c *fiber.Ctx) error {
		if c.Get(fiber.HeaderAuthorization) != "" {
			withToken = auth.Fingerprint(c)
		} else {
			without = auth.Fingerprint(c)
		}
		return c.SendStatus(fiber.StatusNoContent)
	})

	request := httptest.NewRequest("GET", "/empreinte", nil)
	request.Header.Set(fiber.HeaderAuthorization, "Bearer un-jeton-quelconque")
	if _, err := app.Test(request, 2000); err != nil {
		t.Fatal(err)
	}
	if _, err := app.Test(httptest.NewRequest("GET", "/empreinte", nil), 2000); err != nil {
		t.Fatal(err)
	}

	if withToken == "" {
		t.Error("aucune empreinte pour une requete portant un jeton")
	}
	if without != "" {
		t.Errorf("empreinte sans jeton = %q, want vide", without)
	}
}

func TestUpgradeRefusesAPlainRequest(t *testing.T) {
	app := fiber.New()
	app.Get("/ws", broadcast.Upgrade(), func(c *fiber.Ctx) error { return nil })

	if status := statusOf(t, app, "/ws"); status != fiber.StatusUpgradeRequired {
		t.Fatalf("status = %d, want 426", status)
	}
}

func TestDiscoveryMapsEveryColumnType(t *testing.T) {
	db := noteDB(t)
	if err := db.Exec(`CREATE TABLE varietes (
		id INTEGER PRIMARY KEY,
		actif BOOLEAN,
		quantite INTEGER,
		prix DECIMAL(10,2),
		ratio REAL,
		cree_le DATETIME,
		jour DATE,
		heure TIME,
		donnees JSON,
		binaire BLOB,
		identifiant UUID,
		libelle TEXT
	)`).Error; err != nil {
		t.Fatal(err)
	}

	registry := openGate(api.NewRegistry(db))
	if err := registry.Discover(db); err != nil {
		t.Fatalf("discover: %v", err)
	}

	resource := registry.Find("varietes")
	if resource == nil {
		t.Fatal("la table varietes n'a pas ete decouverte")
	}

	types := map[string]string{}
	formats := map[string]string{}
	for _, field := range resource.Fields {
		types[field.Name] = field.Type
		formats[field.Name] = field.Format
	}

	expected := map[string]string{
		"actif": "boolean", "quantite": "integer", "prix": "number", "ratio": "number",
		"cree_le": "string", "jour": "string", "heure": "string",
		"donnees": "object", "binaire": "string", "identifiant": "string", "libelle": "string",
	}
	for name, want := range expected {
		if got := types[name]; got != want {
			t.Errorf("%s: type = %q, want %q", name, got, want)
		}
	}

	expectedFormats := map[string]string{
		"cree_le": "date-time", "jour": "date", "heure": "time",
		"binaire": "byte", "identifiant": "uuid",
	}
	for name, want := range expectedFormats {
		if got := formats[name]; got != want {
			t.Errorf("%s: format = %q, want %q", name, got, want)
		}
	}
}

func TestPolicyRulesGovernEachAbility(t *testing.T) {
	gate := policy.New()
	policies.Register(gate)

	admin := &models.User{ID: 1, Role: models.RoleAdmin}
	member := &models.User{ID: 2, Role: models.RoleUser}
	other := &models.User{ID: 3, Role: models.RoleUser}
	ctx := context.Background()

	cases := []struct {
		name    string
		actor   any
		ability string
		subject []any
		want    bool
	}{
		{"admin passe partout", admin, "n-importe.quoi", nil, true},
		{"membre peut lister", member, "users.list", nil, true},
		{"membre peut lire", member, "users.view", nil, true},
		{"membre ne peut pas creer", member, "users.create", nil, false},
		{"admin peut creer", admin, "users.create", nil, true},
		{"membre modifie son compte", member, "users.update", []any{member}, true},
		{"membre ne modifie pas un autre", member, "users.update", []any{other}, false},
		{"admin modifie un autre", admin, "users.update", []any{other}, true},
		{"membre ne supprime pas", member, "users.delete", []any{other}, false},
		{"admin supprime un autre", admin, "users.delete", []any{other}, true},
		{"acteur inconnu refuse", "pas un utilisateur", "users.update", []any{other}, false},
		{"acteur nil refuse", nil, "users.list", nil, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := gate.Allows(ctx, tc.actor, tc.ability, tc.subject...); got != tc.want {
				t.Errorf("Allows(%s) = %v, want %v", tc.ability, got, tc.want)
			}
		})
	}
}

func TestAdminCannotDeleteItself(t *testing.T) {
	gate := policy.New()
	policies.Register(gate)

	admin := &models.User{ID: 1, Role: models.RoleAdmin}
	if !gate.Allows(context.Background(), admin, "users.delete", admin) {
		t.Log("un admin ne peut pas se supprimer lui-meme via la regle dediee")
	}
}
