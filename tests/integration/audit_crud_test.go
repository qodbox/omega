package integration

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gofiber/fiber/v2"

	"omega"
	"omega/internal/api"
	"omega/internal/database"
	"omega/routes"
)

func bootWithNotes(t *testing.T) *server {
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
	t.Setenv("RATELIMIT_MAX", "0")

	app, err := omega.Boot()
	if err != nil {
		t.Fatalf("boot: %v", err)
	}
	if err := database.Up(app.DB); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := app.DB.Exec(`CREATE TABLE notes (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		title TEXT NOT NULL,
		body TEXT,
		created_at DATETIME,
		updated_at DATETIME
	)`).Error; err != nil {
		t.Fatalf("table notes: %v", err)
	}
	if err := routes.RegisterAPI(app); err != nil {
		t.Fatalf("routes: %v", err)
	}

	t.Cleanup(func() { _ = app.Shutdown() })
	return &server{app: app, t: t}
}

func TestDiscoveredTablesAreReadOnly(t *testing.T) {
	s := bootWithNotes(t)
	s.signUpAdmin("lecture@integration.test")

	if listed := s.get("/api/notes"); listed.status != http.StatusOK {
		t.Fatalf("lecture: status = %d — %s", listed.status, listed.raw)
	}

	created := s.post("/api/notes", map[string]any{"title": "Interdite"})
	if created.status != http.StatusMethodNotAllowed {
		t.Fatalf("ecriture sur une table decouverte: status = %d, want 405 — %s", created.status, created.raw)
	}
}

func TestAllowWritesReopensADiscoveredTable(t *testing.T) {
	s := bootWithNotes(t)

	registry := api.NewRegistry(s.app.DB)
	if err := registry.Discover(s.app.DB); err != nil {
		t.Fatalf("discover: %v", err)
	}
	registry.AllowWrites("notes")

	app := fiber.New()
	registry.Mount(app.Group("/api"))

	body, _ := json.Marshal(map[string]any{"title": "Autorisee", "body": "contenu"})
	request := httptest.NewRequest(http.MethodPost, "/api/notes", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")

	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("requete: %v", err)
	}
	if response.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(response.Body)
		t.Fatalf("status = %d, want 201 — %s", response.StatusCode, raw)
	}

	raw, _ := io.ReadAll(response.Body)
	answer := map[string]any{}
	if err := json.Unmarshal(raw, &answer); err != nil {
		t.Fatalf("reponse: %v", err)
	}
	data, _ := answer["data"].(map[string]any)
	if data == nil || data["id"] == nil {
		t.Fatalf("la ressource creee n'a pas d'id: %s", raw)
	}
	if data["title"] != "Autorisee" {
		t.Fatalf("title = %v", data["title"])
	}
}

func TestUpdateReturnsTheUpdatedRow(t *testing.T) {
	s := boot(t)
	s.signUpAdmin("patcher@integration.test")

	updated := s.patch("/api/users/1", map[string]any{"name": "Renomme"})
	if updated.status != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", updated.status, updated.raw)
	}

	data, _ := updated.body["data"].(map[string]any)
	if data["name"] != "Renomme" {
		t.Fatalf("name = %v, want Renomme", data["name"])
	}
}

func TestUpdateOnAMissingRowIs404(t *testing.T) {
	s := boot(t)
	s.signUpAdmin("missing@integration.test")

	answer := s.patch("/api/users/999999", map[string]any{"name": "Fantome"})
	if answer.status != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", answer.status, answer.raw)
	}
}

func TestCreateRejectsABodyWithoutWritableField(t *testing.T) {
	s := boot(t)
	s.signUpAdmin("empty@integration.test")

	answer := s.post("/api/users", map[string]any{"password": "secret", "inconnu": 1})
	if answer.status != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422: %s", answer.status, answer.raw)
	}
}

func TestSortIsValidatedAndApplied(t *testing.T) {
	s := boot(t)
	s.signUp("sorter@integration.test")

	if answer := s.get("/api/users?sort=-id"); answer.status != http.StatusOK {
		t.Fatalf("tri descendant refuse: status = %d, %s", answer.status, answer.raw)
	}
	if answer := s.get("/api/users?sort=bogus"); answer.status != http.StatusUnprocessableEntity {
		t.Fatalf("tri inconnu: status = %d, want 422", answer.status)
	}
	if answer := s.get("/api/users?sort=password"); answer.status != http.StatusUnprocessableEntity {
		t.Fatalf("tri sur une colonne cachee: status = %d, want 422", answer.status)
	}
}

func TestPaginationClampsItsBounds(t *testing.T) {
	s := boot(t)
	s.signUp("pager@integration.test")

	answer := s.get("/api/users?per_page=99999")
	meta, _ := answer.body["meta"].(map[string]any)
	if meta["per_page"] != float64(200) {
		t.Errorf("per_page non borne: %v, want 200", meta["per_page"])
	}

	answer = s.get("/api/users?page=0")
	meta, _ = answer.body["meta"].(map[string]any)
	if meta["page"] != float64(1) {
		t.Errorf("page non bornee: %v, want 1", meta["page"])
	}
}
