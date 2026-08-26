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

	"omega"
	"omega/routes"

	_ "omega/database/migrations"
	_ "omega/database/seeds"
	"omega/internal/database"
)

type server struct {
	app   *omega.App
	t     *testing.T
	token string
}

func boot(t *testing.T) *server {
	t.Helper()

	root, found := projectRoot()
	if !found {
		t.Skip("no project root found")
	}
	if err := os.Chdir(root); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	file := filepath.Join(t.TempDir(), "test.db")
	t.Setenv("DB_SQLITE_PATH", file)
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
	if err := routes.RegisterAPI(app); err != nil {
		t.Fatalf("routes: %v", err)
	}

	t.Cleanup(func() { _ = app.Shutdown() })
	return &server{app: app, t: t}
}

func projectRoot() (string, bool) {
	current, err := os.Getwd()
	if err != nil {
		return "", false
	}
	for {
		if _, err := os.Stat(filepath.Join(current, "config", "app.yaml")); err == nil {
			return current, true
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", false
		}
		current = parent
	}
}

type response struct {
	status int
	body   map[string]any
	raw    string
}

func (s *server) do(method, path string, payload any) response {
	s.t.Helper()

	var reader io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			s.t.Fatalf("encoding the payload: %v", err)
		}
		reader = bytes.NewReader(encoded)
	}

	request := httptest.NewRequest(method, path, reader)
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if s.token != "" {
		request.Header.Set("Authorization", "Bearer "+s.token)
	}

	result, err := s.app.Fiber.Test(request, -1)
	if err != nil {
		s.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer result.Body.Close()

	raw, _ := io.ReadAll(result.Body)
	body := map[string]any{}
	_ = json.Unmarshal(raw, &body)

	return response{status: result.StatusCode, body: body, raw: string(raw)}
}

func (s *server) get(path string) response    { return s.do(http.MethodGet, path, nil) }
func (s *server) delete(path string) response { return s.do(http.MethodDelete, path, nil) }

func (s *server) post(path string, payload any) response {
	return s.do(http.MethodPost, path, payload)
}

func (s *server) patch(path string, payload any) response {
	return s.do(http.MethodPatch, path, payload)
}

func (s *server) signUp(email string) response {
	answer := s.post("/api/auth/register", map[string]any{
		"name": "Integration", "email": email, "password": "correct-horse-battery",
	})
	if tokens, ok := answer.body["tokens"].(map[string]any); ok {
		s.token, _ = tokens["access_token"].(string)
	}
	return answer
}

func (s *server) signUpAdmin(email string) response {
	answer := s.signUp(email)
	if err := s.app.DB.Table("users").Where("email = ?", email).
		Update("role", "admin").Error; err != nil {
		s.t.Fatalf("promoting %s: %v", email, err)
	}
	return answer
}

func (r response) tokens() map[string]any {
	tokens, _ := r.body["tokens"].(map[string]any)
	return tokens
}

func (r response) data() map[string]any {
	data, _ := r.body["data"].(map[string]any)
	return data
}

func (r response) list() []any {
	data, _ := r.body["data"].([]any)
	return data
}

func (r response) errors() map[string]any {
	errs, _ := r.body["errors"].(map[string]any)
	return errs
}
