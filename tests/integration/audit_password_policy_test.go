package integration

import (
	"net/http"
	"testing"
)

func TestRegisterRefusesACommonPassword(t *testing.T) {
	s := boot(t)

	for _, weak := range []string{"password", "12345678", "motdepasse", "azertyuiop", "PASSWORD", "  password1  "} {
		answer := s.post("/api/auth/register", map[string]any{
			"name": "Faible", "email": "faible@integration.test", "password": weak,
		})
		if answer.status != http.StatusUnprocessableEntity {
			t.Errorf("%q accepte: status = %d, want 422", weak, answer.status)
		}
	}
}

func TestRegisterAcceptsASolidPassword(t *testing.T) {
	s := boot(t)

	answer := s.post("/api/auth/register", map[string]any{
		"name": "Solide", "email": "solide@integration.test", "password": "correct-horse-battery",
	})
	if answer.status != http.StatusCreated && answer.status != http.StatusOK {
		t.Fatalf("status = %d — %s", answer.status, truncate(answer.raw))
	}
}

func TestTheCommonPasswordErrorNamesTheField(t *testing.T) {
	s := boot(t)

	answer := s.post("/api/auth/register", map[string]any{
		"name": "Faible", "email": "champ@integration.test", "password": "password",
	})

	fields, _ := answer.body["errors"].(map[string]any)
	message, _ := fields["password"].(string)
	if message == "" {
		t.Fatalf("le 422 ne designe pas le champ password: %s", truncate(answer.raw))
	}
}

func TestChangePasswordRefusesACommonOne(t *testing.T) {
	s := boot(t)
	s.signUp("politique@integration.test")

	answer := s.post("/api/auth/password", map[string]any{
		"current_password": "correct-horse-battery",
		"new_password":     "password123",
	})
	if answer.status != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422 — %s", answer.status, truncate(answer.raw))
	}
}

func TestChangePasswordRefusesReusingTheCurrentOne(t *testing.T) {
	s := boot(t)
	s.signUp("reutilisation@integration.test")

	answer := s.post("/api/auth/password", map[string]any{
		"current_password": "correct-horse-battery",
		"new_password":     "correct-horse-battery",
	})
	if answer.status != http.StatusUnprocessableEntity {
		t.Fatalf("reutiliser le mot de passe actuel: status = %d, want 422", answer.status)
	}
}
