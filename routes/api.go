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
	registry.SetLimits(apiLimits(app))

	// A member reaches their own account only (Owner on id); users.any — held
	// by admins through the wildcard — lifts the scope. The role and the
	// verification date are written by admins alone.
	api.RegisterWith[models.User](registry, "users", "user",
		api.Hidden("password"),
		api.Owner("id"),
		api.Guarded("role", "email_verified_at"),
	)

	discovery, err := api.ResolveDiscovery(app.Cfg.Env(),
		app.Cfg.String("api.discovery"), app.Cfg.Get("api.discovery_tables"))
	if err != nil {
		return fmt.Errorf("routes: %w", err)
	}
	if _, err := registry.DiscoverWith(app.DB, discovery); err != nil {
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
	group.Get("/stats", func(c fiber.Ctx) error {
		return c.JSON(app.Metrics.Snapshot())
	}).Name("api.stats")
	group.Get("/events", guard.Required(), app.Broadcast.Stream(0)).Name("api.events")
	group.Get("/ws", guard.Required(), broadcast.Upgrade(), app.Broadcast.Socket(nil)).Name("api.ws")

	group.Get("/health", health(app)).Name("api.health")
	group.Get("/openapi.json", func(c fiber.Ctx) error {
		return c.JSON(registry.OpenAPI(
			app.Cfg.StringOr("app.name", "Omega")+" API", omega.Version, "/api"))
	}).Name("api.openapi")

	can := app.Policy.Middleware(func(c fiber.Ctx) any { return guard.User(c) })
	omega.Bind("can", can)

	// REST and GraphQL ask the same gate. Authorize answers list and create
	// up front; the handlers and the resolvers answer view, update and delete
	// once the row is loaded, with the row as policy subject, and scope owned
	// resources to the actor. Fiber's Locals are fasthttp user values, which
	// both the REST handlers and the net/http adaptor read from the request
	// context, so resolveActor must run first.
	registry.Protect(
		app.Policy.Allows,
		func(ctx context.Context) any { return ctx.Value(auth.LocalsUser) },
	)

	registry.Mount(group, guard.Required(), resolveActor(guard), api.Authorize(can))

	return registerGraphQL(app, registry, guard)
}

func apiLimits(app *omega.App) api.Limits {
	return api.Limits{
		MaxFilters:       app.Cfg.Int("api.limits.max_filters"),
		MaxInValues:      app.Cfg.Int("api.limits.max_in_values"),
		MaxPatternLength: app.Cfg.Int("api.limits.max_pattern_length"),
		MaxWildcards:     app.Cfg.Int("api.limits.max_wildcards"),
		MaxDepth:         app.Cfg.Int("api.graphql.max_depth"),
		MaxFields:        app.Cfg.Int("api.graphql.max_fields"),
		MaxRootFields:    app.Cfg.Int("api.graphql.max_root_fields"),
		MaxComplexity:    app.Cfg.Int("api.graphql.max_complexity"),
	}
}

func health(app *omega.App) fiber.Handler {
	return func(c fiber.Ctx) error {
		status := fiber.Map{"status": "ok", "version": omega.Version}

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
		Post("/graphql", registry.GraphQLGuard(), graphqlAuth(app, guard), resolveActor(guard), adaptor.HTTPHandler(handler)).
		Name("graphql")
	return nil
}

// resolveActor forces the authenticated user into Locals before the generated
// handlers run. Guard.User resolves lazily, and neither the GraphQL resolvers
// nor the registry can call it: all they hold is a context, not a fiber.Ctx.
func resolveActor(guard *auth.Guard) fiber.Handler {
	return func(c fiber.Ctx) error {
		guard.User(c)
		return c.Next()
	}
}

func graphqlAuth(app *omega.App, guard *auth.Guard) fiber.Handler {
	required := guard.Required()
	public := app.Cfg.BoolOr("api.public_introspection", true)

	return func(c fiber.Ctx) error {
		if public && api.IntrospectionOnly(c.Body()) {
			return c.Next()
		}
		return required(c)
	}
}
