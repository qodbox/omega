package router

import (
	"fmt"
	"net/url"
	"path"
	"strings"

	"github.com/gofiber/fiber/v2"
)

type Router struct {
	app *fiber.App
}

func New(app *fiber.App) *Router { return &Router{app: app} }

func (r *Router) App() *fiber.App { return r.app }

func (r *Router) Group(prefix string, handlers ...fiber.Handler) fiber.Router {
	return r.app.Group(prefix, handlers...)
}

func (r *Router) Use(args ...any) fiber.Router { return r.app.Use(args...) }

func (r *Router) Static(prefix, root string, config ...fiber.Static) fiber.Router {
	return r.app.Static(prefix, root, config...)
}

func (r *Router) Has(name string) bool {
	return r.app.GetRoute(name).Path != ""
}

func (r *Router) URL(name string, params map[string]any) (string, error) {
	route := r.app.GetRoute(name)
	if route.Path == "" {
		return "", fmt.Errorf("router: no route named %q", name)
	}

	segments := strings.Split(route.Path, "/")
	built := make([]string, 0, len(segments))

	for _, segment := range segments {
		wildcard := strings.HasPrefix(segment, "*")
		if !wildcard && !strings.HasPrefix(segment, ":") {
			built = append(built, segment)
			continue
		}

		key := strings.TrimPrefix(strings.TrimPrefix(segment, ":"), "*")
		optional := strings.HasSuffix(key, "?")
		key = strings.TrimSuffix(key, "?")
		if key == "" {
			key = "*"
		}

		value, ok := params[key]
		if !ok {
			if optional || wildcard {
				continue
			}
			return "", fmt.Errorf("router: route %q needs parameter %q", name, key)
		}
		built = append(built, escapeParam(fmt.Sprint(value), wildcard))
	}

	joined := strings.Join(built, "/")
	if joined != "/" {
		joined = strings.TrimRight(joined, "/")
	}
	if joined == "" {
		joined = "/"
	}
	return joined, nil
}

func escapeParam(value string, wildcard bool) string {
	if !wildcard {
		return url.PathEscape(value)
	}

	parts := strings.Split(strings.TrimPrefix(path.Clean("/"+value), "/"), "/")
	for i, part := range parts {
		parts[i] = url.PathEscape(part)
	}
	return strings.Join(parts, "/")
}

func (r *Router) URLOr(name string, params map[string]any) string {
	built, err := r.URL(name, params)
	if err != nil {
		return ""
	}
	return built
}
