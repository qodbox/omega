package integration

import (
	"fmt"
	"net/http"
	"testing"
)

func aqsCovLogin(s *server, email, password string) response {
	return s.post("/api/auth/login", map[string]any{"email": email, "password": password})
}

func TestAqsCovAccountThrottleBlocksAfterTheConfiguredMax(t *testing.T) {
	t.Setenv("RATELIMIT_AUTH_MAX", "0")
	t.Setenv("RATELIMIT_ACCOUNT_MAX", "3")
	t.Setenv("RATELIMIT_ACCOUNT_WINDOW", "15m")

	s := boot(t)
	s.signUp("compte@integration.test")
	s.token = ""

	for i := 0; i < 3; i++ {
		answer := aqsCovLogin(s, "compte@integration.test", fmt.Sprintf("faux-%d", i))
		if answer.status != http.StatusUnauthorized {
			t.Fatalf("tentative %d: status = %d, want 401 — %s", i, answer.status, answer.raw)
		}
	}

	blocked := aqsCovLogin(s, "compte@integration.test", "correct-horse-battery")
	if blocked.status != http.StatusTooManyRequests {
		t.Fatalf("apres 3 echecs: status = %d, want 429 — %s", blocked.status, blocked.raw)
	}
}

func TestAqsCovASuccessfulLoginClearsTheAccountThrottle(t *testing.T) {
	t.Setenv("RATELIMIT_AUTH_MAX", "0")
	t.Setenv("RATELIMIT_ACCOUNT_MAX", "4")
	t.Setenv("RATELIMIT_ACCOUNT_WINDOW", "15m")

	s := boot(t)
	s.signUp("remise@integration.test")
	s.token = ""

	for i := 0; i < 3; i++ {
		if answer := aqsCovLogin(s, "remise@integration.test", "faux"); answer.status != http.StatusUnauthorized {
			t.Fatalf("tentative %d: status = %d, want 401", i, answer.status)
		}
	}

	if answer := aqsCovLogin(s, "remise@integration.test", "correct-horse-battery"); answer.status != http.StatusOK {
		t.Fatalf("connexion valide: status = %d, want 200 — %s", answer.status, answer.raw)
	}

	for i := 0; i < 3; i++ {
		if answer := aqsCovLogin(s, "remise@integration.test", "faux"); answer.status != http.StatusUnauthorized {
			t.Fatalf("le compteur n'a pas ete remis a zero, tentative %d: status = %d, want 401", i, answer.status)
		}
	}
}

func TestAqsCovAnUnknownAccountIsThrottledToo(t *testing.T) {
	t.Setenv("RATELIMIT_AUTH_MAX", "0")
	t.Setenv("RATELIMIT_ACCOUNT_MAX", "2")
	t.Setenv("RATELIMIT_ACCOUNT_WINDOW", "15m")

	s := boot(t)

	for i := 0; i < 2; i++ {
		if answer := aqsCovLogin(s, "fantome@integration.test", "faux"); answer.status != http.StatusUnauthorized {
			t.Fatalf("tentative %d: status = %d, want 401", i, answer.status)
		}
	}

	blocked := aqsCovLogin(s, "fantome@integration.test", "faux")
	if blocked.status != http.StatusTooManyRequests {
		t.Fatalf("compte inexistant: status = %d, want 429 — %s", blocked.status, blocked.raw)
	}
}

func TestAqsCovTheAccountThrottleIsDisabledWhenMaxIsZero(t *testing.T) {
	t.Setenv("RATELIMIT_AUTH_MAX", "0")
	t.Setenv("RATELIMIT_ACCOUNT_MAX", "0")

	s := boot(t)
	s.signUp("libre@integration.test")
	s.token = ""

	for i := 0; i < 12; i++ {
		if answer := aqsCovLogin(s, "libre@integration.test", "faux"); answer.status != http.StatusUnauthorized {
			t.Fatalf("tentative %d: status = %d, want 401", i, answer.status)
		}
	}
}

func TestAqsCovTheAccountThrottleIgnoresTheEmailCasing(t *testing.T) {
	t.Setenv("RATELIMIT_AUTH_MAX", "0")
	t.Setenv("RATELIMIT_ACCOUNT_MAX", "2")
	t.Setenv("RATELIMIT_ACCOUNT_WINDOW", "15m")

	s := boot(t)
	s.signUp("casse@integration.test")
	s.token = ""

	for i := 0; i < 2; i++ {
		if answer := aqsCovLogin(s, "casse@integration.test", "faux"); answer.status != http.StatusUnauthorized {
			t.Fatalf("tentative %d: status = %d, want 401", i, answer.status)
		}
	}

	blocked := aqsCovLogin(s, "CASSE@Integration.TEST", "faux")
	if blocked.status != http.StatusTooManyRequests {
		t.Fatalf("changer la casse contourne la limite: status = %d, want 429 — %s", blocked.status, blocked.raw)
	}
}
