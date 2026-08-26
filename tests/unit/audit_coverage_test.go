package unit

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/brianvoe/gofakeit/v7"
	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"

	"omega/app/models"
	"omega/internal/api"
	"omega/internal/auth"
	"omega/internal/cache"
	"omega/internal/database"
	"omega/internal/events"
	"omega/internal/mail"
	"omega/internal/policy"
	"omega/internal/queue"
	"omega/internal/router"
	"omega/internal/validation"
)

func TestCacheForeverFlushAndCount(t *testing.T) {
	store := cache.New()

	if err := store.Forever("permanent", "valeur"); err != nil {
		t.Fatalf("Forever: %v", err)
	}
	if !store.Has("permanent") {
		t.Fatal("la clef permanente est absente")
	}
	if store.Count() != 1 {
		t.Fatalf("Count = %d, want 1", store.Count())
	}

	var out string
	if !store.Get("permanent", &out) || out != "valeur" {
		t.Fatalf("Get = %q", out)
	}

	store.Forget("permanent")
	if store.Has("permanent") {
		t.Fatal("Forget n'a rien supprime")
	}

	_ = store.Put("a", 1, time.Minute)
	_ = store.Put("b", 2, time.Minute)
	store.Flush()
	if store.Count() != 0 {
		t.Fatalf("apres Flush, Count = %d", store.Count())
	}
}

func TestEventsListsItsListeners(t *testing.T) {
	bus := events.New(quiet)
	bus.Listen("compte.cree", "journal", func(context.Context, any) error { return nil })
	bus.ListenAsync("compte.cree", "courriel", func(context.Context, any) error { return nil })

	listed := bus.Events()
	names := listed["compte.cree"]
	if len(names) != 2 {
		t.Fatalf("%d listener(s) listes: %v", len(names), listed)
	}
}

func TestPolicyDeniesAbilitiesAndRoles(t *testing.T) {
	gate := policy.New()
	gate.Grant("editeur", "articles.write", "articles.read")
	gate.Grant("lecteur", "articles.read")

	if !gate.Denies(context.Background(), nil, "articles.write") {
		t.Error("un acteur nil n'est pas refuse")
	}

	abilities := gate.Abilities("editeur")
	if len(abilities) != 2 {
		t.Errorf("Abilities = %v", abilities)
	}

	roles := gate.Roles()
	if len(roles) != 2 || roles[0] != "editeur" {
		t.Errorf("Roles = %v", roles)
	}
}

func TestRouterHasUseStaticAndURLOr(t *testing.T) {
	app := fiber.New()
	r := router.New(app)

	r.Use(func(c *fiber.Ctx) error { return c.Next() })
	r.Static("/public", t.TempDir())
	app.Get("/articles/:id", func(c *fiber.Ctx) error { return nil }).Name("articles.show")

	if !r.Has("articles.show") {
		t.Error("Has ne trouve pas une route nommee")
	}
	if r.Has("route.absente") {
		t.Error("Has trouve une route inexistante")
	}
	if got := r.URLOr("articles.show", map[string]any{"id": 7}); got != "/articles/7" {
		t.Errorf("URLOr = %q", got)
	}
	if got := r.URLOr("route.absente", nil); got != "" {
		t.Errorf("URLOr sur une route absente = %q, want vide", got)
	}
	if r.App() == nil {
		t.Error("App() est nil")
	}
}

func TestValidationErrorMessage(t *testing.T) {
	failure := validation.Failed("email", "Cette adresse est invalide.")

	if failure.Error() == "" {
		t.Error("Error() est vide")
	}
	if failure.Fields["email"] == "" {
		t.Error("le champ n'est pas renseigne")
	}
}

func TestGuardOptionalAndRoles(t *testing.T) {
	guard := auth.New(auth.Config{Secret: "un-secret-de-test-assez-long-pour-signer"})

	app := fiber.New()
	app.Get("/libre", guard.Optional(), func(c *fiber.Ctx) error { return c.SendStatus(fiber.StatusNoContent) })
	app.Get("/admin", guard.Roles("admin"), func(c *fiber.Ctx) error { return c.SendStatus(fiber.StatusNoContent) })

	if status := statusOf(t, app, "/libre"); status != fiber.StatusNoContent {
		t.Errorf("Optional bloque une requete anonyme: %d", status)
	}
	if status := statusOf(t, app, "/admin"); status != fiber.StatusUnauthorized {
		t.Errorf("Roles laisse passer un anonyme: %d", status)
	}
}

func TestSocialProvidersAreValidated(t *testing.T) {
	names, err := auth.UseProviders([]auth.Social{
		{Name: "google", Key: "k", Secret: "s", Callback: "https://exemple.test/cb"},
		{Name: "github", Key: "k", Secret: "s", Callback: "https://exemple.test/cb", Scopes: []string{"user"}},
		{Name: "discord"},
	})
	if err != nil {
		t.Fatalf("UseProviders: %v", err)
	}
	if len(names) != 2 || names[0] != "github" || names[1] != "google" {
		t.Fatalf("noms = %v", names)
	}

	if _, err := auth.UseProviders([]auth.Social{
		{Name: "inexistant", Key: "k", Secret: "s", Callback: "https://exemple.test/cb"},
	}); err == nil || !strings.Contains(err.Error(), "google") {
		t.Fatalf("fournisseur inconnu: %v", err)
	}

	if _, err := auth.UseProviders([]auth.Social{
		{Name: "google", Key: "k", Secret: "s"},
	}); err == nil {
		t.Fatal("un fournisseur sans callback a ete accepte")
	}
}

func TestRegistryDBAndFind(t *testing.T) {
	db := noteDB(t)
	registry := api.NewRegistry(db)

	if registry.DB() != db {
		t.Error("DB() ne renvoie pas la connexion")
	}
	if registry.Find("absente") != nil {
		t.Error("Find trouve une ressource inexistante")
	}

	if err := registry.Discover(db); err != nil {
		t.Fatalf("discover: %v", err)
	}
	if registry.Find("notes") == nil {
		t.Error("Find ne trouve pas une ressource decouverte")
	}
}

func TestUserIsVerified(t *testing.T) {
	var user models.User
	if user.IsVerified() {
		t.Error("un compte sans date de verification est marque verifie")
	}

	now := time.Now()
	user.EmailVerifiedAt = &now
	if !user.IsVerified() {
		t.Error("un compte verifie est marque non verifie")
	}
}

func TestMailerReportsItsDriver(t *testing.T) {
	if got := mail.New(mail.Config{Driver: "log"}, quiet).Driver(); got != "log" {
		t.Errorf("Driver = %q, want log", got)
	}
}

func TestMailerRefusesAnUnknownDriver(t *testing.T) {
	err := mail.New(mail.Config{Driver: "pigeon", From: "a@b.test"}, quiet).
		Send(mail.Message{To: []string{"c@d.test"}, Subject: "x"})

	if err == nil || !strings.Contains(err.Error(), "pigeon") {
		t.Fatalf("driver inconnu: %v", err)
	}
}

func TestQueueListsItsHandlersInOrder(t *testing.T) {
	q := queue.New(nil)
	q.Handle("zeta", func(context.Context, []byte) error { return nil })
	q.Handle("alpha", func(context.Context, []byte) error { return nil })

	names := q.Names()
	if len(names) != 2 || names[0] != "alpha" || names[1] != "zeta" {
		t.Fatalf("Names = %v — attendu trie", names)
	}
}

func TestSeedersAreListedAndRunnable(t *testing.T) {
	db := noteDB(t)

	ran := 0
	database.RegisterSeeder(database.SeederFunc{
		SeederName: "audit-seed",
		Fn:         func(*gorm.DB) error { ran++; return nil },
	})

	found := false
	for _, seeder := range database.Seeders() {
		if seeder.Name() == "audit-seed" {
			found = true
		}
	}
	if !found {
		t.Fatal("le seeder enregistre n'est pas liste")
	}

	if err := database.Seed(db, "audit-seed"); err != nil {
		t.Fatalf("Seed: %v", err)
	}
	if ran != 1 {
		t.Fatalf("le seeder a tourne %d fois", ran)
	}

	if err := database.Seed(db, "inexistant"); err == nil {
		t.Error("un seeder inconnu a ete accepte")
	}
}

func TestFactoryMakesAndCreates(t *testing.T) {
	database.SeedFaker(1234)
	if database.Faker() == nil {
		t.Fatal("Faker() est nil")
	}

	factory := database.NewFactory(func(f *gofakeit.Faker) note {
		return note{Title: f.Word(), Slug: f.Word()}
	})

	if made := factory.Make(); made.Title == "" {
		t.Error("Make ne remplit rien")
	}
	if many := factory.MakeMany(3); len(many) != 3 {
		t.Errorf("MakeMany = %d", len(many))
	}

	fixed := factory.State(func(n *note) { n.Title = "impose" })
	if made := fixed.Make(); made.Title != "impose" {
		t.Errorf("State ignore: %q", made.Title)
	}

	db := noteDB(t)
	created, err := fixed.Create(db)
	if err != nil || created.Title != "impose" {
		t.Fatalf("Create: %+v, %v", created, err)
	}
	if batch, err := fixed.CreateMany(db, 2); err != nil || len(batch) != 2 {
		t.Fatalf("CreateMany: %d, %v", len(batch), err)
	}
}
