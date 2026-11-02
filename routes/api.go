package routes

import (
	"context"
	"fmt"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/adaptor"
	graphqlhandler "github.com/graphql-go/handler"

	"omega"
	"omega/app/models"
	"omega/internal/api"
	"omega/internal/auth"
	"omega/internal/broadcast"
)

func RegisterAPI(app *omega.App) error {
	registry := api.NewRegistry(app.DB)

	api.Register[models.User](registry, "users", "user", "password", "token_version")

	if err := registry.Discover(app.DB); err != nil {
		return fmt.Errorf("routes: discovering the schema: %w", err)
	}

	omega.Bind("api", registry)

	group := app.Router.Group("/api")

	guard, err := registerAuth(app, group, registry)
	if err != nil {
		return err
	}

	registerDocs(app, group, guard)

	registerCurrency(app, group, guard, registry)

	if err := registerBilling(app, group, guard, registry); err != nil {
		return err
	}

	if app.Cfg.Env() != "testing" {
		if err := RegisterWorkers(app); err != nil {
			return err
		}
	}

	if gate, enabled := metricsGate(app, guard); enabled {
		group.Get("/metrics", gate, app.Metrics.Handler()).Name("api.metrics")
		group.Get("/stats", gate, func(c fiber.Ctx) error {
			return c.JSON(app.Metrics.Snapshot())
		}).Name("api.stats")
	}
	group.Get("/events", guard.Required(), app.Broadcast.Stream(0)).Name("api.events")
	group.Get("/ws", checkOrigin(app.Origins()), guard.Required(), broadcast.Upgrade(),
		app.Broadcast.Socket(nil)).Name("api.ws")

	group.Get("/health", health(app)).Name("api.health")
	group.Get("/openapi.json", docsGate(app, guard), func(c fiber.Ctx) error {
		return c.JSON(registry.OpenAPI(
			app.Cfg.StringOr("app.name", "Omega")+" API", omega.Version, "/api"))
	}).Name("api.openapi")

	can := app.Policy.Middleware(func(c fiber.Ctx) any { return guard.User(c) })
	omega.Bind("can", can)

	registry.Mount(group, guard.Required(), api.Authorize(can))

	// GraphQL serves the same registry, so it has to ask the same policies. REST
	// gets that from the Authorize middleware; the resolvers get it from here.
	// Fiber's Locals are fasthttp user values, which the net/http adaptor leaves
	// on the request context, so the actor the middleware resolved is still
	// readable once the resolvers run.
	registry.Protect(
		app.Policy.Allows,
		func(ctx context.Context) any { return ctx.Value(auth.LocalsUser) },
	)

	return registerGraphQL(app, registry, guard)
}

func health(app *omega.App) fiber.Handler {
	return func(c fiber.Ctx) error {
		status := fiber.Map{"status": "ok"}
		if devEnv(app) {
			// The exact version tells an attacker which advisories apply.
			status["version"] = omega.Version
		}

		if app.DB != nil {
			sqlDB, err := app.DB.DB()
			if err != nil || sqlDB.PingContext(c.Context()) != nil {
				status["status"] = "degraded"
				status["database"] = "unreachable"
				return c.Status(fiber.StatusServiceUnavailable).JSON(status)
			}
			status["database"] = "ok"
		}
		return c.JSON(status)
	}
}

func registerDocs(app *omega.App, group fiber.Router, guard *auth.Guard) {
	docs := api.Docs{
		Title:       app.Cfg.StringOr("app.name", "Omega"),
		OpenAPIPath: "/api/openapi.json",
		GraphQLPath: "/graphql",
		AssetPrefix: "/api/docs/assets",
	}

	gate := docsGate(app, guard)
	group.Get("/docs", gate, docs.SwaggerUI()).Name("api.docs")
	group.Get("/docs/assets/:file", gate, docs.Asset()).Name("api.docs.assets")
	app.Router.App().Get("/graphql", gate, docs.GraphiQL()).Name("graphiql")
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
		Post("/graphql", graphqlAuth(app, guard), graphqlActor(guard), adaptor.HTTPHandler(handler)).
		Name("graphql")
	return nil
}

// graphqlActor forces the authenticated user into Locals before the request
// crosses into net/http. Guard.User resolves lazily, and the resolvers cannot
// call it: all they hold is a context, not a fiber.Ctx.
func graphqlActor(guard *auth.Guard) fiber.Handler {
	return func(c fiber.Ctx) error {
		guard.User(c)
		return c.Next()
	}
}

func graphqlAuth(app *omega.App, guard *auth.Guard) fiber.Handler {
	required := guard.Required()
	public := publicIntrospection(app)

	return func(c fiber.Ctx) error {
		if public && api.IntrospectionOnly(c.Body()) {
			return c.Next()
		}
		return required(c)
	}
}
