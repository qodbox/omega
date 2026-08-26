package routes

import (
	"fmt"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"

	"omega"
	controller "omega/app/controllers/auth"
	"omega/app/models"
	"omega/app/policies"
	service "omega/app/services/auth"
	"omega/internal/api"
	jwt "omega/internal/auth"
)

type authSettings struct {
	Secret      string        `mapstructure:"secret"`
	AccessTTL   time.Duration `mapstructure:"access_ttl"`
	RefreshTTL  time.Duration `mapstructure:"refresh_ttl"`
	CallbackURL string        `mapstructure:"callback_url"`
	Providers   []jwt.Social  `mapstructure:"providers"`
}

func registerAuth(app *omega.App, group fiber.Router, registry *api.Registry) (*jwt.Guard, error) {
	var settings authSettings
	if err := app.Cfg.Unmarshal("auth", &settings); err != nil {
		return nil, fmt.Errorf("routes: auth config: %w", err)
	}
	if strings.TrimSpace(settings.Secret) == "" {
		return nil, fmt.Errorf("routes: auth.secret is empty — set AUTH_SECRET (openssl rand -base64 48)")
	}

	guard := jwt.New(jwt.Config{
		Secret:     settings.Secret,
		AccessTTL:  settings.AccessTTL,
		RefreshTTL: settings.RefreshTTL,
		Store:      jwt.NewStore(app.DB),
		Loader:     userLoader(app),
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
	social := controller.NewSocialController(service, providers)

	signin := group.Group("/auth")

	signin.Get("/providers", social.Providers).Name("auth.providers")
	signin.Get("/me", guard.Required(), local.Me).Name("auth.me")

	throttle := app.CredentialLimiter()

	signin.Post("/register", throttle, local.Register).Name("auth.register")
	signin.Post("/login", throttle, local.Login).Name("auth.login")
	signin.Post("/refresh", throttle, local.Refresh).Name("auth.refresh")
	signin.Post("/logout", local.Logout).Name("auth.logout")
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
