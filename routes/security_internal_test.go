package routes

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
)

func TestCheckSecret(t *testing.T) {
	cases := map[string]struct {
		secret, env string
		ok          bool
	}{
		"empty":                  {"", "testing", false},
		"short in testing":       {"short", "testing", true},
		"short in production":    {"short", "production", false},
		"long passphrase":        {strings.Repeat("pass-", 8), "production", true},
		"base64 of 24 bytes":     {"YWJjZGVmZ2hpamtsbW5vcHFyc3R1dnd4", "local", false},
		"base64 of 48 bytes":     {"q3m1Kc0x8b0e2u4n6Q9t1y3r5w7z9A2c4E6g8I0k2M4o6Q8s0U2w4Y6a8C0e2G4i", "production", true},
		"base64 padded 33 bytes": {"YWJjZGVmZ2hpamtsbW5vcHFyc3R1dnd4eXphYmNkZWZn", "production", true},
	}
	for name, c := range cases {
		err := checkSecret(c.secret, c.env)
		if (err == nil) != c.ok {
			t.Errorf("%s: err = %v, want ok=%v", name, err, c.ok)
		}
	}
}

func TestFlag(t *testing.T) {
	if !flag("", true) || flag("", false) || !flag("true", false) || flag("0", true) || !flag("nonsense", true) {
		t.Error("flag does not read an optional boolean")
	}
}

func TestCheckOrigin(t *testing.T) {
	app := fiber.New()
	app.Get("/ws", checkOrigin([]string{"https://App.example.test/"}), func(c fiber.Ctx) error {
		return c.SendStatus(fiber.StatusOK)
	})

	for origin, want := range map[string]int{
		"":                          http.StatusOK,
		"https://app.example.test":  http.StatusOK,
		"http://example.com":        http.StatusOK, // same origin as the request below
		"https://evil.example.test": http.StatusForbidden,
		"null":                      http.StatusForbidden,
	} {
		request := httptest.NewRequest(http.MethodGet, "http://example.com/ws", nil)
		if origin != "" {
			request.Header.Set("Origin", origin)
		}
		result, err := app.Test(request, fiber.TestConfig{Timeout: 0, FailOnTimeout: false})
		if err != nil {
			t.Fatal(err)
		}
		if result.StatusCode != want {
			t.Errorf("Origin %q: status = %d, want %d", origin, result.StatusCode, want)
		}
	}
}

func TestStaticBearer(t *testing.T) {
	app := fiber.New()
	app.Get("/", func(c fiber.Ctx) error {
		if staticBearer(c, "s3cret") {
			return c.SendStatus(fiber.StatusOK)
		}
		return c.SendStatus(fiber.StatusUnauthorized)
	})

	for header, want := range map[string]int{
		"Bearer s3cret": http.StatusOK,
		"bearer s3cret": http.StatusOK,
		"Bearer s3cre":  http.StatusUnauthorized,
		"Basic s3cret":  http.StatusUnauthorized,
		"":              http.StatusUnauthorized,
	} {
		request := httptest.NewRequest(http.MethodGet, "/", nil)
		request.Header.Set("Authorization", header)
		result, err := app.Test(request, fiber.TestConfig{Timeout: 0, FailOnTimeout: false})
		if err != nil {
			t.Fatal(err)
		}
		if result.StatusCode != want {
			t.Errorf("%q: status = %d, want %d", header, result.StatusCode, want)
		}
	}
}
