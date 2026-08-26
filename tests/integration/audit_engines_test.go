package integration

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"omega"
	"omega/internal/database"
	"omega/routes"
)

type engine struct {
	name       string
	connection string
	host       string
	port       string
	envPort    string
}

var engines = []engine{
	{name: "postgres", connection: "postgres", host: "127.0.0.1", port: "55433", envPort: "OMEGA_TEST_PG_PORT"},
	{name: "mysql", connection: "mysql", host: "127.0.0.1", port: "53306", envPort: "OMEGA_TEST_MYSQL_PORT"},
}

func (e engine) resolvedPort() string {
	if custom := os.Getenv(e.envPort); custom != "" {
		return custom
	}
	return e.port
}

func (e engine) reachable() bool {
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(e.host, e.resolvedPort()), 500*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func bootOn(t *testing.T, e engine) *server {
	t.Helper()

	if !e.reachable() {
		t.Skipf("%s injoignable sur %s:%s", e.name, e.host, e.resolvedPort())
	}

	root, found := projectRoot()
	if !found {
		t.Skip("no project root found")
	}
	if err := os.Chdir(root); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	t.Setenv("DB_CONNECTION", e.connection)
	t.Setenv("DB_HOST", e.host)
	t.Setenv("DB_PORT", e.resolvedPort())
	t.Setenv("DB_DATABASE", "omega")
	t.Setenv("DB_USERNAME", "omega")
	t.Setenv("DB_PASSWORD", "omega")
	t.Setenv("DB_SQLITE_PATH", filepath.Join(t.TempDir(), "unused.db"))
	t.Setenv("AUTH_SECRET", "an-integration-secret-long-enough-to-sign")
	t.Setenv("APP_ENV", "testing")
	t.Setenv("LOG_LEVEL", "error")
	t.Setenv("RATELIMIT_MAX", "0")

	app, err := omega.Boot()
	if err != nil {
		t.Fatalf("%s boot: %v", e.name, err)
	}
	t.Cleanup(func() { _ = app.Shutdown() })

	if err := database.Fresh(app.DB); err != nil {
		t.Fatalf("%s fresh: %v", e.name, err)
	}
	if err := routes.RegisterAPI(app); err != nil {
		t.Fatalf("%s routes: %v", e.name, err)
	}
	return &server{app: app, t: t}
}

func TestEveryEngineBootsAndMigrates(t *testing.T) {
	for _, e := range engines {
		t.Run(e.name, func(t *testing.T) {
			s := bootOn(t, e)

			if got := s.app.DB.Dialector.Name(); got != e.connection {
				t.Fatalf("dialecte = %q, want %q", got, e.connection)
			}

			statuses, err := database.StatusList(s.app.DB)
			if err != nil {
				t.Fatalf("status: %v", err)
			}
			for _, status := range statuses {
				if !status.Applied {
					t.Errorf("%s non appliquee", status.ID)
				}
			}
		})
	}
}

func TestEveryEngineServesTheAuthAndCrudRoundTrip(t *testing.T) {
	for _, e := range engines {
		t.Run(e.name, func(t *testing.T) {
			s := bootOn(t, e)

			email := fmt.Sprintf("moteur-%s@integration.test", e.name)
			if answer := s.signUp(email); answer.status != http.StatusCreated && answer.status != http.StatusOK {
				t.Fatalf("inscription: status = %d — %s", answer.status, truncate(answer.raw))
			}

			listed := s.get("/api/users")
			if listed.status != http.StatusOK {
				t.Fatalf("liste: status = %d — %s", listed.status, truncate(listed.raw))
			}
			if len(listed.list()) == 0 {
				t.Fatal("la liste est vide apres inscription")
			}

			filtered := s.get("/api/users?email=" + email)
			if rows := filtered.list(); len(rows) != 1 {
				t.Fatalf("filtre: %d ligne(s), want 1", len(rows))
			}

			sorted := s.get("/api/users?sort=-id")
			if sorted.status != http.StatusOK {
				t.Fatalf("tri: status = %d — %s", sorted.status, truncate(sorted.raw))
			}
		})
	}
}

func TestEveryEngineSurvivesResetAndFresh(t *testing.T) {
	for _, e := range engines {
		t.Run(e.name, func(t *testing.T) {
			s := bootOn(t, e)

			if err := database.Reset(s.app.DB); err != nil {
				t.Fatalf("reset: %v", err)
			}
			applied, err := database.Applied(s.app.DB)
			if err != nil {
				t.Fatalf("applied: %v", err)
			}
			if len(applied) != 0 {
				t.Fatalf("apres Reset il reste %d migration(s)", len(applied))
			}

			if err := database.Fresh(s.app.DB); err != nil {
				t.Fatalf("fresh apres reset: %v", err)
			}
			if !s.app.DB.Migrator().HasTable("users") {
				t.Fatal("table users absente apres Fresh")
			}
		})
	}
}
