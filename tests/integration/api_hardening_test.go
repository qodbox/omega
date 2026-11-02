package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"

	"omega"
	"omega/internal/api"
	"omega/routes"
)

func userID(t *testing.T, s *server, email string) int {
	t.Helper()
	var id int
	if err := s.app.DB.Table("users").Select("id").Where("email = ?", email).Scan(&id).Error; err != nil || id == 0 {
		t.Fatalf("no account %s: %v", email, err)
	}
	return id
}

func TestDiscoveryModeNoneServesRegisteredResourcesOnly(t *testing.T) {
	t.Setenv("API_DISCOVERY", "none")
	s := bootWithNotes(t)
	s.signUpAdmin("modes@integration.test")

	if answer := s.get("/api/notes"); answer.status != http.StatusNotFound {
		t.Errorf("discovery none still serves notes: %d", answer.status)
	}
	if answer := s.get("/api/users"); answer.status != http.StatusOK {
		t.Errorf("a registered resource is always served: %d", answer.status)
	}
	if answer := gql(t, s, "{ notes { id } }"); !strings.Contains(answer.raw, "Cannot query field") {
		t.Errorf("GraphQL still serves notes: %s", flat(answer.raw))
	}
}

func TestDiscoveryModeAllowlistServesTheListedTables(t *testing.T) {
	t.Setenv("API_DISCOVERY", "allowlist")
	t.Setenv("API_DISCOVERY_TABLES", "notes, jobs")
	s := bootWithNotes(t)
	s.signUpAdmin("allow@integration.test")

	if answer := s.get("/api/notes"); answer.status != http.StatusOK {
		t.Errorf("an allow-listed table is not served: %d", answer.status)
	}
	if answer := s.get("/api/jobs"); answer.status != http.StatusNotFound {
		t.Errorf("an internal table is served because it was allow-listed: %d", answer.status)
	}
}

func TestAnUnknownDiscoveryModeStopsTheBoot(t *testing.T) {
	root, found := projectRoot()
	if !found {
		t.Skip("no project root found")
	}
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DB_SQLITE_PATH", filepath.Join(t.TempDir(), "test.db"))
	t.Setenv("AUTH_SECRET", "an-integration-secret-long-enough-to-sign")
	t.Setenv("APP_ENV", "testing")
	t.Setenv("LOG_LEVEL", "error")
	t.Setenv("API_DISCOVERY", "everything")

	app, err := omega.Boot()
	if err != nil {
		t.Fatalf("boot: %v", err)
	}
	t.Cleanup(func() { _ = app.Shutdown() })

	if err := routes.RegisterAPI(app); err == nil || !strings.Contains(err.Error(), "discovery mode") {
		t.Fatalf("an unknown discovery mode must stop the boot: %v", err)
	}
}

func TestMembersOnlyReachTheirOwnAccount(t *testing.T) {
	s := boot(t)
	s.signUp("other@integration.test")
	otherID := userID(t, s, "other@integration.test")
	s.token = ""
	s.signUp("self@integration.test")
	selfID := userID(t, s, "self@integration.test")

	listed := s.get("/api/users")
	if listed.status != http.StatusOK || len(listed.list()) != 1 || contains(listed.raw, "other@integration.test") {
		t.Fatalf("a member lists other accounts: %d %s", listed.status, truncate(listed.raw))
	}
	if meta, _ := listed.body["meta"].(map[string]any); meta["total"] != float64(1) {
		t.Errorf("the total counts other accounts: %v", meta)
	}

	for _, answer := range []response{
		s.get(fmt.Sprintf("/api/users/%d", otherID)),
		s.patch(fmt.Sprintf("/api/users/%d", otherID), map[string]any{"name": "Pris"}),
	} {
		if answer.status != http.StatusNotFound {
			t.Errorf("another account must answer 404, got %d — %s", answer.status, truncate(answer.raw))
		}
	}

	graphql := gql(t, s, fmt.Sprintf(`{ users { email } users_count other: user(id: "%d") { email } }`, otherID))
	if contains(graphql.raw, "other@integration.test") || !contains(flat(graphql.raw), `"users_count": 1`) {
		t.Errorf("GraphQL lets a member reach another account: %s", flat(graphql.raw))
	}

	updated := s.patch(fmt.Sprintf("/api/users/%d", selfID), map[string]any{"name": "Renamed", "role": "admin"})
	if updated.status != http.StatusOK {
		t.Fatalf("a member updates their own account: %d %s", updated.status, truncate(updated.raw))
	}
	data, _ := updated.body["data"].(map[string]any)
	if data["name"] != "Renamed" || data["role"] != "user" {
		t.Errorf("a member promoted themselves or lost the update: %v", data)
	}

	mutation := gql(t, s, fmt.Sprintf(`mutation { update_user(id: "%d", role: "admin") { role } }`, selfID))
	if contains(mutation.raw, `"admin"`) {
		t.Errorf("GraphQL lets a member promote themselves: %s", flat(mutation.raw))
	}

	s.token = ""
	s.signUpAdmin("chief@integration.test")
	if all := s.get("/api/users"); len(all.list()) != 3 {
		t.Errorf("users.any must see every account: %d", len(all.list()))
	}
	promoted := s.patch(fmt.Sprintf("/api/users/%d", otherID), map[string]any{"role": "admin"})
	if data, _ := promoted.body["data"].(map[string]any); data["role"] != "admin" {
		t.Errorf("an admin writes the guarded role: %d %s", promoted.status, truncate(promoted.raw))
	}
}

func TestTheGraphQLRouteIsGuarded(t *testing.T) {
	s := boot(t)
	s.signUp("guarded@integration.test")

	aliases := make([]string, 0, 20)
	for i := range 20 {
		aliases = append(aliases, fmt.Sprintf("a%d: users { id }", i))
	}
	if answer := gql(t, s, "{ "+strings.Join(aliases, " ")+" }"); answer.status != http.StatusBadRequest {
		t.Errorf("alias amplification: %d, want 400 — %s", answer.status, truncate(answer.raw))
	}

	smuggled := s.post("/graphql?query=%7Busers%7Bemail%7D%7D", map[string]any{"query": "{ __schema { queryType { name } } }"})
	if smuggled.status != http.StatusBadRequest {
		t.Errorf("a query smuggled in the URL: %d, want 400", smuggled.status)
	}
}

func TestPostgresDecimalsRoundTripExactly(t *testing.T) {
	s := bootOn(t, engines[0])

	if err := s.app.DB.Exec(`CREATE TABLE ledgers (
		id BIGSERIAL PRIMARY KEY,
		amount NUMERIC(36,18) NOT NULL
	)`).Error; err != nil {
		t.Fatalf("ledgers: %v", err)
	}
	t.Cleanup(func() { s.app.DB.Exec(`DROP TABLE IF EXISTS ledgers`) })

	registry := api.NewRegistry(s.app.DB)
	if err := registry.Discover(s.app.DB); err != nil {
		t.Fatal(err)
	}
	registry.AllowWrites("ledgers")
	field := registry.Find("ledgers").Fields[1]
	if !field.Decimal || field.Precision != 36 || field.Scale != 18 {
		t.Fatalf("NUMERIC(36,18) is not described as such: %+v", field)
	}
	app := fiber.New()
	registry.Mount(app.Group("/api"))

	for _, amount := range []string{
		"123456789012345678.123456789012345678",
		"0.000000000000000001",
		"-999999999999999999.999999999999999999",
	} {
		body, _ := json.Marshal(map[string]any{"amount": amount})
		request := httptest.NewRequest(http.MethodPost, "/api/ledgers", bytes.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		response, err := app.Test(request, fiber.TestConfig{Timeout: 0, FailOnTimeout: false})
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := io.ReadAll(response.Body)
		_ = response.Body.Close()
		if response.StatusCode != http.StatusCreated {
			t.Fatalf("%s: status %d — %s", amount, response.StatusCode, raw)
		}

		var answer struct {
			Data struct {
				Amount any `json:"amount"`
			} `json:"data"`
		}
		_ = json.Unmarshal(raw, &answer)
		text, isText := answer.Data.Amount.(string)
		want, _ := new(big.Rat).SetString(amount)
		got, ok := new(big.Rat).SetString(text)
		if !isText || !ok || got.Cmp(want) != 0 {
			t.Errorf("%s came back as %v", amount, answer.Data.Amount)
		}
	}
}
