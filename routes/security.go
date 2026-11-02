package routes

import (
	"crypto/subtle"
	"strings"

	"github.com/gofiber/fiber/v3"

	"omega"
	jwt "omega/internal/auth"
)

// Abilities that open the operational endpoints to a signed-in user. Admins
// hold every ability; grant these to another role to share them.
const (
	AbilityMetrics = "metrics.view"
	AbilityDocs    = "docs.view"
)

// metricsGate guards /api/metrics and /api/stats. Prometheus presents the
// static token from observability.metrics_token as a bearer token; a person
// signs in and needs the metrics.view ability. With neither configured the
// endpoints are not served at all (enabled=false).
func metricsGate(app *omega.App, guard *jwt.Guard) (gate fiber.Handler, enabled bool) {
	token := strings.TrimSpace(app.Cfg.String("observability.metrics_token"))
	admins := flag(app.Cfg.String("observability.metrics_admins"), true)
	if token == "" && !admins {
		return nil, false
	}

	return func(c fiber.Ctx) error {
		if token != "" && staticBearer(c, token) {
			return c.Next()
		}
		if !admins {
			return fiber.NewError(fiber.StatusUnauthorized, "Authentication required.")
		}
		if err := guard.Authenticate(c); err != nil {
			return err
		}
		if !app.Policy.Allows(c.Context(), guard.User(c), AbilityMetrics) {
			return fiber.NewError(fiber.StatusForbidden, "This account may not read the metrics.")
		}
		return c.Next()
	}, true
}

// staticBearer compares the bearer token with a configured secret in
// constant time.
func staticBearer(c fiber.Ctx, secret string) bool {
	header := strings.TrimSpace(c.Get(fiber.HeaderAuthorization))
	if len(header) < 7 || !strings.EqualFold(header[:7], "bearer ") {
		return false
	}
	presented := strings.TrimSpace(header[7:])
	return subtle.ConstantTimeCompare([]byte(presented), []byte(secret)) == 1
}

// publicDocs reads api.docs.public: on by default in local and testing, off
// elsewhere.
func publicDocs(app *omega.App) bool {
	return flag(app.Cfg.String("api.docs.public"), devEnv(app))
}

// publicIntrospection reads api.public_introspection, with the same default.
func publicIntrospection(app *omega.App) bool {
	return flag(app.Cfg.String("api.public_introspection"), devEnv(app))
}

// docsGate hides the API consoles and the OpenAPI document unless they are
// public: a caller holding docs.view still reaches them, anyone else gets a
// 404, as if they did not exist.
func docsGate(app *omega.App, guard *jwt.Guard) fiber.Handler {
	if publicDocs(app) {
		return func(c fiber.Ctx) error { return c.Next() }
	}
	return func(c fiber.Ctx) error {
		if guard.Authenticate(c) != nil || !app.Policy.Allows(c.Context(), guard.User(c), AbilityDocs) {
			return fiber.ErrNotFound
		}
		return c.Next()
	}
}

// checkOrigin refuses a WebSocket upgrade from a page on another site. The
// browser sends the session cookie with the upgrade whatever page opened it,
// and the WebSocket handshake is not covered by CORS: without this check any
// site could open a socket as the signed-in user (cross-site WebSocket
// hijacking). Clients that send no Origin are not browsers and pass.
func checkOrigin(origins []string) fiber.Handler {
	allowed := make(map[string]bool, len(origins))
	for _, origin := range origins {
		allowed[strings.TrimSuffix(strings.ToLower(origin), "/")] = true
	}

	return func(c fiber.Ctx) error {
		origin := strings.TrimSuffix(strings.ToLower(c.Get(fiber.HeaderOrigin)), "/")
		if origin == "" || allowed[origin] || origin == strings.ToLower(c.Scheme()+"://"+c.Host()) {
			return c.Next()
		}
		return fiber.NewError(fiber.StatusForbidden, "This origin may not open a socket here.")
	}
}
