package api

import (
	"context"
	"strings"

	"github.com/gofiber/fiber/v3"
)

type Ability func(string) fiber.Handler

// Authorize asks the policies "<plural>.<action>" before a generated route
// runs. On a route mounted by a Registry that has a gate (Protect), the row
// actions — view, update, delete — are left to the handler, which asks the very
// same question once the row is loaded, with the row as subject: a rule
// written as Define("orders.update", func(ctx, actor, order) bool) then sees
// the order, not nil.
func Authorize(can Ability) fiber.Handler {
	return func(c fiber.Ctx) error {
		resource, action := abilityOf(c)
		if resource == "" {
			return c.Next()
		}
		if registry, ok := c.Locals(registryLocal{}).(*Registry); ok && registry.gated() {
			switch action {
			case ActionView, ActionUpdate, ActionDelete:
				return c.Next()
			}
		}
		return can(resource + "." + action)(c)
	}
}

func abilityOf(c fiber.Ctx) (string, string) {
	name := c.Route().Name
	if !strings.HasPrefix(name, "api.") {
		return "", ""
	}

	rest := strings.TrimPrefix(name, "api.")
	resource, action, found := strings.Cut(rest, ".")
	if !found {
		return "", ""
	}

	switch action {
	case "index":
		return resource, "list"
	case "show":
		return resource, "view"
	case "store":
		return resource, "create"
	case "update":
		return resource, "update"
	case "destroy":
		return resource, "delete"
	default:
		return "", ""
	}
}

// Gate answers whether an actor holds an ability, optionally over a subject. It
// is the shape of policy.Registry.Allows, kept as a function so this package
// does not have to depend on the policy package.
type Gate func(ctx context.Context, actor any, ability string, subject ...any) bool

// ActorFrom pulls the authenticated actor out of a request context. The REST
// handlers receive a fiber.Ctx and are guarded by Authorize, but GraphQL
// reaches the resolvers through the net/http adaptor, where all that survives is
// the request context — which still carries whatever fiber stored in Locals.
type ActorFrom func(ctx context.Context) any

// Protect gives the GraphQL resolvers the same authorisation the REST routes get
// from Authorize. Until this is called, the resolvers refuse everything: a
// schema mounted without a gate used to serve every registered table to any
// authenticated caller, whatever the policies said.
func (r *Registry) Protect(gate Gate, actor ActorFrom) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.gate = gate
	r.actorFrom = actor
}
