package integration

import (
	"fmt"
	"net/http"
	"testing"
)

func TestLoginIsThrottledAgainstBruteForce(t *testing.T) {
	t.Setenv("RATELIMIT_AUTH_MAX", "5")
	s := boot(t)
	s.signUp("victime@integration.test")
	s.token = ""

	blocked := 0
	for i := 0; i < 25; i++ {
		answer := s.post("/api/auth/login", map[string]any{
			"email": "victime@integration.test", "password": fmt.Sprintf("essai-%d", i),
		})
		if answer.status == http.StatusTooManyRequests {
			blocked++
		}
	}

	if blocked == 0 {
		t.Fatal("25 tentatives de mot de passe n'ont declenche aucun 429")
	}
}

func TestRotatingTheEmailDoesNotEscapeTheThrottle(t *testing.T) {
	t.Setenv("RATELIMIT_AUTH_MAX", "5")
	s := boot(t)

	blocked := 0
	for i := 0; i < 25; i++ {
		answer := s.post("/api/auth/login", map[string]any{
			"email": fmt.Sprintf("cible-%d@integration.test", i), "password": "peu-importe",
		})
		if answer.status == http.StatusTooManyRequests {
			blocked++
		}
	}

	if blocked == 0 {
		t.Fatal("enumerer les emails depuis une meme adresse contourne la limite")
	}
}

func TestRegisterAndPasswordAreThrottledToo(t *testing.T) {
	t.Setenv("RATELIMIT_AUTH_MAX", "3")
	s := boot(t)

	for _, route := range []string{"/api/auth/register", "/api/auth/refresh"} {
		blocked := 0
		for i := 0; i < 15; i++ {
			answer := s.post(route, map[string]any{"email": fmt.Sprintf("x%d@t.test", i), "password": "x"})
			if answer.status == http.StatusTooManyRequests {
				blocked++
			}
		}
		if blocked == 0 {
			t.Errorf("%s n'est pas limite", route)
		}
	}
}

func TestThrottleCanBeDisabled(t *testing.T) {
	t.Setenv("RATELIMIT_AUTH_MAX", "0")
	t.Setenv("RATELIMIT_ACCOUNT_MAX", "0")
	s := boot(t)

	for i := 0; i < 15; i++ {
		answer := s.post("/api/auth/login", map[string]any{"email": "a@t.test", "password": "b"})
		if answer.status == http.StatusTooManyRequests {
			t.Fatalf("limite active alors que RATELIMIT_AUTH_MAX=0 (tentative %d)", i)
		}
	}
}

func TestThrottleDoesNotBlockNormalUse(t *testing.T) {
	s := boot(t)
	s.signUp("normal@integration.test")

	if answer := s.get("/api/auth/me"); answer.status != http.StatusOK {
		t.Fatalf("un usage normal est bloque: status = %d", answer.status)
	}
}

func TestOneAccountIsThrottledAcrossManyAddresses(t *testing.T) {
	t.Setenv("RATELIMIT_AUTH_MAX", "0")
	t.Setenv("RATELIMIT_ACCOUNT_MAX", "4")
	s := boot(t)
	s.signUp("cible@integration.test")
	s.token = ""

	blocked := 0
	for i := 0; i < 12; i++ {
		answer := s.post("/api/auth/login", map[string]any{
			"email": "cible@integration.test", "password": fmt.Sprintf("essai-%d", i),
		})
		if answer.status == http.StatusTooManyRequests {
			blocked++
		}
	}

	if blocked == 0 {
		t.Fatal("aucun 429 alors que la limite par compte est active et celle par IP desactivee")
	}
}

func TestASuccessfulLoginClearsTheAccountCounter(t *testing.T) {
	t.Setenv("RATELIMIT_AUTH_MAX", "0")
	t.Setenv("RATELIMIT_ACCOUNT_MAX", "4")
	s := boot(t)
	s.signUp("reussite@integration.test")
	s.token = ""

	for i := 0; i < 3; i++ {
		s.post("/api/auth/login", map[string]any{
			"email": "reussite@integration.test", "password": "mauvais",
		})
	}

	if good := s.post("/api/auth/login", map[string]any{
		"email": "reussite@integration.test", "password": "correct-horse-battery",
	}); good.status != http.StatusOK {
		t.Fatalf("la connexion valide est refusee: %d — %s", good.status, truncate(good.raw))
	}

	for i := 0; i < 3; i++ {
		answer := s.post("/api/auth/login", map[string]any{
			"email": "reussite@integration.test", "password": "mauvais",
		})
		if answer.status == http.StatusTooManyRequests {
			t.Fatalf("le compteur n'a pas ete remis a zero apres une reussite (tentative %d)", i)
		}
	}
}

func TestAnotherAccountIsNotAffected(t *testing.T) {
	t.Setenv("RATELIMIT_AUTH_MAX", "0")
	t.Setenv("RATELIMIT_ACCOUNT_MAX", "3")
	s := boot(t)
	s.signUp("victime@integration.test")
	s.signUp("voisin@integration.test")
	s.token = ""

	for i := 0; i < 8; i++ {
		s.post("/api/auth/login", map[string]any{
			"email": "victime@integration.test", "password": "mauvais",
		})
	}

	if good := s.post("/api/auth/login", map[string]any{
		"email": "voisin@integration.test", "password": "correct-horse-battery",
	}); good.status != http.StatusOK {
		t.Fatalf("un compte voisin a ete bloque: %d — %s", good.status, truncate(good.raw))
	}
}
