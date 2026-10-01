package api

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/gofiber/fiber/v2"
)

type Ability func(string) fiber.Handler

func Authorize(can Ability) fiber.Handler {
	return func(c *fiber.Ctx) error {
		resource, action := abilityOf(c)
		if resource == "" {
			return c.Next()
		}
		return can(resource + "." + action)(c)
	}
}

func abilityOf(c *fiber.Ctx) (string, string) {
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
// handlers receive a *fiber.Ctx and are guarded by Authorize, but GraphQL
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

// permits mirrors abilityOf's naming, so GraphQL and REST ask the policies the
// very same question: "<plural>.<action>".
func (r *Registry) permits(ctx context.Context, resource *Resource, action string) error {
	r.mu.RLock()
	gate, actorFrom := r.gate, r.actorFrom
	r.mu.RUnlock()

	ability := resource.Plural + "." + action

	if gate == nil || actorFrom == nil {
		return fmt.Errorf("graphql: no policy gate is configured, refusing %s", ability)
	}

	actor := actorFrom(ctx)
	if actor == nil {
		return errors.New("graphql: authentication required")
	}

	if !gate(ctx, actor, ability) {
		return fmt.Errorf("you may not %s", ability)
	}
	return nil
}
