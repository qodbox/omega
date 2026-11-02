package integration

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"

	"omega"
	"omega/routes"
)

func TestSecurityHeadersOnEveryAPIAnswer(t *testing.T) {
	s := boot(t)

	for _, path := range []string{"/api/health", "/api/does-not-exist"} {
		answer := s.get(path)
		want := map[string]string{
			"X-Content-Type-Options":       "nosniff",
			"X-Frame-Options":              "DENY",
			"Referrer-Policy":              "strict-origin-when-cross-origin",
			"Cross-Origin-Opener-Policy":   "same-origin",
			"Cross-Origin-Resource-Policy": "same-site",
			"Content-Security-Policy":      "default-src 'none'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'",
			"Strict-Transport-Security":    "max-age=63072000; includeSubDomains",
		}
		for header, value := range want {
			if got := answer.header.Get(header); got != value {
				t.Errorf("%s %s = %q, want %q", path, header, got, value)
			}
		}
		if !strings.Contains(answer.header.Get("Permissions-Policy"), "camera=()") {
			t.Errorf("%s Permissions-Policy = %q", path, answer.header.Get("Permissions-Policy"))
		}
	}
}

func TestNoHSTSInLocalDevelopment(t *testing.T) {
	s := bootWith(t, map[string]string{"APP_ENV": "local"})
	if got := s.get("/api/health").header.Get("Strict-Transport-Security"); got != "" {
		t.Errorf("HSTS in local = %q", got)
	}
}

func TestTheConsolesCarryAPolicyThatLetsThemRun(t *testing.T) {
	s := boot(t)

	for _, path := range []string{"/api/docs", "/graphql"} {
		policy := s.get(path).header.Get("Content-Security-Policy")
		for _, part := range []string{"script-src 'self' 'sha256-", "connect-src 'self'", "frame-ancestors 'none'"} {
			if !strings.Contains(policy, part) {
				t.Errorf("%s CSP %q lacks %q", path, policy, part)
			}
		}
		if strings.Contains(policy, "unsafe-eval") {
			t.Errorf("%s CSP allows eval", path)
		}
	}
}

func TestAuthenticatedJSONIsNotCached(t *testing.T) {
	s := boot(t)
	s.signUp("cache@integration.test")

	if got := s.get("/api/users").header.Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
}

func TestMetricsAreProtected(t *testing.T) {
	s := boot(t)

	for _, path := range []string{"/api/metrics", "/api/stats"} {
		if answer := s.get(path); answer.status != http.StatusUnauthorized {
			t.Errorf("anonymous %s: status = %d, want 401", path, answer.status)
		}
	}

	s.signUp("member@integration.test")
	if answer := s.get("/api/metrics"); answer.status != http.StatusForbidden {
		t.Errorf("member: status = %d, want 403", answer.status)
	}

	s.signUpAdmin("admin-metrics@integration.test")
	if answer := s.get("/api/metrics"); answer.status != http.StatusOK {
		t.Errorf("admin: status = %d, want 200", answer.status)
	}
}

func TestMetricsAcceptTheScraperToken(t *testing.T) {
	s := bootWith(t, map[string]string{"METRICS_TOKEN": "a-scraper-token-from-the-config"})

	s.token = "a-scraper-token-from-the-config"
	if answer := s.get("/api/metrics"); answer.status != http.StatusOK || !strings.Contains(answer.raw, "omega_uptime_seconds") {
		t.Errorf("with the token: status = %d", answer.status)
	}

	s.token = "a-scraper-token-from-the-confih"
	if answer := s.get("/api/metrics"); answer.status != http.StatusUnauthorized {
		t.Errorf("wrong token: status = %d, want 401", answer.status)
	}
}

func TestMetricsTokenOnlyWhenAdminsAreOff(t *testing.T) {
	s := bootWith(t, map[string]string{"METRICS_TOKEN": "only-the-scraper", "METRICS_ADMINS": "false"})
	s.signUpAdmin("no-metrics@integration.test")

	if answer := s.get("/api/metrics"); answer.status != http.StatusUnauthorized {
		t.Errorf("admin with admins off: status = %d, want 401", answer.status)
	}
}

func TestMetricsAreNotServedWithoutAnyWayIn(t *testing.T) {
	s := bootWith(t, map[string]string{"APP_ENV": "production", "METRICS_ADMINS": "false"})
	s.signUpAdmin("prod-metrics@integration.test")

	for _, path := range []string{"/api/metrics", "/api/stats"} {
		if answer := s.get(path); answer.status != http.StatusNotFound {
			t.Errorf("%s: status = %d, want 404", path, answer.status)
		}
	}
}

func TestProductionHidesTheDocsAndTheVersion(t *testing.T) {
	s := bootWith(t, map[string]string{"APP_ENV": "production"})

	for _, path := range []string{"/api/docs", "/api/docs/assets/swagger-ui.css", "/api/openapi.json", "/graphql"} {
		if answer := s.get(path); answer.status != http.StatusNotFound {
			t.Errorf("anonymous %s in production: status = %d, want 404", path, answer.status)
		}
	}

	introspection := s.post("/graphql", map[string]any{"query": "{ __schema { types { name } } }"})
	if introspection.status != http.StatusUnauthorized {
		t.Errorf("anonymous introspection in production: status = %d, want 401", introspection.status)
	}

	health := s.get("/api/health")
	if health.status != http.StatusOK || health.body["version"] != nil {
		t.Errorf("health in production = %d %s", health.status, health.raw)
	}

	s.signUp("member-docs@integration.test")
	if answer := s.get("/api/docs"); answer.status != http.StatusNotFound {
		t.Errorf("member: status = %d, want 404", answer.status)
	}

	s.signUpAdmin("admin-docs@integration.test")
	if answer := s.get("/api/openapi.json"); answer.status != http.StatusOK {
		t.Errorf("admin: status = %d, want 200", answer.status)
	}
}

func TestProductionCookiesAreSecure(t *testing.T) {
	s := bootWith(t, map[string]string{"APP_ENV": "production"})
	s.cookies = true

	answer := s.post("/api/auth/register", map[string]any{
		"name": "Prod", "email": "prod@integration.test", "password": password,
	})
	for _, name := range []string{"omega_access", "omega_refresh", "omega_csrf"} {
		if cookie := setCookie(answer, name); cookie == nil || !cookie.Secure {
			t.Errorf("%s is not Secure in production: %+v", name, cookie)
		}
	}
}

func TestDocsCanBeMadePublicInProduction(t *testing.T) {
	s := bootWith(t, map[string]string{"APP_ENV": "production", "API_DOCS_PUBLIC": "true", "API_PUBLIC_INTROSPECTION": "true"})

	if answer := s.get("/api/openapi.json"); answer.status != http.StatusOK {
		t.Errorf("status = %d, want 200", answer.status)
	}
	if answer := s.post("/graphql", map[string]any{"query": "{ __schema { types { name } } }"}); answer.status != http.StatusOK {
		t.Errorf("public introspection: status = %d, want 200", answer.status)
	}
}

func TestBootRefusesAWildcardOriginWithCredentials(t *testing.T) {
	root, found := projectRoot()
	if !found {
		t.Skip("no project root found")
	}
	t.Chdir(root)
	t.Setenv("CORS_ORIGINS", "https://app.example.test,*")
	t.Setenv("CORS_CREDENTIALS", "true")

	if _, err := omega.Boot(omega.WithoutDatabase()); err == nil || !strings.Contains(err.Error(), "app.cors.origins") {
		t.Fatalf("boot error = %v, want a refusal naming app.cors.origins", err)
	}
}

func TestBootRefusesAWeakSecretOutsideTests(t *testing.T) {
	for name, secret := range map[string]string{
		"short":           "too-short-a-secret",
		"short in base64": "c2hvcnQtYnV0LWJhc2U2NC1lbmNvZGVkLXNlY3JldA==", // 31 bytes once decoded
		"empty":           "",
	} {
		t.Run(name, func(t *testing.T) {
			root, found := projectRoot()
			if !found {
				t.Skip("no project root found")
			}
			t.Chdir(root)
			t.Setenv("DB_SQLITE_PATH", filepath.Join(t.TempDir(), "secret.db"))
			t.Setenv("LOG_LEVEL", "error")
			t.Setenv("APP_ENV", "production")
			t.Setenv("AUTH_SECRET", secret)

			app, err := omega.Boot()
			if err != nil {
				t.Fatalf("boot: %v", err)
			}
			defer func() { _ = app.Shutdown() }()

			err = routes.RegisterAPI(app)
			if err == nil || !strings.Contains(err.Error(), "AUTH_SECRET") {
				t.Fatalf("RegisterAPI = %v, want a refusal naming AUTH_SECRET", err)
			}
		})
	}
}

func TestBootAcceptsAStrongSecret(t *testing.T) {
	s := bootWith(t, map[string]string{
		"APP_ENV":     "production",
		"AUTH_SECRET": "q3m1Kc0x8b0e2u4n6Q9t1y3r5w7z9A2c4E6g8I0k2M4o6Q8s0U2w4Y6a8C0e2G4i",
	})
	if s.get("/api/health").status != http.StatusOK {
		t.Fatal("a strong secret was refused")
	}
}

func TestWebhookBodiesAreBounded(t *testing.T) {
	s := bootWith(t, map[string]string{
		"BILLING_ENABLED":       "true",
		"STRIPE_SECRET_KEY":     "sk_test_bounded",
		"STRIPE_WEBHOOK_SECRET": "whsec_bounded",
	})

	request := httptest.NewRequest(http.MethodPost, "/api/billing/webhook", strings.NewReader(strings.Repeat("x", 300*1024)))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Stripe-Signature", "t=1,v1=00")
	result, err := s.app.Fiber.Test(request, fiber.TestConfig{Timeout: 0, FailOnTimeout: false})
	if err != nil {
		t.Fatal(err)
	}
	if result.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("oversized webhook: status = %d, want 413", result.StatusCode)
	}

	stale := s.post("/api/billing/webhook", map[string]any{"id": "evt_1"})
	if stale.status != http.StatusBadRequest {
		t.Errorf("unsigned webhook: status = %d, want 400", stale.status)
	}
}
