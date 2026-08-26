package policy

import (
	"github.com/gofiber/fiber/v2"
)

const Local = "policy_actor"

type Resolver func(c *fiber.Ctx) any

func (r *Registry) Middleware(resolve Resolver) func(string) fiber.Handler {
	return func(ability string) fiber.Handler {
		return func(c *fiber.Ctx) error {
			actor := resolve(c)
			if actor == nil {
				return fiber.NewError(fiber.StatusUnauthorized, "Authentication required.")
			}
			if !r.Allows(c.UserContext(), actor, ability) {
				return fiber.NewError(fiber.StatusForbidden, "You may not "+ability+".")
			}
			c.Locals(Local, actor)
			return c.Next()
		}
	}
}
