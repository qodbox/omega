package integration

import (
	"net/http"
	"strings"
	"testing"
)

func TestInternalTablesAreNotExposedAsResources(t *testing.T) {
	s := boot(t)
	s.signUpAdmin("probe@integration.test")

	for _, table := range []string{"jobs", "refresh_tokens", "migrations"} {
		answer := s.get("/api/" + table)
		if answer.status != http.StatusNotFound {
			t.Errorf("/api/%s est expose: status = %d, want 404 — %s", table, answer.status, truncate(answer.raw))
		}
	}
}

func TestTheQueueTableCannotBeWrittenThroughTheAPI(t *testing.T) {
	s := boot(t)
	s.signUpAdmin("injector@integration.test")

	answer := s.post("/api/jobs", map[string]any{
		"queue": "default", "handler": "arbitrary", "payload": "{}", "attempts": 0,
	})
	if answer.status != http.StatusNotFound && answer.status != http.StatusForbidden {
		t.Fatalf("injection de tache via l'API: status = %d — %s", answer.status, truncate(answer.raw))
	}
}

func TestDriverErrorsNeverReachTheClient(t *testing.T) {
	s := boot(t)
	s.signUpAdmin("leak@integration.test")

	answer := s.post("/api/users", map[string]any{"name": "Sans", "email": "sans@integration.test"})

	for _, marker := range []string{"NOT NULL", "constraint", "users.password", "sqlite", "SQLSTATE"} {
		if strings.Contains(strings.ToLower(answer.raw), strings.ToLower(marker)) {
			t.Errorf("l'erreur du driver fuit au client (%q): %s", marker, truncate(answer.raw))
		}
	}
}

func truncate(raw string) string {
	if len(raw) > 220 {
		return raw[:220] + "…"
	}
	return raw
}
