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

	"golang.org/x/crypto/bcrypt"

	"omega"
	"omega/app/models"
	"omega/routes"

	_ "omega/database/migrations"
	_ "omega/database/seeds"
	"omega/internal/database"

	"github.com/gofiber/fiber/v3"
)

type server struct {
	app   *omega.App
	t     *testing.T
	token string

	// cookies switches the client to the cookie transport: no bearer mode
	// header, and the cookies the server set sent back, like a browser.
	cookies bool
	jar     map[string]string
	csrf    bool
}

func init() {
	// bcrypt at the production cost takes seconds under the race detector;
	// the suite tests the logic, not the work factor.
	models.PasswordCost = bcrypt.MinCost + 1
}

func boot(t *testing.T) *server {
	t.Helper()
	return bootWith(t, nil)
}

// bootWith boots like boot, then applies env on top of the test defaults
// (APP_ENV=production, a feature flag, ...).
func bootWith(t *testing.T, env map[string]string) *server {
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
	for name, value := range env {
		t.Setenv(name, value)
	}

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
	header http.Header
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
	if s.cookies {
		for name, value := range s.jar {
			request.AddCookie(&http.Cookie{Name: name, Value: value})
		}
		if s.csrf && s.jar["omega_csrf"] != "" {
			request.Header.Set("X-CSRF-Token", s.jar["omega_csrf"])
		}
	} else {
		// The API clients' mode: tokens in the body, never in cookies.
		request.Header.Set("X-Omega-Auth", "bearer")
	}

	result, err := s.app.Fiber.Test(request, fiber.TestConfig{Timeout: 0, FailOnTimeout: false})
	if err != nil {
		s.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer result.Body.Close()

	if s.cookies {
		if s.jar == nil {
			s.jar = map[string]string{}
		}
		for _, cookie := range result.Cookies() {
			if cookie.MaxAge < 0 || cookie.Value == "" {
				delete(s.jar, cookie.Name)
				continue
			}
			s.jar[cookie.Name] = cookie.Value
		}
	}

	raw, _ := io.ReadAll(result.Body)
	body := map[string]any{}
	_ = json.Unmarshal(raw, &body)

	return response{status: result.StatusCode, body: body, raw: string(raw), header: result.Header}
}

func (s *server) get(path string) response    { return s.do(http.MethodGet, path, nil) }
func (s *server) delete(path string) response { return s.do(http.MethodDelete, path, nil) }

func (s *server) post(path string, payload any) response {
	return s.do(http.MethodPost, path, payload)
}

func (s *server) put(path string, payload any) response {
	return s.do(http.MethodPut, path, payload)
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
