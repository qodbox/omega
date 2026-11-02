package kernel

import (
	"fmt"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/helmet"
	"github.com/gofiber/fiber/v3/middleware/limiter"

	"omega/internal/auth"
)

// APICSP is the policy of every JSON answer: it loads nothing and may not be
// framed. A page that does render (the API consoles) sets its own.
const APICSP = "default-src 'none'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'"

// PermissionsPolicy turns off the powerful features nothing here uses.
const PermissionsPolicy = "accelerometer=(), camera=(), geolocation=(), gyroscope=(), " +
	"magnetometer=(), microphone=(), payment=(), usb=()"

// HSTS is sent outside local development: two years, subdomains included.
const HSTS = "max-age=63072000; includeSubDomains"

// securityHeaders sets the browser hardening headers on every answer. The
// strict CSP applies to the API (/api, /graphql); other paths, such as a
// single-page application served from the same binary, set their own.
func securityHeaders(conf *Config) fiber.Handler {
	headers := helmet.New(helmet.Config{
		ContentTypeNosniff:        "nosniff",
		XFrameOptions:             "DENY",
		ReferrerPolicy:            "strict-origin-when-cross-origin",
		CrossOriginOpenerPolicy:   "same-origin",
		CrossOriginResourcePolicy: "same-site",
		// require-corp would block a SPA served alongside from embedding
		// third-party images; the API answers JSON and needs no isolation.
		CrossOriginEmbedderPolicy: "unsafe-none",
		PermissionPolicy:          PermissionsPolicy,
		// HSTS is set below, whatever the scheme the proxy forwarded.
		HSTSMaxAge: 0,
	})
	hsts := !conf.IsLocal()

	return func(c fiber.Ctx) error {
		if hsts {
			c.Set(fiber.HeaderStrictTransportSecurity, HSTS)
		}
		if isAPIPath(c.Path()) {
			c.Set(fiber.HeaderContentSecurityPolicy, APICSP)
		}
		return headers(c)
	}
}

func isAPIPath(path string) bool {
	return path == "/api" || strings.HasPrefix(path, "/api/") ||
		path == "/graphql" || strings.HasPrefix(path, "/graphql/")
}

// noStore keeps credentials and personal data out of shared and browser
// caches: everything under /api/auth, and any answer to an authenticated
// request that did not choose its own caching.
func noStore() fiber.Handler {
	return func(c fiber.Ctx) error {
		err := c.Next()

		path := c.Path()
		authPath := path == "/api/auth" || strings.HasPrefix(path, "/api/auth/")
		_, authenticated := auth.ClaimsOf(c)

		if authPath || (authenticated && len(c.Response().Header.Peek(fiber.HeaderCacheControl)) == 0) {
			c.Set(fiber.HeaderCacheControl, "no-store")
			c.Set(fiber.HeaderPragma, "no-cache")
		}
		return err
	}
}

// checkCORS refuses a wildcard origin together with credentials: that would
// let any site read a signed-in user's answers.
func checkCORS(origins []string, credentials bool) error {
	if !credentials {
		return nil
	}
	for _, origin := range origins {
		if strings.Contains(origin, "*") {
			return fmt.Errorf("kernel: app.cors.origins contains %q while app.cors.credentials is on — "+
				"list the allowed origins explicitly (CORS_ORIGINS), or turn credentials off", origin)
		}
	}
	return nil
}

// trustedProxies reads app.trusted_proxies, a list or a comma-separated
// string.
func trustedProxies(conf *Config) []string {
	out := []string{}
	for _, entry := range conf.StringSlice("app.trusted_proxies") {
		out = append(out, splitList(entry)...)
	}
	return out
}

// Origins is the CORS allow-list, for the checks that must agree with it
// (the WebSocket upgrade).
func (a *App) Origins() []string {
	return splitList(a.Cfg.StringOr("app.cors.origins", "http://localhost:5173"))
}

// Limiter is a per-address limiter configured under app.ratelimit.<name>
// (max, window). A max of 0 turns it off.
func (a *App) Limiter(name string) fiber.Handler {
	defaults := map[string]struct {
		max    int
		window time.Duration
	}{
		"register": {10, 10 * time.Minute},
		"refresh":  {30, time.Minute},
	}[name]

	max := a.Cfg.IntOr("app.ratelimit."+name+".max", defaults.max)
	if max <= 0 {
		return func(c fiber.Ctx) error { return c.Next() }
	}

	window := a.Cfg.Duration("app.ratelimit." + name + ".window")
	if window <= 0 {
		window = defaults.window
	}
	if window <= 0 {
		window = time.Minute
	}

	return limiter.New(limiter.Config{
		Max:        max,
		Expiration: window,
		KeyGenerator: func(c fiber.Ctx) string {
			return name + ":" + c.IP()
		},
		LimitReached: func(c fiber.Ctx) error {
			return fiber.NewError(fiber.StatusTooManyRequests, "Too many requests. Try again later.")
		},
	})
}
