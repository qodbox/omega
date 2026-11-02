package routes

import (
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"

	"omega"
	controller "omega/app/controllers/auth"
	"omega/app/models"
	"omega/app/policies"
	service "omega/app/services/auth"
	"omega/internal/api"
	jwt "omega/internal/auth"
)

type authSettings struct {
	Secret      string         `mapstructure:"secret"`
	AccessTTL   time.Duration  `mapstructure:"access_ttl"`
	RefreshTTL  time.Duration  `mapstructure:"refresh_ttl"`
	ReuseGrace  time.Duration  `mapstructure:"reuse_grace"`
	CallbackURL string         `mapstructure:"callback_url"`
	Providers   []jwt.Social   `mapstructure:"providers"`
	Cookies     cookieSettings `mapstructure:"cookies"`
}

type cookieSettings struct {
	Enabled  string `mapstructure:"enabled"`
	Domain   string `mapstructure:"domain"`
	Secure   string `mapstructure:"secure"`
	SameSite string `mapstructure:"same_site"`
}

// MinSecretBytes is the shortest signing key accepted outside the tests: an
// HS256 key shorter than the hash output weakens every token.
const MinSecretBytes = 32

// checkSecret refuses a missing or short signing secret. A secret written as
// standard base64 (what openssl rand -base64 produces) is measured decoded.
func checkSecret(secret, env string) error {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return fmt.Errorf("routes: auth.secret is empty — set AUTH_SECRET (openssl rand -base64 48)")
	}
	if env == "testing" {
		return nil
	}

	size := len(secret)
	if decoded, err := base64.StdEncoding.DecodeString(secret); err == nil {
		size = len(decoded)
	}
	if size < MinSecretBytes {
		return fmt.Errorf("routes: auth.secret is too weak (%d bytes, at least %d required) — "+
			"set AUTH_SECRET to a random value (openssl rand -base64 48)", size, MinSecretBytes)
	}
	return nil
}

// cookieConfig reads auth.cookies. Secure defaults to on outside local and
// testing, and SameSite=None without Secure is refused, since browsers drop it.
func cookieConfig(app *omega.App, settings cookieSettings) (jwt.Cookies, error) {
	cookies := jwt.Cookies{
		Enabled:  flag(settings.Enabled, true),
		Domain:   strings.TrimSpace(settings.Domain),
		Secure:   flag(settings.Secure, !devEnv(app)),
		SameSite: strings.TrimSpace(settings.SameSite),
	}
	switch strings.ToLower(cookies.SameSite) {
	case "", "lax", "strict":
	case "none":
		if !cookies.Secure {
			return cookies, fmt.Errorf("routes: auth.cookies.same_site=None requires auth.cookies.secure")
		}
	default:
		return cookies, fmt.Errorf("routes: auth.cookies.same_site must be Lax, Strict or None, not %q", cookies.SameSite)
	}
	return cookies, nil
}

// flag reads an optional boolean setting: empty means the default.
func flag(raw string, fallback bool) bool {
	if parsed, err := strconv.ParseBool(strings.TrimSpace(raw)); err == nil {
		return parsed
	}
	return fallback
}

// devEnv is local development or the test suite: the environments where the
// docs are public and cookies may travel over plain HTTP.
func devEnv(app *omega.App) bool {
	return app.Cfg.IsLocal() || app.Cfg.Env() == "testing"
}

func registerAuth(app *omega.App, group fiber.Router, registry *api.Registry) (*jwt.Guard, error) {
	var settings authSettings
	if err := app.Cfg.Unmarshal("auth", &settings); err != nil {
		return nil, fmt.Errorf("routes: auth config: %w", err)
	}
	if err := checkSecret(settings.Secret, app.Cfg.Env()); err != nil {
		return nil, err
	}
	cookies, err := cookieConfig(app, settings.Cookies)
	if err != nil {
		return nil, err
	}

	guard := jwt.New(jwt.Config{
		Secret:     settings.Secret,
		AccessTTL:  settings.AccessTTL,
		RefreshTTL: settings.RefreshTTL,
		Store:      jwt.NewStore(app.DB),
		Loader:     userLoader(app),
		Cookies:    cookies,
		ReuseGrace: settings.ReuseGrace,
		OnReuse: func(subject, family string) {
			app.Log.Warn().Str("event", "refresh_token_reuse").Str("subject", subject).Str("family", family).
				Msg("security: a rotated refresh token was replayed — the whole login family was revoked")
		},
	})
	omega.Bind("auth", guard)

	policies.Register(app.Policy)

	providers, err := useProviders(settings)
	if err != nil {
		return nil, err
	}
	if len(providers) > 0 {
		app.Log.Info().Strs("providers", providers).Msg("oauth providers enabled")
	}

	service := service.NewService(app.DB, guard, app.Events).
		Throttle(jwt.NewAttempts(
			app.DB,
			app.Cfg.IntOr("app.ratelimit.account.max", 8),
			app.Cfg.Duration("app.ratelimit.account.window"),
		))
	local := controller.NewController(service, guard)
	social := controller.NewSocialController(service, providers).WithGuard(guard)

	signin := group.Group("/auth")

	signin.Get("/providers", social.Providers).Name("auth.providers")
	signin.Get("/csrf", local.CSRF).Name("auth.csrf")
	signin.Get("/me", guard.Required(), local.Me).Name("auth.me")

	throttle := app.CredentialLimiter()
	csrf := guard.CSRF()

	signin.Post("/register", app.Limiter("register"), throttle, local.Register).Name("auth.register")
	signin.Post("/login", throttle, local.Login).Name("auth.login")
	signin.Post("/refresh", app.Limiter("refresh"), csrf, local.Refresh).Name("auth.refresh")
	signin.Post("/logout", csrf, local.Logout).Name("auth.logout")
	signin.Post("/logout-all", guard.Required(), local.LogoutAll).Name("auth.logout_all")
	signin.Post("/password", throttle, guard.Required(), local.ChangePassword).Name("auth.password")

	signin.Get("/:provider", social.Redirect).Name("auth.provider.redirect")
	signin.Get("/:provider/callback", social.Callback).Name("auth.provider.callback")

	documentAuth(registry, providers)
	return guard, nil
}

func useProviders(settings authSettings) ([]string, error) {
	base := strings.TrimSuffix(settings.CallbackURL, "/")

	socials := make([]jwt.Social, 0, len(settings.Providers))
	for _, social := range settings.Providers {
		if social.Callback == "" {
			social.Callback = base + "/api/auth/" + strings.ToLower(social.Name) + "/callback"
		}
		socials = append(socials, social)
	}
	return jwt.UseProviders(socials)
}

func userLoader(app *omega.App) jwt.Loader {
	return func(subject string) (any, error) {
		var user models.User
		if err := app.DB.Where("id = ?", subject).Take(&user).Error; err != nil {
			return nil, err
		}
		return &user, nil
	}
}
