package integration

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"

	"omega"
	"omega/app/models"
	jwt "omega/internal/auth"
)

func TestFacadeExposesEveryBootedService(t *testing.T) {
	boot(t)

	checks := map[string]bool{
		"Cfg":       omega.Cfg() != nil,
		"DB":        omega.DB() != nil,
		"Router":    omega.Router() != nil,
		"Events":    omega.Events() != nil,
		"Cache":     omega.Cache() != nil,
		"Mail":      omega.Mail() != nil,
		"Storage":   omega.Storage() != nil,
		"Broadcast": omega.Broadcast() != nil,
		"Metrics":   omega.Metrics() != nil,
		"Policy":    omega.Policy() != nil,
		"Schedule":  omega.Schedule() != nil,
		"Queue":     omega.Queue() != nil,
	}
	for name, ok := range checks {
		if !ok {
			t.Errorf("omega.%s() est nil apres Boot", name)
		}
	}
}

func TestFacadeRememberDelegatesToTheCache(t *testing.T) {
	boot(t)

	builds := 0
	build := func() (string, error) {
		builds++
		return "valeur", nil
	}

	for i := 0; i < 3; i++ {
		got, err := omega.Remember("facade", time.Minute, build)
		if err != nil || got != "valeur" {
			t.Fatalf("Remember: %q, %v", got, err)
		}
	}
	if builds != 1 {
		t.Errorf("construit %d fois, want 1", builds)
	}
}

func TestFacadeEmitReachesItsListeners(t *testing.T) {
	boot(t)

	seen := make(chan any, 1)
	omega.Events().Listen("facade.test", "audit", func(ctx context.Context, payload any) error {
		seen <- payload
		return nil
	})

	if err := omega.Emit(context.Background(), "facade.test", 42); err != nil {
		t.Fatalf("Emit: %v", err)
	}

	select {
	case payload := <-seen:
		if payload != 42 {
			t.Errorf("payload = %v, want 42", payload)
		}
	case <-time.After(time.Second):
		t.Fatal("le listener n'a jamais recu l'evenement")
	}
}

func TestFacadeURLGeneratesANamedRoute(t *testing.T) {
	boot(t)

	built, err := omega.URL("api.users.show", map[string]any{"id": 1})
	if err != nil {
		t.Fatalf("URL: %v", err)
	}
	if built != "/api/users/1" {
		t.Fatalf("URL = %q, want /api/users/1", built)
	}

	if _, err := omega.URL("route.inexistante", nil); err == nil {
		t.Error("une route inconnue a produit une URL")
	}
}

func TestFacadeContainerBindsAndResolves(t *testing.T) {
	boot(t)

	omega.Bind("audit.service", "une-valeur")

	got, err := omega.Make[string]("audit.service")
	if err != nil || got != "une-valeur" {
		t.Fatalf("Make: %q, %v", got, err)
	}

	if _, err := omega.Make[string]("audit.absent"); err == nil {
		t.Error("un service inconnu a ete resolu")
	}

	if _, err := omega.Make[int]("audit.service"); err == nil {
		t.Error("un service resolu dans le mauvais type")
	}
}

func TestFacadeConnectionRejectsAnUnknownName(t *testing.T) {
	boot(t)

	if _, err := omega.Connection("inexistante"); err == nil {
		t.Error("une connexion inconnue a ete ouverte")
	}
}

func TestFacadeStorageIsConfined(t *testing.T) {
	boot(t)

	if err := omega.Storage().PutBytes("audit/ok.txt", []byte("dedans")); err != nil {
		t.Fatalf("ecriture legitime refusee: %v", err)
	}
	t.Cleanup(func() { _ = omega.Storage().Delete("audit/ok.txt") })

	if err := omega.Storage().PutBytes("../../evade.txt", []byte("normalise")); err != nil {
		t.Fatalf("ecriture traversante: %v", err)
	}
	t.Cleanup(func() { _ = omega.Storage().Delete("evade.txt") })

	landed, err := omega.Storage().Get("evade.txt")
	if err != nil || string(landed) != "normalise" {
		t.Fatalf("un nom traversant n'a pas ete ramene dans la racine: %q, %v", landed, err)
	}
}

func TestFacadeAllowsHonoursThePolicy(t *testing.T) {
	boot(t)

	if omega.Allows(context.Background(), nil, "users.delete") {
		t.Error("un acteur nil a obtenu users.delete")
	}
}

func TestFacadeDispatchRefusesAnUnknownJob(t *testing.T) {
	boot(t)

	err := omega.Dispatch(context.Background(), "job.inexistant", nil)
	if err == nil {
		t.Error("une tache inconnue a ete acceptee")
		return
	}
	if errors.Is(err, context.Canceled) {
		t.Errorf("erreur inattendue: %v", err)
	}
}

func TestConfigAccessorsReadEveryShape(t *testing.T) {
	s := boot(t)
	cfg := s.app.Cfg

	if cfg.Dir() == "" {
		t.Error("Dir() est vide")
	}
	if cfg.Viper() == nil {
		t.Error("Viper() est nil")
	}
	if cfg.Get("app.name") == nil {
		t.Error("Get(app.name) est nil")
	}
	if cfg.String("app.env") != "testing" {
		t.Errorf("String(app.env) = %q", cfg.String("app.env"))
	}
	if cfg.Int("app.port") == 0 {
		t.Error("Int(app.port) vaut 0")
	}
	if cfg.Duration("app.read_timeout") == 0 {
		t.Error("Duration(app.read_timeout) vaut 0")
	}
	if len(cfg.Map("app.cors")) == 0 {
		t.Error("Map(app.cors) est vide")
	}
	_ = cfg.Bool("app.debug")
	_ = cfg.StringSlice("app.trusted_proxies")

	if cfg.Env() != "testing" {
		t.Errorf("Env() = %q", cfg.Env())
	}
	if cfg.IsLocal() {
		t.Error("IsLocal() est vrai en environnement testing")
	}
}

func TestFacadeExposesTheLoggerAndTheContainer(t *testing.T) {
	s := boot(t)

	logger := omega.Log()
	logger.Debug().Msg("audit")

	if s.app.Container() == nil {
		t.Error("Container() est nil")
	}
	if s.app.Addr() == "" {
		t.Error("Addr() est vide")
	}
}

func TestFacadeMustMakeReturnsOrPanics(t *testing.T) {
	boot(t)
	omega.Bind("audit.mustmake", "present")

	if got := omega.MustMake[string]("audit.mustmake"); got != "present" {
		t.Fatalf("MustMake = %q", got)
	}

	defer func() {
		if recover() == nil {
			t.Error("MustMake n'a pas panique sur un nom inconnu")
		}
	}()
	_ = omega.MustMake[string]("audit.absent")
}

func TestFacadeModelBindsThroughTheRouter(t *testing.T) {
	s := boot(t)

	s.app.Fiber.Get("/audit/users/:id", omega.Model[models.User](), func(c *fiber.Ctx) error {
		return c.SendString(omega.Bound[models.User](c).Email)
	})

	s.signUp("facade-model@integration.test")
	if answer := s.get("/audit/users/1"); answer.status != http.StatusOK {
		t.Fatalf("status = %d, want 200 — %s", answer.status, truncate(answer.raw))
	}
	if answer := s.get("/audit/users/99999"); answer.status != http.StatusNotFound {
		t.Fatalf("id inconnu: status = %d, want 404", answer.status)
	}
}

func TestRefreshTokenStorePurgesExpiredRows(t *testing.T) {
	s := boot(t)
	store := jwt.NewStore(s.app.DB)

	if err := store.Save("perime", "1", time.Now().Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := store.Save("valide", "1", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	if err := store.Purge(time.Now()); err != nil {
		t.Fatalf("Purge: %v", err)
	}

	if exists, _ := store.Exists("perime"); exists {
		t.Error("un jeton expire a survecu a la purge")
	}
	if exists, _ := store.Exists("valide"); !exists {
		t.Error("un jeton valide a ete purge")
	}
}
