package integration

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"omega"
	"omega/internal/database"
	"omega/routes"
)

func TestCorsOriginsComeFromTheConfiguration(t *testing.T) {
	t.Setenv("CORS_ORIGINS", "https://console.dinasa.ht")
	s := boot(t)

	request := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	request.Header.Set("Origin", "https://console.dinasa.ht")

	response, err := s.app.Fiber.Test(request, -1)
	if err != nil {
		t.Fatalf("requete: %v", err)
	}

	if got := response.Header.Get("Access-Control-Allow-Origin"); got != "https://console.dinasa.ht" {
		t.Fatalf("Allow-Origin = %q — CORS_ORIGINS n'est pas pris en compte", got)
	}
}

func bootWithGlobalLimit(t *testing.T, max string) *server {
	t.Helper()

	root, found := projectRoot()
	if !found {
		t.Skip("no project root found")
	}
	if err := os.Chdir(root); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	t.Setenv("DB_SQLITE_PATH", filepath.Join(t.TempDir(), "test.db"))
	t.Setenv("AUTH_SECRET", "an-integration-secret-long-enough-to-sign")
	t.Setenv("APP_ENV", "testing")
	t.Setenv("LOG_LEVEL", "error")
	t.Setenv("RATELIMIT_MAX", max)

	app, err := omega.Boot()
	if err != nil {
		t.Fatalf("boot: %v", err)
	}
	if err := database.Up(app.DB); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := routes.RegisterAPI(app); err != nil {
		t.Fatalf("routes: %v", err)
	}

	t.Cleanup(func() { _ = app.Shutdown() })
	return &server{app: app, t: t}
}

func TestGlobalRateLimitCanBeTurnedOn(t *testing.T) {
	s := bootWithGlobalLimit(t, "5")

	blocked := 0
	for i := 0; i < 20; i++ {
		if s.get("/api/health").status == http.StatusTooManyRequests {
			blocked++
		}
	}

	if blocked == 0 {
		t.Fatal("RATELIMIT_MAX=5 n'a bloque aucune requete sur 20")
	}
}

func TestConfigKeysFromAppYamlAreNamespaced(t *testing.T) {
	s := boot(t)

	for _, key := range []string{
		"app.name", "app.cors.origins", "app.ratelimit.max",
		"app.ratelimit.credentials.max", "app.ratelimit.credentials.window",
	} {
		if !s.app.Cfg.Has(key) {
			t.Errorf("%s introuvable — une lecture de config sans prefixe retomberait en silence sur son defaut", key)
		}
	}
}
