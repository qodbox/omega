package api

import (
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
