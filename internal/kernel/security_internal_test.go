package kernel

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/rs/zerolog"

	"omega/internal/auth"
)

func TestCORSRefusesAWildcardWithCredentials(t *testing.T) {
	if err := checkCORS([]string{"https://a.test", "*"}, true); err == nil || !strings.Contains(err.Error(), "CORS_ORIGINS") {
		t.Errorf("wildcard with credentials = %v", err)
	}
	if err := checkCORS([]string{"https://*.a.test"}, true); err == nil {
		t.Error("a pattern origin with credentials was accepted")
	}
	if err := checkCORS([]string{"*"}, false); err != nil {
		t.Errorf("wildcard without credentials = %v", err)
	}
	if err := checkCORS([]string{"https://a.test"}, true); err != nil {
		t.Errorf("explicit origin = %v", err)
	}
}

func TestBootHTTPRefusesAWildcardOrigin(t *testing.T) {
	app := &App{Cfg: covConfig(t, "cors:\n  origins: \"*\"\n  credentials: true\n"), Log: zerolog.Nop()}
	if err := app.bootHTTP(app.Cfg); err == nil {
		t.Fatal("bootHTTP accepted a wildcard origin with credentials")
	}
}

func TestBootHTTPWarnsAboutAnUntrustedProxyHeader(t *testing.T) {
	app := &App{Cfg: covConfig(t, "proxy_header: X-Real-IP\ntrusted_proxies: \"10.0.0.1, 10.0.0.2\"\n"), Log: zerolog.Nop()}
	if got := trustedProxies(app.Cfg); len(got) != 2 || got[1] != "10.0.0.2" {
		t.Errorf("trusted proxies = %v", got)
	}
	if err := app.bootHTTP(app.Cfg); err != nil {
		t.Fatal(err)
	}
	if got := app.Origins(); len(got) != 1 || got[0] != "http://localhost:5173" {
		t.Errorf("origins = %v", got)
	}
}

func TestLimiterReadsItsSection(t *testing.T) {
	app := &App{Cfg: covConfig(t, "ratelimit:\n  refresh:\n    max: 2\n    window: 1m\n  register:\n    max: 0\n")}

	server := fiber.New()
	server.Post("/refresh", app.Limiter("refresh"), func(c fiber.Ctx) error { return c.SendStatus(fiber.StatusOK) })
	server.Post("/register", app.Limiter("register"), func(c fiber.Ctx) error { return c.SendStatus(fiber.StatusOK) })
	server.Post("/other", app.Limiter("other"), func(c fiber.Ctx) error { return c.SendStatus(fiber.StatusOK) })

	status := func(path string) int {
		result, err := server.Test(httptest.NewRequest(http.MethodPost, path, nil), fiber.TestConfig{Timeout: 0, FailOnTimeout: false})
		if err != nil {
			t.Fatal(err)
		}
		return result.StatusCode
	}

	for i := 0; i < 2; i++ {
		if got := status("/refresh"); got != http.StatusOK {
			t.Fatalf("refresh %d: %d", i, got)
		}
	}
	if got := status("/refresh"); got != http.StatusTooManyRequests {
		t.Errorf("third refresh: %d, want 429", got)
	}
	for i := 0; i < 5; i++ {
		if got := status("/register"); got != http.StatusOK {
			t.Errorf("a limiter with max 0 limited: %d", got)
		}
	}
	if got := status("/other"); got != http.StatusOK {
		t.Errorf("unknown section with no default: %d", got)
	}
}

func TestSecurityHeadersAndNoStore(t *testing.T) {
	server := fiber.New()
	server.Use(securityHeaders(covConfig(t, "env: production\n")))
	server.Use(noStore())
	server.Get("/api/public", func(c fiber.Ctx) error { return c.JSON(fiber.Map{}) })
	server.Get("/api/private", func(c fiber.Ctx) error {
		c.Locals(auth.LocalsClaims, auth.Claims{})
		return c.JSON(fiber.Map{})
	})
	server.Get("/api/auth/me", func(c fiber.Ctx) error { return c.JSON(fiber.Map{}) })
	server.Get("/app", func(c fiber.Ctx) error { return c.SendString("spa") })

	get := func(path string) http.Header {
		result, err := server.Test(httptest.NewRequest(http.MethodGet, path, nil), fiber.TestConfig{Timeout: 0, FailOnTimeout: false})
		if err != nil {
			t.Fatal(err)
		}
		return result.Header
	}

	public := get("/api/public")
	if public.Get("Content-Security-Policy") != APICSP || public.Get("Strict-Transport-Security") != HSTS {
		t.Errorf("public API headers = %v", public)
	}
	if public.Get("Cache-Control") != "" {
		t.Errorf("an anonymous answer was marked no-store")
	}
	if get("/api/private").Get("Cache-Control") != "no-store" {
		t.Error("an authenticated answer may be cached")
	}
	if get("/api/auth/me").Get("Cache-Control") != "no-store" {
		t.Error("an auth answer may be cached")
	}

	spa := get("/app")
	if spa.Get("Content-Security-Policy") != "" {
		t.Error("the API policy leaked onto a non-API path")
	}
	if spa.Get("X-Frame-Options") != "DENY" {
		t.Error("the framing protection is missing outside the API")
	}
}
