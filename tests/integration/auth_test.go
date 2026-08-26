package integration

import (
	"net/http"
	"testing"
)

func TestRegisterReturnsUserAndTokens(t *testing.T) {
	s := boot(t)

	answer := s.signUp("ada@integration.test")

	if answer.status != http.StatusCreated {
		t.Fatalf("status = %d, want 201 — %s", answer.status, answer.raw)
	}
	if email := answer.data()["email"]; email != "ada@integration.test" {
		t.Errorf("email = %v", email)
	}
	if answer.tokens()["access_token"] == "" {
		t.Error("no access token in the answer")
	}
	if _, leaked := answer.data()["password"]; leaked {
		t.Error("the password hash reached the client")
	}
}

func TestRegisterRejectsAnInvalidBody(t *testing.T) {
	s := boot(t)

	answer := s.post("/api/auth/register", map[string]any{
		"name": "", "email": "not-an-email", "password": "short",
	})

	if answer.status != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", answer.status)
	}
	for _, field := range []string{"name", "email", "password"} {
		if _, reported := answer.errors()[field]; !reported {
			t.Errorf("%s was not reported: %v", field, answer.errors())
		}
	}
}

func TestRegisterRefusesADuplicateEmail(t *testing.T) {
	s := boot(t)
	s.signUp("twice@integration.test")

	answer := s.post("/api/auth/register", map[string]any{
		"name": "Other", "email": "twice@integration.test", "password": "correct-horse-battery",
	})

	if answer.status != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", answer.status)
	}
	if _, reported := answer.errors()["email"]; !reported {
		t.Error("the duplicate was not reported on the email field")
	}
}

func TestLoginRejectsAWrongPassword(t *testing.T) {
	s := boot(t)
	s.signUp("login@integration.test")
	s.token = ""

	answer := s.post("/api/auth/login", map[string]any{
		"email": "login@integration.test", "password": "wrong",
	})

	if answer.status != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", answer.status)
	}
}

func TestProtectedRoutesNeedAToken(t *testing.T) {
	s := boot(t)

	if answer := s.get("/api/users"); answer.status != http.StatusUnauthorized {
		t.Errorf("no token: status = %d, want 401", answer.status)
	}

	s.token = "forged.token.value"
	if answer := s.get("/api/users"); answer.status != http.StatusUnauthorized {
		t.Errorf("forged token: status = %d, want 401", answer.status)
	}

	s.token = ""
	s.signUp("guarded@integration.test")
	if answer := s.get("/api/users"); answer.status != http.StatusOK {
		t.Errorf("valid token: status = %d, want 200", answer.status)
	}
}

func TestMeReturnsTheBearer(t *testing.T) {
	s := boot(t)
	s.signUp("me@integration.test")

	answer := s.get("/api/auth/me")

	if answer.status != http.StatusOK {
		t.Fatalf("status = %d, want 200", answer.status)
	}
	if email := answer.data()["email"]; email != "me@integration.test" {
		t.Errorf("email = %v", email)
	}
}

func TestRefreshRotatesAndRefusesAReplay(t *testing.T) {
	s := boot(t)
	first := s.signUp("rotate@integration.test")
	refresh := first.tokens()["refresh_token"]

	rotated := s.post("/api/auth/refresh", map[string]any{"refresh_token": refresh})
	if rotated.status != http.StatusOK {
		t.Fatalf("first rotation: status = %d, want 200", rotated.status)
	}

	replayed := s.post("/api/auth/refresh", map[string]any{"refresh_token": refresh})
	if replayed.status != http.StatusUnauthorized {
		t.Fatalf("replay: status = %d, want 401", replayed.status)
	}
}

func TestLogoutRetiresTheRefreshToken(t *testing.T) {
	s := boot(t)
	session := s.signUp("bye@integration.test")
	refresh := session.tokens()["refresh_token"]

	if answer := s.post("/api/auth/logout", map[string]any{"refresh_token": refresh}); answer.status != http.StatusNoContent {
		t.Fatalf("logout: status = %d, want 204", answer.status)
	}

	answer := s.post("/api/auth/refresh", map[string]any{"refresh_token": refresh})
	if answer.status != http.StatusUnauthorized {
		t.Errorf("the retired token still works: status = %d", answer.status)
	}
}

func TestTheBearerPrefixIsForgiving(t *testing.T) {
	s := boot(t)
	s.signUp("prefix@integration.test")
	token := s.token

	for name, header := range map[string]string{
		"bare":   token,
		"double": "Bearer " + token,
	} {
		s.token = header
		if answer := s.get("/api/auth/me"); answer.status != http.StatusOK {
			t.Errorf("%s: status = %d, want 200", name, answer.status)
		}
	}
}
