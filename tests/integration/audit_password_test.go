package integration

import (
	"net/http"
	"testing"
)

func TestChangePasswordRotatesTheSession(t *testing.T) {
	s := boot(t)
	s.signUp("motdepasse@integration.test")
	previous := s.token

	answer := s.post("/api/auth/password", map[string]any{
		"current_password": "correct-horse-battery",
		"new_password":     "un-nouveau-mot-de-passe",
	})
	if answer.status != http.StatusOK {
		t.Fatalf("status = %d, want 200 — %s", answer.status, truncate(answer.raw))
	}

	tokens, _ := answer.body["tokens"].(map[string]any)
	fresh, _ := tokens["access_token"].(string)
	if fresh == "" {
		t.Fatalf("aucun jeton emis apres le changement: %s", truncate(answer.raw))
	}

	s.token = fresh
	if me := s.get("/api/auth/me"); me.status != http.StatusOK {
		t.Fatalf("le jeton emis ne fonctionne pas: %d", me.status)
	}
	_ = previous

	s.token = ""
	if login := s.post("/api/auth/login", map[string]any{
		"email": "motdepasse@integration.test", "password": "un-nouveau-mot-de-passe",
	}); login.status != http.StatusOK {
		t.Errorf("le nouveau mot de passe ne fonctionne pas: %d", login.status)
	}
	if login := s.post("/api/auth/login", map[string]any{
		"email": "motdepasse@integration.test", "password": "correct-horse-battery",
	}); login.status == http.StatusOK {
		t.Error("l'ancien mot de passe fonctionne encore")
	}
}

func TestChangePasswordRefusesAWrongCurrentPassword(t *testing.T) {
	s := boot(t)
	s.signUp("mauvais@integration.test")

	answer := s.post("/api/auth/password", map[string]any{
		"current_password": "ce-n-est-pas-le-bon",
		"new_password":     "un-nouveau-mot-de-passe",
	})
	if answer.status != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422 — %s", answer.status, truncate(answer.raw))
	}
}

func TestChangePasswordRevokesTheOldRefreshTokens(t *testing.T) {
	s := boot(t)
	registered := s.signUp("revocation@integration.test")

	tokens, _ := registered.body["tokens"].(map[string]any)
	oldRefresh, _ := tokens["refresh_token"].(string)

	if answer := s.post("/api/auth/password", map[string]any{
		"current_password": "correct-horse-battery",
		"new_password":     "un-nouveau-mot-de-passe",
	}); answer.status != http.StatusOK {
		t.Fatalf("changement refuse: %d", answer.status)
	}

	replay := s.post("/api/auth/refresh", map[string]any{"refresh_token": oldRefresh})
	if replay.status == http.StatusOK {
		t.Fatal("un jeton de rafraichissement anterieur au changement fonctionne encore")
	}
}

func TestChangePasswordNeedsAToken(t *testing.T) {
	s := boot(t)

	answer := s.post("/api/auth/password", map[string]any{
		"current_password": "x", "new_password": "un-nouveau-mot-de-passe",
	})
	if answer.status != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", answer.status)
	}
}
