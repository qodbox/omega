package routes

import (
	"fmt"

	"github.com/gofiber/adaptor/v2"
	"github.com/gofiber/fiber/v2"
	graphqlhandler "github.com/graphql-go/handler"

	"omega"
	"omega/app/models"
	"omega/internal/api"
	"omega/internal/auth"
	"omega/internal/broadcast"
)

func RegisterAPI(app *omega.App) error {
	registry := api.NewRegistry(app.DB)

	api.Register[models.User](registry, "users", "user", "password")

	if err := registry.Discover(app.DB); err != nil {
		return fmt.Errorf("routes: discovering the schema: %w", err)
	}

	omega.Bind("api", registry)

	group := app.Router.Group("/api")

	guard, err := registerAuth(app, group, registry)
	if err != nil {
		return err
	}

	registerDocs(app, group)

	registerCurrency(app, group, guard, registry)

	if err := registerBilling(app, group, guard, registry); err != nil {
		return err
	}

	if app.Cfg.Env() != "testing" {
		if err := RegisterWorkers(app); err != nil {
			return err
		}
	}

	group.Get("/metrics", app.Metrics.Handler()).Name("api.metrics")
	group.Get("/stats", func(c *fiber.Ctx) error {
		return c.JSON(app.Metrics.Snapshot())
	}).Name("api.stats")
	group.Get("/events", guard.Required(), app.Broadcast.Stream(0)).Name("api.events")
	group.Get("/ws", guard.Required(), broadcast.Upgrade(), app.Broadcast.Socket(nil)).Name("api.ws")

	group.Get("/health", health(app)).Name("api.health")
	group.Get("/openapi.json", func(c *fiber.Ctx) error {
		return c.JSON(registry.OpenAPI(
			app.Cfg.StringOr("app.name", "Omega")+" API", omega.Version, "/api"))
	}).Name("api.openapi")

	can := app.Policy.Middleware(func(c *fiber.Ctx) any { return guard.User(c) })
	omega.Bind("can", can)

	registry.Mount(group, guard.Required(), api.Authorize(can))

	return registerGraphQL(app, registry, guard)
}

func health(app *omega.App) fiber.Handler {
	return func(c *fiber.Ctx) error {
		status := fiber.Map{"status": "ok", "version": omega.Version}

		if app.DB != nil {
			sqlDB, err := app.DB.DB()
			if err != nil || sqlDB.PingContext(c.UserContext()) != nil {
				status["status"] = "degraded"
				status["database"] = "unreachable"
				return c.Status(fiber.StatusServiceUnavailable).JSON(status)
			}
			status["database"] = "ok"
		}
		return c.JSON(status)
	}
}

func registerDocs(app *omega.App, group fiber.Router) {
	docs := api.Docs{
		Title:       app.Cfg.StringOr("app.name", "Omega"),
		OpenAPIPath: "/api/openapi.json",
		GraphQLPath: "/graphql",
		AssetPrefix: "/api/docs/assets",
	}

	group.Get("/docs", docs.SwaggerUI()).Name("api.docs")
	group.Get("/docs/assets/:file", docs.Asset()).Name("api.docs.assets")
	app.Router.App().Get("/graphql", docs.GraphiQL()).Name("graphiql")
}

func registerGraphQL(app *omega.App, registry *api.Registry, guard *auth.Guard) error {
	schema, err := registry.Schema()
	if err != nil {
		return err
	}

	handler := graphqlhandler.New(&graphqlhandler.Config{
		Schema:     &schema,
		Pretty:     true,
		GraphiQL:   false,
		Playground: false,
	})

	app.Router.App().
		Post("/graphql", graphqlAuth(app, guard), adaptor.HTTPHandler(handler)).
		Name("graphql")
	return nil
}

func graphqlAuth(app *omega.App, guard *auth.Guard) fiber.Handler {
	required := guard.Required()
	public := app.Cfg.BoolOr("api.public_introspection", true)

	return func(c *fiber.Ctx) error {
		if public && api.IntrospectionOnly(c.Body()) {
			return c.Next()
		}
		return required(c)
	}
}
