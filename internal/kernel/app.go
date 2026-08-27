package kernel

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/limiter"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/rs/zerolog"
	"gorm.io/gorm"

	"omega/internal/auth"
	"omega/internal/broadcast"
	"omega/internal/cache"
	"omega/internal/currency"
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

const Version = "1.0.0"

type App struct {
	Cfg       *Config
	Log       zerolog.Logger
	Fiber     *fiber.App
	Router    *router.Router
	Databases *database.Manager
	DB        *gorm.DB

	Queue     *queue.Queue
	Events    *events.Bus
	Scheduler *scheduler.Scheduler
	Cache     *cache.Cache
	Currency  *currency.Exchange
	Mail      *mail.Mailer
	Storage   *storage.Disk
	Broadcast *broadcast.Hub
	Metrics   *observability.Metrics
	Policy    *policy.Registry

	addr      string
	container *Container
}

type bootConfig struct {
	configDir string
	withHTTP  bool
	withDB    bool
	withChdir bool
}

type Option func(*bootConfig)

func WithConfigDir(dir string) Option {
	return func(b *bootConfig) {
		if dir != "" {
			b.configDir = dir
		}
	}
}

func WithoutHTTP() Option { return func(b *bootConfig) { b.withHTTP = false } }

func WithoutDatabase() Option { return func(b *bootConfig) { b.withDB = false } }

func WithoutChdir() Option { return func(b *bootConfig) { b.withChdir = false } }

func enterRootIf(move bool) (string, bool, error) {
	if move {
		return EnterRoot()
	}

	working, err := os.Getwd()
	if err != nil {
		return "", false, err
	}
	if _, found := FindRoot(working); !found {
		return working, false, ErrNoProject
	}
	return working, false, nil
}

func Boot(opts ...Option) (*App, error) {
	cfg := bootConfig{
		configDir: "config",
		withHTTP:  true,
		withDB:    true,
		withChdir: true,
	}
	for _, opt := range opts {
		opt(&cfg)
	}

	root, moved, err := enterRootIf(cfg.withChdir)
	if err != nil {
		return nil, err
	}

	conf, err := LoadConfig(cfg.configDir)
	if err != nil {
		return nil, fmt.Errorf("kernel: config: %w", err)
	}

	log := NewLogger(conf)
	if moved {
		log.Debug().Str("root", root).Msg("kernel: moved to the project root")
	}
	app := &App{Cfg: conf, Log: log, container: Global()}

	if cfg.withDB {
		if err := app.bootDatabase(); err != nil {
			return nil, err
		}
	}

	if err := app.bootServices(conf); err != nil {
		return nil, err
	}

	if cfg.withHTTP {
		app.bootHTTP(conf)
	}

	app.fillContainer()
	return app, nil
}

func (a *App) bootDatabase() error {
	dbCfg := database.Config{}
	if err := a.Cfg.Unmarshal("database", &dbCfg); err != nil {
		return fmt.Errorf("kernel: database config: %w", err)
	}

	a.Databases = database.NewManager(dbCfg, a.Log)

	db, err := a.Databases.Default()
	if err != nil {
		return fmt.Errorf("kernel: %w", err)
	}
	a.DB = db
	return nil
}

func (a *App) bootServices(conf *Config) error {
	a.Events = events.New(a.Log)
	a.Scheduler = scheduler.New(a.Log)
	a.Cache = cache.New()
	a.Currency = currency.New(currency.Config{
		Base:      conf.StringOr("currency.base", currency.DefaultBase),
		Endpoint:  conf.StringOr("currency.endpoint", currency.DefaultEndpoint),
		TTL:       conf.Duration("currency.ttl"),
		Timeout:   conf.Duration("currency.timeout"),
		Available: conf.StringSlice("currency.available"),
	})
	a.Metrics = observability.New()
	a.Broadcast = broadcast.New()
	a.Policy = policy.New()

	if a.DB != nil {
		a.Queue = queue.New(queue.NewStore(a.DB))
	}

	a.Mail = mail.New(mail.Config{
		Driver:   conf.StringOr("mail.driver", "log"),
		Host:     conf.StringOr("mail.host", "127.0.0.1"),
		Port:     conf.IntOr("mail.port", 1025),
		Username: conf.StringOr("mail.username", ""),
		Password: conf.StringOr("mail.password", ""),
		From:     conf.StringOr("mail.from", "omega@localhost"),
	}, a.Log)

	disk, err := storage.New(
		conf.StringOr("storage.root", "storage/app"),
		conf.StringOr("storage.url", "/storage"),
	)
	if err != nil {
		return fmt.Errorf("kernel: storage: %w", err)
	}
	a.Storage = disk

	return nil
}

func (a *App) bootHTTP(conf *Config) {
	a.Fiber = fiber.New(fiber.Config{
		AppName:                 conf.StringOr("app.name", "Omega"),
		ErrorHandler:            ErrorHandler(a.Log, conf.Debug(), conf.IsLocal()),
		DisableStartupMessage:   true,
		ReadTimeout:             conf.Duration("app.read_timeout"),
		WriteTimeout:            conf.Duration("app.write_timeout"),
		BodyLimit:               conf.IntOr("app.body_limit", 4*1024*1024),
		ProxyHeader:             conf.StringOr("app.proxy_header", ""),
		EnableTrustedProxyCheck: len(conf.StringSlice("app.trusted_proxies")) > 0,
		TrustedProxies:          conf.StringSlice("app.trusted_proxies"),
	})

	a.Fiber.Use(recover.New())
	a.Fiber.Use(cors.New(cors.Config{
		AllowOrigins:     conf.StringOr("app.cors.origins", "http://localhost:5173"),
		AllowCredentials: conf.BoolOr("app.cors.credentials", true),
		AllowHeaders:     "Origin, Content-Type, Accept, Authorization, X-Omega-Live, X-Omega-Navigate",
		AllowMethods:     "GET, POST, PUT, PATCH, DELETE, OPTIONS",
		ExposeHeaders:    "Content-Length",
		MaxAge:           600,
	}))
	a.Fiber.Use(RequestID())
	a.Fiber.Use(RequestLogger(a.Log, conf.IsLocal()))
	a.Fiber.Use(a.Metrics.Middleware())

	if conf.IntOr("app.ratelimit.max", 0) > 0 {
		a.Fiber.Use(limiter.New(limiter.Config{
			Max:          conf.IntOr("app.ratelimit.max", 120),
			Expiration:   conf.Duration("app.ratelimit.window"),
			KeyGenerator: rateKey,
			LimitReached: func(c *fiber.Ctx) error {
				return fiber.NewError(fiber.StatusTooManyRequests, "Too many requests.")
			},
		}))
	}

	a.Router = router.New(a.Fiber)
}

func (a *App) CredentialLimiter() fiber.Handler {
	max := a.Cfg.IntOr("app.ratelimit.credentials.max", 10)
	if max <= 0 {
		return func(c *fiber.Ctx) error { return c.Next() }
	}

	window := a.Cfg.Duration("app.ratelimit.credentials.window")
	if window <= 0 {
		window = time.Minute
	}

	return limiter.New(limiter.Config{
		Max:        max,
		Expiration: window,
		KeyGenerator: func(c *fiber.Ctx) string {
			return "credentials:" + c.Route().Path + ":" + c.IP()
		},
		LimitReached: func(c *fiber.Ctx) error {
			return fiber.NewError(fiber.StatusTooManyRequests, "Too many attempts. Try again later.")
		},
	})
}

func rateKey(c *fiber.Ctx) string {
	if token := auth.Fingerprint(c); token != "" {
		return "token:" + token
	}
	return "ip:" + c.IP()
}

func (a *App) fillContainer() {
	c := a.container
	c.Cfg = a.Cfg
	c.Log = a.Log
	c.DB = a.DB
	c.Databases = a.Databases
	c.Queue = a.Queue
	c.Events = a.Events
	c.Scheduler = a.Scheduler
	c.Cache = a.Cache
	c.Currency = a.Currency
	c.Mail = a.Mail
	c.Storage = a.Storage
	c.Broadcast = a.Broadcast
	c.Metrics = a.Metrics
	c.Policy = a.Policy
	c.Router = a.Router
	c.Fiber = a.Fiber
}

func (a *App) Container() *Container { return a.container }

func (a *App) Addr() string {
	if a.addr != "" {
		return a.addr
	}
	return net.JoinHostPort(a.Cfg.StringOr("app.host", "0.0.0.0"), strconv.Itoa(a.wantedPort()))
}

func (a *App) wantedPort() int { return a.Cfg.IntOr("app.port", 3000) }

func (a *App) listen() (net.Listener, error) {
	host := a.Cfg.StringOr("app.host", "0.0.0.0")
	wanted := a.wantedPort()
	attempts := a.Cfg.IntOr("app.port_attempts", 20)
	if attempts < 1 {
		attempts = 1
	}

	for offset := 0; offset < attempts; offset++ {
		port := wanted + offset
		listener, err := net.Listen("tcp", net.JoinHostPort(host, strconv.Itoa(port)))
		if err == nil {
			if port != wanted {
				a.Log.Warn().Int("wanted", wanted).Int("using", port).Msg("port taken, moved to the next free one")
			}
			return listener, nil
		}
		if !addressInUse(err) {
			return nil, err
		}
	}

	return nil, fmt.Errorf("kernel: ports %d-%d are all taken", wanted, wanted+attempts-1)
}

func addressInUse(err error) bool {
	return errors.Is(err, syscall.EADDRINUSE) || errors.Is(err, syscall.EACCES)
}

func (a *App) Run() error {
	if a.Fiber == nil {
		return fmt.Errorf("kernel: this app was booted without HTTP")
	}

	listener, err := a.listen()
	if err != nil {
		return err
	}
	a.addr = listener.Addr().String()

	if a.Cfg.IsLocal() {
		writeConsole(banner(a.Cfg.StringOr("app.name", "Omega"), Version, a.Cfg.Env(), a.driverName(), a.addr))
	} else {
		a.Log.Info().Str("addr", a.addr).Str("env", a.Cfg.Env()).Msg("omega is listening")
	}

	errs := make(chan error, 1)
	go func() {
		if err := a.Fiber.Listener(listener); err != nil {
			errs <- err
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	select {
	case err := <-errs:
		return err
	case <-stop:
	}

	if err := a.Shutdown(); err != nil {
		return err
	}
	if a.Cfg.IsLocal() {
		writeConsole(paint(ansiDim, "\n  stopped cleanly\n\n"))
	}
	return nil
}

func (a *App) driverName() string {
	if a.Databases == nil {
		return "no database"
	}
	return a.Cfg.StringOr("database.connections."+a.Cfg.StringOr("database.default", "sqlite")+".driver", "?")
}

func (a *App) Shutdown() error {
	timeout := a.Cfg.Duration("app.shutdown_timeout")
	if timeout <= 0 {
		timeout = 10 * time.Second
	}

	var shutdownErr error
	if a.Fiber != nil {
		shutdownErr = a.Fiber.ShutdownWithTimeout(timeout)
	}

	if a.Events != nil && !a.Events.Wait(timeout) {
		a.Log.Warn().Msg("kernel: deferred listeners were still running at shutdown")
	}

	if a.Cache != nil {
		a.Cache.Close()
	}

	if a.Databases != nil {
		if err := a.Databases.Close(); err != nil {
			a.Log.Warn().Err(err).Msg("closing database connections")
		}
	}

	if shutdownErr != nil {
		return fmt.Errorf("kernel: shutdown: %w", shutdownErr)
	}

	if !a.Cfg.IsLocal() {
		a.Log.Info().Msg("stopped cleanly")
	}
	return nil
}
