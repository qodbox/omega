package integration

import (
	"net/http"
	"testing"
)

func TestHealthIsPublic(t *testing.T) {
	s := boot(t)

	answer := s.get("/api/health")

	if answer.status != http.StatusOK {
		t.Fatalf("status = %d, want 200", answer.status)
	}
	if answer.body["status"] != "ok" {
		t.Errorf("status field = %v", answer.body["status"])
	}
}

func TestOpenAPIDescribesTheRoutesItServes(t *testing.T) {
	s := boot(t)

	answer := s.get("/api/openapi.json")
	if answer.status != http.StatusOK {
		t.Fatalf("status = %d, want 200", answer.status)
	}

	paths, ok := answer.body["paths"].(map[string]any)
	if !ok {
		t.Fatal("the document has no paths")
	}
	for _, path := range []string{"/users", "/users/{id}", "/auth/login", "/health"} {
		if _, described := paths[path]; !described {
			t.Errorf("%s is missing from the document", path)
		}
	}

	components, _ := answer.body["components"].(map[string]any)
	schemes, _ := components["securitySchemes"].(map[string]any)
	if _, declared := schemes["bearerAuth"]; !declared {
		t.Error("the bearer scheme is not declared")
	}
}

func TestConsolesAreServed(t *testing.T) {
	s := boot(t)

	for _, path := range []string{"/api/docs", "/graphql", "/api/metrics"} {
		if answer := s.get(path); answer.status != http.StatusOK {
			t.Errorf("%s: status = %d, want 200", path, answer.status)
		}
	}
}

func TestListIsPaginatedAndFiltered(t *testing.T) {
	s := boot(t)
	s.signUp("list@integration.test")

	answer := s.get("/api/users?per_page=1")
	if answer.status != http.StatusOK {
		t.Fatalf("status = %d, want 200", answer.status)
	}
	if rows := answer.list(); len(rows) != 1 {
		t.Errorf("rows = %d, want 1", len(rows))
	}

	meta, _ := answer.body["meta"].(map[string]any)
	if meta["per_page"] != float64(1) {
		t.Errorf("per_page = %v", meta["per_page"])
	}

	filtered := s.get("/api/users?email=list@integration.test")
	if rows := filtered.list(); len(rows) != 1 {
		t.Errorf("filtered rows = %d, want 1", len(rows))
	}
}

func TestHiddenColumnsNeverLeak(t *testing.T) {
	s := boot(t)
	s.signUp("hidden@integration.test")

	answer := s.get("/api/users")
	for _, row := range answer.list() {
		record, _ := row.(map[string]any)
		for _, column := range []string{"password", "deleted_at"} {
			if _, leaked := record[column]; leaked {
				t.Errorf("%s reached the client", column)
			}
		}
	}

	oracle := s.get("/api/users?password=whatever")
	if oracle.status != http.StatusUnprocessableEntity {
		t.Errorf("filtering on a hidden column: status = %d, want 422", oracle.status)
	}
}

func TestSoftDeleteHidesTheRowEverywhere(t *testing.T) {
	s := boot(t)
	s.signUp("victim@integration.test")
	s.token = ""
	s.signUpAdmin("remover@integration.test")

	created := s.get("/api/users?email=victim@integration.test")
	rows := created.list()
	if len(rows) != 1 {
		t.Fatalf("the account was not created")
	}
	record, _ := rows[0].(map[string]any)
	id := int(record["id"].(float64))

	if answer := s.delete("/api/users/" + itoa(id)); answer.status != http.StatusNoContent {
		t.Fatalf("delete: status = %d, want 204", answer.status)
	}

	if answer := s.get("/api/users/" + itoa(id)); answer.status != http.StatusNotFound {
		t.Errorf("the deleted row is still readable: status = %d", answer.status)
	}

	graphql := s.post("/graphql", map[string]any{"query": "{ users { email } }"})
	if graphql.status != http.StatusOK {
		t.Fatalf("graphql: status = %d", graphql.status)
	}
	if contains(graphql.raw, "victim@integration.test") {
		t.Error("the deleted row is still visible through GraphQL")
	}
}

func TestGraphQLIntrospectionIsOpenButDataIsNot(t *testing.T) {
	s := boot(t)

	introspection := s.post("/graphql", map[string]any{
		"query": "{ __schema { queryType { name } } }",
	})
	if introspection.status != http.StatusOK {
		t.Errorf("introspection: status = %d, want 200", introspection.status)
	}

	data := s.post("/graphql", map[string]any{"query": "{ users { email } }"})
	if data.status != http.StatusUnauthorized {
		t.Errorf("data without a token: status = %d, want 401", data.status)
	}

	mixed := s.post("/graphql", map[string]any{
		"query": "{ __schema { queryType { name } } users { email } }",
	})
	if mixed.status != http.StatusUnauthorized {
		t.Errorf("introspection mixed with data: status = %d, want 401", mixed.status)
	}
}

func TestUnknownRouteAnswersJSON(t *testing.T) {
	s := boot(t)

	answer := s.get("/api/nowhere")

	if answer.status != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", answer.status)
	}
	if answer.body["error"] == nil {
		t.Errorf("the 404 is not a JSON error: %s", answer.raw)
	}
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	digits := ""
	for value > 0 {
		digits = string(rune('0'+value%10)) + digits
		value /= 10
	}
	return digits
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}

func TestPoliciesGovernTheGeneratedRoutes(t *testing.T) {
	s := boot(t)
	s.signUp("member@integration.test")

	if answer := s.get("/api/users"); answer.status != http.StatusOK {
		t.Errorf("a member may list: status = %d, want 200", answer.status)
	}
	if answer := s.get("/api/users/1"); answer.status != http.StatusOK {
		t.Errorf("a member may read: status = %d, want 200", answer.status)
	}

	created := s.post("/api/users", map[string]any{"name": "New", "email": "new@integration.test"})
	if created.status != http.StatusForbidden {
		t.Errorf("a member created a record: status = %d, want 403", created.status)
	}
	if removed := s.delete("/api/users/1"); removed.status != http.StatusForbidden {
		t.Errorf("a member deleted a record: status = %d, want 403", removed.status)
	}

	s.token = ""
	s.signUpAdmin("boss@integration.test")
	if removed := s.delete("/api/users/1"); removed.status != http.StatusNoContent {
		t.Errorf("an admin was refused: status = %d, want 204", removed.status)
	}
}
