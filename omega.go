package omega

import (
	"context"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/rs/zerolog"
	"gorm.io/gorm"

	"omega/internal/broadcast"
	"omega/internal/cache"
	"omega/internal/events"
	"omega/internal/kernel"
	"omega/internal/mail"
	"omega/internal/notify"
	"omega/internal/observability"
	"omega/internal/policy"
	"omega/internal/queue"
	"omega/internal/router"
	"omega/internal/scheduler"
	"omega/internal/storage"
)

const Version = kernel.Version

type (
	Job       = queue.Options
	Message   = mail.Message
	App       = kernel.App
	Config    = kernel.Config
	Container = kernel.Container
	Option    = kernel.Option
	Ctx       = fiber.Ctx
	Map       = fiber.Map
	Handler   = fiber.Handler
)

var (
	WithConfigDir   = kernel.WithConfigDir
	WithoutHTTP     = kernel.WithoutHTTP
	WithoutDatabase = kernel.WithoutDatabase
	WithoutChdir    = kernel.WithoutChdir
)

func Boot(opts ...Option) (*App, error) { return kernel.Boot(opts...) }

func C() *Container { return kernel.Global() }

func Cfg() *Config { return C().Cfg }

func Log() zerolog.Logger { return C().Log }

func DB() *gorm.DB { return C().DB }

func Connection(name string) (*gorm.DB, error) { return C().Databases.Connection(name) }

func Router() *router.Router { return C().Router }

func Queue() *queue.Queue { return C().Queue }

func Events() *events.Bus { return C().Events }

func Schedule() *scheduler.Scheduler { return C().Scheduler }

func Cache() *cache.Cache { return C().Cache }

func Mail() *mail.Mailer { return C().Mail }

func Storage() *storage.Disk { return C().Storage }

func Broadcast() *broadcast.Hub { return C().Broadcast }

func Notify(ctx context.Context, notification notify.Notification) error {
	return notify.New(Mail(), Broadcast()).Send(ctx, notification)
}

func Metrics() *observability.Metrics { return C().Metrics }

func Policy() *policy.Registry { return C().Policy }

func Allows(ctx context.Context, actor any, ability string, subject ...any) bool {
	return Policy().Allows(ctx, actor, ability, subject...)
}

func Dispatch(ctx context.Context, name string, payload any, opts ...queue.Options) error {
	return Queue().Push(ctx, name, payload, opts...)
}

func Emit(ctx context.Context, event string, payload any) error {
	return Events().Emit(ctx, event, payload)
}

func Remember[T any](key string, ttl time.Duration, build func() (T, error)) (T, error) {
	return cache.Remember(Cache(), key, ttl, build)
}

func Bind(name string, service any) { C().Bind(name, service) }

func Make[T any](name string) (T, error) { return kernel.Resolve[T](C(), name) }

func MustMake[T any](name string) T { return kernel.MustResolve[T](C(), name) }

func Model[T any](opts ...router.BindOptions) fiber.Handler {
	return router.Model[T](DB(), opts...)
}

func Bound[T any](c *fiber.Ctx) *T { return router.MustBound[T](c) }

func URL(name string, params map[string]any) (string, error) { return Router().URL(name, params) }
