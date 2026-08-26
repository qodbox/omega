package kernel

import (
	"fmt"
	"sort"
	"sync"

	"github.com/gofiber/fiber/v2"
	"github.com/rs/zerolog"
	"gorm.io/gorm"

	"omega/internal/broadcast"
	"omega/internal/cache"
	"omega/internal/database"
	"omega/internal/events"
	"omega/internal/mail"
	"omega/internal/observability"
	"omega/internal/policy"
	"omega/internal/queue"
	"omega/internal/router"
	"omega/internal/scheduler"
	"omega/internal/storage"
)

type Container struct {
	Cfg       *Config
	Log       zerolog.Logger
	DB        *gorm.DB
	Databases *database.Manager
	Queue     *queue.Queue
	Events    *events.Bus
	Scheduler *scheduler.Scheduler
	Cache     *cache.Cache
	Mail      *mail.Mailer
	Storage   *storage.Disk
	Broadcast *broadcast.Hub
	Metrics   *observability.Metrics
	Policy    *policy.Registry
	Router    *router.Router
	Fiber     *fiber.App

	mu       sync.RWMutex
	bindings map[string]any
}

var (
	global     *Container
	globalOnce sync.Once
)

func Global() *Container {
	globalOnce.Do(func() {
		global = &Container{bindings: make(map[string]any)}
	})
	return global
}

func (c *Container) Bind(name string, service any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.bindings == nil {
		c.bindings = make(map[string]any)
	}
	c.bindings[name] = service
}

func (c *Container) Lookup(name string) (any, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	service, ok := c.bindings[name]
	return service, ok
}

func (c *Container) Names() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	names := make([]string, 0, len(c.bindings))
	for name := range c.bindings {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func Resolve[T any](c *Container, name string) (T, error) {
	var zero T
	service, ok := c.Lookup(name)
	if !ok {
		return zero, fmt.Errorf("container: %q is not bound", name)
	}
	typed, ok := service.(T)
	if !ok {
		return zero, fmt.Errorf("container: %q is a %T, not a %T", name, service, zero)
	}
	return typed, nil
}

func MustResolve[T any](c *Container, name string) T {
	service, err := Resolve[T](c, name)
	if err != nil {
		panic(err)
	}
	return service
}
