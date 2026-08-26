export type Block =
  | { kind: "text"; heading?: string; body: string }
  | { kind: "code"; heading?: string; body: string }
  | { kind: "note"; heading: string; body: string }
  | { kind: "table"; heading?: string; columns: string[]; rows: string[][] }

export type DocPage = {
  slug: string
  title: string
  section: string
  lead: string
  blocks: Block[]
}

export const sections = [
  { key: "start", title: "Getting started" },
  { key: "cli", title: "Command line" },
  { key: "api", title: "The API" },
  { key: "services", title: "Services" },
  { key: "frontend", title: "Frontend" },
]

export const docs: DocPage[] = [
  {
    slug: "installation",
    title: "Installation",
    section: "start",
    lead: "Omega needs Go and nothing else. Bun is only required for this interface.",
    blocks: [
      {
        kind: "code",
        heading: "Start the API",
        body: `cp .env.example .env
echo "AUTH_SECRET=$(openssl rand -base64 48)" >> .env

go run ./cmd/omega migrate:fresh --seed
go run ./cmd/omega serve`,
      },
      {
        kind: "note",
        heading: "AUTH_SECRET has no default",
        body: "The server refuses to boot without it, rather than signing tokens with a guessable key.",
      },
      {
        kind: "code",
        heading: "Start this interface",
        body: `cd web
bun install
bun run dev`,
      },
      {
        kind: "table",
        heading: "What is served",
        columns: ["Address", "What it is"],
        rows: [
          [":3300/api/docs", "Swagger UI, embedded in the binary"],
          [":3300/graphql", "GraphiQL with its Explorer"],
          [":3300/api/openapi.json", "the document this interface is typed from"],
          [":5173", "this interface"],
        ],
      },
      {
        kind: "note",
        heading: "The port frees itself",
        body: "If the configured port is taken, Omega takes the next free one and logs it: warn port taken, moved to the next free one using=3301 wanted=3300",
      },
    ],
  },
  {
    slug: "structure",
    title: "Project structure",
    section: "start",
    lead: "Where each kind of code lives, and the one rule that keeps it honest.",
    blocks: [
      {
        kind: "code",
        body: `omega/
├── cmd/omega/          the CLI and its generator stubs
├── internal/
│   ├── kernel/         config, logger, container, JSON errors
│   ├── router/         named routes, route model binding
│   ├── api/            REST, OpenAPI, GraphQL, embedded consoles
│   ├── auth/           JWT guard, refresh tokens, OAuth providers
│   ├── validation/     bind a body, report a 422 per field
│   ├── queue/          database-backed jobs, worker, retries
│   ├── events/         in-process bus, sync and deferred listeners
│   ├── scheduler/      periodic and daily tasks
│   ├── cache/          key-value in memory, with a TTL
│   ├── mail/           log and SMTP drivers
│   ├── storage/        local disk, paths confined to the root
│   ├── broadcast/      server-sent events with channels
│   ├── observability/  Prometheus metrics per route
│   ├── database/       connections, migrations, seeders, factories
│   └── mcp/            the Model Context Protocol server
├── app/
│   ├── models/         rows, nothing more
│   ├── requests/       what a client may send, with validate tags
│   ├── services/       what the app does, takes a context.Context
│   ├── presenters/     what leaves the process
│   ├── jobs/           the queued tasks
│   ├── listeners/      what reacts to an event
│   └── controllers/    parse, call a service, answer
├── database/           migrations, seeds, factories, the SQLite file
├── routes/             api.go · auth.go · auth_docs.go · workers.go
├── config/             app · database · auth · queue · mail · storage
└── web/                this interface`,
      },
      {
        kind: "text",
        heading: "The rule",
        body: "Application code never imports internal/ directly — everything goes through the omega façade. The single exception is internal/validation, whose error shape is part of the public contract.",
      },
      {
        kind: "table",
        heading: "The path of a request",
        columns: ["Step", "Where"],
        rows: [
          ["parse and validate", "internal/validation.Bind"],
          ["business logic", "app/services/<domain>"],
          ["database", "GORM, inside the service"],
          ["side effects", "omega.Emit, then a listener or a job"],
          ["what becomes public", "app/presenters/<domain>"],
          ["HTTP answer", "app/controllers/<domain>"],
        ],
      },
    ],
  },
  {
    slug: "configuration",
    title: "Configuration",
    section: "start",
    lead: "Each file in config/ is namespaced under its base name.",
    blocks: [
      {
        kind: "code",
        body: `omega.Cfg().Int("app.port")             // config/app.yaml
omega.Cfg().String("auth.secret")       // config/auth.yaml
omega.Cfg().String("database.default")  // config/database.yaml
omega.Cfg().Int("queue.concurrency")    // config/queue.yaml
omega.Cfg().String("mail.driver")       // config/mail.yaml
omega.Cfg().String("storage.root")      // config/storage.yaml`,
      },
      {
        kind: "table",
        heading: "The variables that matter",
        columns: ["Variable", "What it does"],
        rows: [
          ["AUTH_SECRET", "signs the JWTs — required, no default"],
          ["AUTH_ACCESS_TTL", "access token lifetime, default 15m"],
          ["AUTH_REFRESH_TTL", "refresh token lifetime, default 720h"],
          ["APP_PORT", "the wanted port, default 3000"],
          ["APP_PORT_ATTEMPTS", "how many ports to try, default 20"],
          ["DB_SQLITE_PATH", "the SQLite file, default database/omega.db"],
          ["DB_CONNECTION", "sqlite · postgres · mysql"],
          ["CORS_ORIGINS", "who may call the API from a browser"],
        ],
      },
      {
        kind: "table",
        heading: "The services",
        columns: ["Variable", "What it does"],
        rows: [
          ["QUEUE_CONCURRENCY", "jobs processed in parallel, default 4"],
          ["QUEUE_POLL", "wait when the queue is empty, default 1s"],
          ["QUEUE_STUCK_AFTER", "when a claimed job is reclaimed, default 5m"],
          ["MAIL_DRIVER", "log or smtp — log by default, nothing is sent"],
          ["MAIL_HOST · MAIL_PORT", "the SMTP server"],
          ["STORAGE_ROOT", "where files land, default storage/app"],
          ["STORAGE_URL", "the public prefix, default /storage"],
          ["RATELIMIT_MAX", "requests per window on every route — 0 disables it"],
          ["RATELIMIT_WINDOW", "the window, default 1m"],
          ["RATELIMIT_AUTH_MAX", "attempts per address on login, register, refresh and password — default 10"],
          ["RATELIMIT_AUTH_WINDOW", "the window, default 1m"],
          ["RATELIMIT_ACCOUNT_MAX", "failed logins per account, counted in the database — default 8"],
          ["RATELIMIT_ACCOUNT_WINDOW", "the window, default 15m"],
          ["APP_PROXY_HEADER", "behind a proxy, the header carrying the client address"],
          ["APP_TRUSTED_PROXIES", "the proxies allowed to set it"],
        ],
      },
      {
        kind: "note",
        heading: "Interpolation happens before parsing",
        body: "${VAR} and ${VAR:-fallback} are substituted on the YAML bytes before viper reads them. Viper does not know that syntax, so expanding afterwards would leave the literal references in place.",
      },
    ],
  },

  {
    slug: "cli-overview",
    title: "Every command",
    section: "cli",
    lead: "The complete surface of the omega binary.",
    blocks: [
      {
        kind: "table",
        heading: "Running",
        columns: ["Command", "What it does"],
        rows: [
          ["omega serve [--port N]", "start the HTTP server"],
          ["omega dev", "recompile and restart on every change, through air"],
          ["omega build [-o bin/app]", "static binary, CGO disabled, cross-compiles"],
          ["omega test [--coverage]", "run the suite, optionally with a coverage profile"],
          ["omega routes", "list the registered routes with their names"],
          ["omega mcp", "serve the Model Context Protocol on stdin/stdout"],
        ],
      },
      {
        kind: "table",
        heading: "Database",
        columns: ["Command", "What it does"],
        rows: [
          ["omega migrate", "apply the pending migrations"],
          ["omega migrate:status", "who ran, who did not"],
          ["omega migrate:rollback [--step N]", "unwind the last N, default 1"],
          ["omega migrate:reset", "unwind everything"],
          ["omega migrate:fresh [--seed]", "drop every table, migrate, optionally seed"],
          ["omega db:seed [Seeder...]", "run every seeder, or the named ones"],
        ],
      },
      {
        kind: "table",
        heading: "Queue and scheduler",
        columns: ["Command", "What it does"],
        rows: [
          ["omega queue:work", "consume jobs until interrupted"],
          ["omega queue:failed", "list the jobs that gave up"],
          ["omega queue:retry [--id N]", "push failed jobs back"],
          ["omega queue:flush [--status]", "delete jobs from the table"],
          ["omega schedule:run", "run the periodic tasks"],
          ["omega schedule:list", "show them and their next run"],
        ],
      },
      {
        kind: "table",
        heading: "Generators",
        columns: ["Command", "What it writes"],
        rows: [
          ["omega make:resource Post", "an API resource — the presenter, as in artisan"],
          ["omega make:model Post", "the model and its create migration"],
          ["omega make:model Post -mcr", "the whole scaffold, artisan style"],
          ["omega make:model Post -sf", "adds a seeder and a factory"],
          ["omega make:controller Post", "a controller on its own"],
          ["omega make:controller Post -r", "the five CRUD actions, with request and service"],
          ["omega make:request Post", "the shape a client may send"],
          ["omega make:service Post", "where the logic lives"],
          ["omega make:migration add_slug", "a blank migration"],
          ["omega make:seed Post", "a seeder"],
          ["omega make:factory Post", "a factory for tests and seeds"],
          ["omega make:middleware EnsureAdmin", "a Fiber middleware"],
          ["omega web:nuxt", "rebuild web/ as a Nuxt 4 SPA with shadcn-vue"],
          ["omega web:react", "rebuild web/ as a React 19 app with shadcn/ui"],
          ["omega web:next", "rebuild web/ as a Next.js 15 app with shadcn/ui"],
          ["omega web:nuxt --pm pnpm", "any of bun, pnpm, yarn or npm works"],
          ["omega make:policy Post", "who may do what — registers itself"],
          ["omega make:rule Siret", "a custom validation rule"],
          ["omega make:event OrderPaid", "an event payload and its emitter"],
          ["omega make:mail Invoice", "a mailable"],
          ["omega make:notification Welcome", "mail and broadcast channels, sent by omega.Notify"],
          ["omega make:observer Post", "GORM callbacks for a model"],
          ["omega make:test Pricing", "a unit test skeleton"],
          ["omega make:presenter Post", "alias of make:resource"],
          ["omega make:seeder Demo", "alias of make:seed"],
          ["omega make:job SendInvoice", "a queued job"],
          ["omega make:listener NotifyAdmin", "an event listener"],
        ],
      },
      {
        kind: "note",
        heading: "Every generator takes --force",
        body: "Without it, an existing file is never overwritten — the generator stops and tells you.",
      },
    ],
  },
  {
    slug: "cli-serve",
    title: "serve, dev, build",
    section: "cli",
    lead: "The three ways to run the application.",
    blocks: [
      {
        kind: "code",
        heading: "serve",
        body: `$ omega serve
$ omega serve --port 8080

  Ω Omega 1.0.0
  local  ·  sqlite

  http://127.0.0.1:3300
  ctrl-c to stop`,
      },
      {
        kind: "text",
        body: "If the port is taken, Omega acquires the next free one instead of failing. It says so in the log, because a server that silently moves is a trap.",
      },
      {
        kind: "code",
        heading: "dev",
        body: `$ go install github.com/air-verse/air@latest
$ omega dev`,
      },
      {
        kind: "code",
        heading: "build",
        body: `$ omega build -o bin/app

# CGO_ENABLED=0 — the SQLite driver is pure Go, so the result is a
# static binary that cross-compiles and runs on a scratch image.
$ GOOS=linux GOARCH=arm64 omega build -o bin/app-arm64`,
      },
      {
        kind: "note",
        heading: "A globally installed binary carries its own migrations",
        body: "Migrations register themselves through init(), so they are compiled in. After make:model, run go run ./cmd/omega migrate — or go install ./cmd/omega again.",
      },
    ],
  },
  {
    slug: "cli-make",
    title: "The generators",
    section: "cli",
    lead: "A generator writes the vertical slice, never the routes — those belong to a file you own.",
    blocks: [
      {
        kind: "code",
        heading: "The whole slice at once",
        body: `$ omega make:model Article -a

created app/models/article.go
created database/migrations/20260818120000_create_articles_table.go
created app/requests/article/requests.go
created app/services/article/service.go
created app/presenters/article/presenter.go
created app/controllers/article/controller.go`,
      },
      {
        kind: "code",
        heading: "What the controller looks like",
        body: `func (r *ArticleController) Store(c *fiber.Ctx) error {
	body, err := validation.Bind[requests.Article](c)
	if err != nil {
		return err
	}

	record, err := r.articles.Create(c.UserContext(), *body)
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(omega.Map{"data": record})
}`,
      },
      {
        kind: "text",
        body: "Parse, call, answer. No validation in if statements, no GORM call, no response shaping. The rules live in app/requests/<domain> as tags; the logic lives in app/services/<domain> and takes a context.Context, so it is testable without an HTTP request.",
      },
      {
        kind: "code",
        heading: "Then paste the routes it prints",
        body: `article := controllers.NewArticleController(services.NewArticle(app.DB))
group.Get("/articles", article.Index).Name("articles.index")
group.Post("/articles", article.Store).Name("articles.store")
group.Get("/articles/:id", omega.Model[models.Article](), article.Show).Name("articles.show")`,
      },
      {
        kind: "note",
        heading: "Or write no controller at all",
        body: "api.Register[models.Article](registry, \"articles\", \"article\") publishes the five REST routes, the OpenAPI description and the GraphQL types in one line.",
      },
    ],
  },
  {
    slug: "cli-migrate",
    title: "Migrations",
    section: "cli",
    lead: "Every migration carries its own rollback. Nothing is guessed.",
    blocks: [
      {
        kind: "code",
        body: `func init() {
	database.Register(&gormigrate.Migration{
		ID: "20260818120000_create_articles_table",
		Migrate: func(tx *gorm.DB) error {
			return tx.AutoMigrate(&models.Article{})
		},
		Rollback: func(tx *gorm.DB) error {
			return tx.Migrator().DropTable("articles")
		},
	})
}`,
      },
      {
        kind: "code",
        heading: "A full cycle",
        body: `$ omega migrate
migrated 3 migration(s)

$ omega migrate:status
  [applied]  20260101000001_create_users_table
  [applied]  20260101000002_create_refresh_tokens_table
  [applied]  20260818120000_create_articles_table

$ omega migrate:rollback --step 2
rolled back 20260818120000_create_articles_table
rolled back 20260101000002_create_refresh_tokens_table

$ omega migrate:reset
all migrations rolled back`,
      },
      {
        kind: "text",
        body: "The identifier is the filename, so migrations run in the order they were created. The documentation follows: a table added by a migration is described in OpenAPI and GraphQL at the next boot, with no code to write.",
      },
    ],
  },

  {
    slug: "resources",
    title: "Automatic resources",
    section: "api",
    lead: "Omega reads the live schema at boot and publishes one resource per table.",
    blocks: [
      {
        kind: "code",
        heading: "A table is enough",
        body: `$ omega make:model Post
$ omega migrate
$ omega routes | grep posts

GET|HEAD  /api/posts        api.posts.index
GET|HEAD  /api/posts/:id    api.posts.show
POST      /api/posts        api.posts.store
PATCH     /api/posts/:id    api.posts.update
DELETE    /api/posts/:id    api.posts.destroy`,
      },
      {
        kind: "table",
        heading: "What comes for free",
        columns: ["Parameter", "Effect"],
        rows: [
          ["?page=2&per_page=50", "pagination, capped at 200 rows"],
          ["?sort=-created_at", "sort, the minus sign for descending"],
          ["?name=Ada", "exact filter on any visible column"],
          ["?name__like=%ada%", "also __gt __gte __lt __lte __ne __in"],
        ],
      },
      {
        kind: "note",
        heading: "Hidden columns are not filterable either",
        body: "Answering “no rows” or “one row” to ?password=x would turn the list into an oracle. Columns named password, secret, *_token, *_hash and *_key are hidden by convention, and so are the internal tables.",
      },
      {
        kind: "text",
        heading: "An honest limit",
        body: "SQLite declares booleans as numeric, indistinguishable from a decimal, so a discovered boolean column is typed as a number. On PostgreSQL and MySQL the type is exact. Declaring the model with api.Register gives the right type everywhere.",
      },
    ],
  },
  {
    slug: "authentication",
    title: "Authentication",
    section: "api",
    lead: "Short stateless access tokens, single-use revocable refresh tokens.",
    blocks: [
      {
        kind: "table",
        heading: "The routes",
        columns: ["Route", "What it does"],
        rows: [
          ["POST /api/auth/register", "create an account, returns a token pair"],
          ["POST /api/auth/login", "exchange credentials for a token pair"],
          ["POST /api/auth/refresh", "spend a refresh token, get a new pair"],
          ["POST /api/auth/logout", "retire the refresh token"],
          ["GET /api/auth/me", "the bearer of the token"],
          ["POST /api/auth/password", "change it, signing every other session out"],
          ["GET /api/auth/{provider}", "start an OAuth sign-in"],
        ],
      },
      {
        kind: "note",
        heading: "A refresh token is single-use",
        body: "Replaying one that has already been spent is refused. That is what makes a stolen token loud instead of silent.",
      },
      {
        kind: "code",
        heading: "OAuth providers",
        body: `# Two variables and the provider comes up. Leave them empty and it is skipped.
GOOGLE_CLIENT_ID=...
GOOGLE_CLIENT_SECRET=...

# Known: google, github, gitlab, microsoftonline, facebook, discord, linkedin`,
      },
      {
        kind: "code",
        heading: "Protecting a route",
        body: `group.Get("/reports", guard.Required(), reports.Index)
group.Delete("/users/:id", guard.Required(), guard.Roles("admin"), users.Destroy)`,
      },
    ],
  },
  {
    slug: "validation",
    title: "Validation",
    section: "api",
    lead: "Rules live in tags. A failure always has the same shape.",
    blocks: [
      {
        kind: "code",
        heading: "The request",
        body: `package requests

type Register struct {
	Name     string \`json:"name" validate:"required,max=120"\`
	Email    string \`json:"email" validate:"required,email,max=255"\`
	Password string \`json:"password" validate:"required,min=8,max=72"\`
}`,
      },
      {
        kind: "code",
        heading: "The controller",
        body: `body, err := validation.Bind[requests.Register](c)
if err != nil {
	return err // the kernel renders it as a 422
}`,
      },
      {
        kind: "code",
        heading: "What the client receives",
        body: `{
  "error": "Validation failed.",
  "status": 422,
  "errors": {
    "email": "A valid email address is required.",
    "password": "At least 8 characters are required."
  }
}`,
      },
      {
        kind: "note",
        heading: "The field name is the JSON name",
        body: "A client that sent new_password is told about new_password, not about NewPassword.",
      },
    ],
  },
  {
    slug: "graphql",
    title: "GraphQL",
    section: "api",
    lead: "The same registry that serves REST builds the schema.",
    blocks: [
      {
        kind: "code",
        heading: "Queries and mutations, per resource",
        body: `{
  users(limit: 10, sort: "-created_at") { id name email role }
  users_count
  user(id: "1") { name }
}

mutation {
  create_post(title: "Hello") { id title created_at }
  update_post(id: "1", title: "Edited") { id }
  delete_post(id: "1")
}`,
      },
      {
        kind: "note",
        heading: "Introspection is open, data is not",
        body: "A query asking only for __schema needs no token, so the console can draw the schema before you sign in. Mix one real field into it and the whole request needs a token — the check parses the query rather than searching the text for __schema.",
      },
      {
        kind: "text",
        body: "Set api.public_introspection to false to demand a token even for the schema.",
      },
    ],
  },


  {
    slug: "queue",
    title: "Queues and jobs",
    section: "services",
    lead: "The queue lives in your database, not in Redis. The binary stays self-contained.",
    blocks: [
      {
        kind: "code",
        heading: "Dispatch",
        body: `omega.Dispatch(ctx, "welcome_email", map[string]any{"email": user.Email})

omega.Dispatch(ctx, "report", payload, omega.Job{
    Delay:    time.Hour,
    MaxTries: 5,
    Queue:    "reports",
})`,
      },
      {
        kind: "code",
        heading: "The handler",
        body: `func WelcomeEmail(mailer *mail.Mailer) func(context.Context, []byte) error {
	return func(ctx context.Context, payload []byte) error {
		var target Welcome
		if err := json.Unmarshal(payload, &target); err != nil {
			return err
		}
		return mailer.Send(mail.Message{To: []string{target.Email}})
	}
}

app.Queue.Handle("welcome_email", jobs.WelcomeEmail(app.Mail))`,
      },
      {
        kind: "code",
        heading: "Run the worker",
        body: `$ omega queue:work --concurrency 4 --queue default,reports

queue: worker started concurrency=4 handlers=2 queues=["default"]
queue: done id=1 job=welcome_email took=23ms`,
      },
      {
        kind: "table",
        heading: "The commands",
        columns: ["Command", "What it does"],
        rows: [
          ["omega queue:work", "consume jobs until interrupted"],
          ["omega queue:failed", "list the jobs that exhausted their attempts"],
          ["omega queue:retry --id 12", "push one back, or every failed one"],
          ["omega queue:flush --status failed", "delete jobs from the table"],
          ["omega make:job SendInvoice", "generate a job"],
        ],
      },
      {
        kind: "note",
        heading: "A killed worker loses nothing",
        body: "A job is claimed with an atomic update, so two workers never take the same one. If a worker dies mid-job, the row stays claimed until stuck_after passes, then another worker reclaims it.",
      },
      {
        kind: "text",
        heading: "Retries",
        body: "A failing job is released with an exponential backoff capped at ten minutes: 2s, 4s, 8s… Past MaxTries it moves to failed and waits for queue:retry. A panic inside a handler is caught and counted as a failure rather than taking the worker down.",
      },
    ],
  },
  {
    slug: "events",
    title: "Events and scheduler",
    section: "services",
    lead: "An in-process bus, and periodic tasks that need no crontab.",
    blocks: [
      {
        kind: "code",
        heading: "Listen",
        body: `app.Events.Listen("user.registered", "log", listeners.LogRegistration(app.Log))

app.Events.ListenAsync("user.registered", "welcome", func(ctx context.Context, payload any) error {
	return app.Queue.Push(ctx, "welcome_email", payload)
})`,
      },
      {
        kind: "code",
        heading: "Emit",
        body: `omega.Emit(ctx, "user.registered", map[string]any{
    "id": user.ID, "email": user.Email,
})`,
      },
      {
        kind: "note",
        heading: "Synchronous or deferred",
        body: "Listen runs inside the request and can abort it by returning an error. ListenAsync runs in its own goroutine with a context detached from the request, so a slow listener never delays the response.",
      },
      {
        kind: "code",
        heading: "Schedule",
        body: `app.Scheduler.
	Every("purge-tokens", time.Hour, purgeTokens).
	DailyAt("report", "03:00", nightlyReport)`,
      },
      {
        kind: "code",
        heading: "Run it",
        body: `$ omega schedule:list
  purge-expired-tokens    1h0m0s          next 10:49:44
  report                  daily at 03:00  next 03:00:00

$ omega schedule:run`,
      },
      {
        kind: "text",
        body: "A task that is still running when its next turn comes is skipped rather than started twice. Panics are caught and logged, never fatal.",
      },
    ],
  },
  {
    slug: "cache-storage-mail",
    title: "Cache, storage, mail",
    section: "services",
    lead: "Three small services with no external dependency.",
    blocks: [
      {
        kind: "code",
        heading: "Cache",
        body: `count, err := omega.Remember("users.count", 5*time.Minute, func() (int64, error) {
	var total int64
	return total, db.Model(&models.User{}).Count(&total).Error
})

omega.Cache().Put("key", value, time.Hour)
omega.Cache().Forget("key")`,
      },
      {
        kind: "text",
        body: "In memory, with a TTL and a sweeper that drops expired entries every minute. Remember is generic: the type you return is the type you get back.",
      },
      {
        kind: "code",
        heading: "Storage",
        body: `omega.Storage().PutBytes("invoices/42.pdf", pdf)
content, err := omega.Storage().Get("invoices/42.pdf")
url := omega.Storage().URL("invoices/42.pdf")`,
      },
      {
        kind: "note",
        heading: "Paths cannot escape",
        body: "Every name is resolved against the root and refused if it lands outside — ../../etc/passwd returns an error rather than a file.",
      },
      {
        kind: "code",
        heading: "Mail",
        body: `omega.Mail().Send(omega.Message{
    To:      []string{"ada@example.com"},
    Subject: "Welcome",
    HTML:    "<p>Your account is ready.</p>",
})`,
      },
      {
        kind: "table",
        heading: "Drivers",
        columns: ["MAIL_DRIVER", "Behaviour"],
        rows: [
          ["log", "nothing is sent, the message is logged — the default"],
          ["smtp", "real delivery, STARTTLS when the server offers it"],
        ],
      },
    ],
  },
  {
    slug: "realtime",
    title: "Realtime and metrics",
    section: "services",
    lead: "Server-sent events, rate limiting, and Prometheus metrics per route.",
    blocks: [
      {
        kind: "code",
        heading: "Publish",
        body: `omega.Broadcast().Publish("orders", "created", order)`,
      },
      {
        kind: "code",
        heading: "Subscribe from the browser",
        body: `const stream = new EventSource("/api/events?channels=orders")

stream.addEventListener("created", (event) => {
  console.log(JSON.parse(event.data))
})`,
      },
      {
        kind: "text",
        body: "A subscriber with no channel receives everything. The stream sends a comment every 25 seconds so proxies do not close an idle connection, and a slow client is skipped rather than allowed to block the publisher.",
      },
      {
        kind: "table",
        heading: "Observability",
        columns: ["Route", "What it returns"],
        rows: [
          ["GET /api/metrics", "Prometheus text format"],
          ["GET /api/stats", "the same figures as JSON"],
        ],
      },
      {
        kind: "code",
        heading: "What is measured",
        body: `omega_requests_total{method="GET",route="/api/users"} 1420
omega_errors_total{method="GET",route="/api/users"} 0
omega_request_seconds_total{method="GET",route="/api/users"} 3.184
omega_requests_in_flight 2
omega_goroutines 14
omega_memory_bytes 8912384`,
      },
      {
        kind: "note",
        heading: "Grouped by route pattern",
        body: "/api/users/1 and /api/users/2 count under /api/users/:id, so the metric stays readable instead of exploding into one series per identifier.",
      },
      {
        kind: "code",
        heading: "Rate limiting",
        body: `# off by default
RATELIMIT_MAX=120
RATELIMIT_WINDOW=1m`,
      },
    ],
  },
  {
    slug: "frontend-setup",
    title: "Frontend setup",
    section: "frontend",
    lead: "How this interface was built, step by step.",
    blocks: [
      {
        kind: "code",
        heading: "1. Vite, React and Tailwind",
        body: `bun create vite@latest web --template react-ts
cd web && bun install
bun add tailwindcss @tailwindcss/vite`,
      },
      {
        kind: "code",
        heading: "2. The alias and the proxy",
        body: `// vite.config.ts
export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: { alias: { "@": path.resolve(__dirname, "./src") } },
  server: {
    port: 5173,
    proxy: {
      "/api": { target: "http://127.0.0.1:3300", changeOrigin: true },
      "/graphql": { target: "http://127.0.0.1:3300", changeOrigin: true },
    },
  },
})`,
      },
      {
        kind: "text",
        body: "The proxy is why development needs no CORS at all: the browser only ever talks to :5173. CORS still matters in production, where the two are served from different origins.",
      },
      {
        kind: "code",
        heading: "3. composants maison",
        body: `bun x shadcn@latest init --template vite --base radix --preset nova
bun x shadcn@latest add button card table input sidebar dropdown-menu`,
      },
      {
        kind: "note",
        heading: "shadcn is not a dependency",
        body: "The command copies component source files into app/components/ui. They are yours: edit them, they will not be overwritten by an update.",
      },
      {
        kind: "code",
        heading: "4. CORS, on the Go side",
        body: `# config/app.yaml
cors:
  origins: \${CORS_ORIGINS:-http://localhost:5173}
  credentials: \${CORS_CREDENTIALS:-true}`,
      },
    ],
  },
  {
    slug: "typed-client",
    title: "The typed client",
    section: "frontend",
    lead: "This interface does not guess the API shape — it reads it.",
    blocks: [
      {
        kind: "code",
        heading: "Generate the types",
        body: `curl -s http://127.0.0.1:3300/api/openapi.json > openapi.json
bun x openapi-typescript openapi.json -o app/lib/api-types.ts`,
      },
      {
        kind: "code",
        heading: "Use them",
        body: `import type { components } from "./api-types"

export type User = components["schemas"]["user"]`,
      },
      {
        kind: "code",
        heading: "The refresh that nobody sees",
        body: `// src/lib/api.ts
if (response.status === 401 && auth && retry) {
  const renewed = await refresh()
  if (renewed) return request(path, { ...options, retry: false })
  writeTokens(null)
}`,
      },
      {
        kind: "text",
        body: "An expired access token is the normal case, not a failure: rotate once and replay. Only a refresh token that is also dead ends the session. One rotation is in flight at a time, even when ten requests fail together.",
      },
    ],
  },
  {
    slug: "i18n",
    title: "Internationalisation",
    section: "frontend",
    lead: "Three languages, detected from the browser, remembered in localStorage.",
    blocks: [
      {
        kind: "code",
        heading: "1. Install",
        body: `bun add i18next i18n maison i18next-browser-languagedetector`,
      },
      {
        kind: "code",
        heading: "2. Configure",
        body: `// src/lib/i18n.ts
import en from "@/locales/en.json"
import fr from "@/locales/fr.json"
import es from "@/locales/es.json"

i18n.use(LanguageDetector).use(initReactI18next).init({
  resources: {
    en: { translation: en },
    fr: { translation: fr },
    es: { translation: es },
  },
  fallbackLng: "en",
  supportedLngs: ["en", "fr", "es"],
  detection: {
    order: ["localStorage", "navigator"],
    lookupLocalStorage: "omega.language",
    caches: ["localStorage"],
  },
})`,
      },
      {
        kind: "code",
        heading: "3. The translation files",
        body: `// src/locales/en.json
{
  "users": {
    "title": "Users",
    "count": "{{total}} account(s) · page {{page}} of {{last}}"
  }
}`,
      },
      {
        kind: "code",
        heading: "4. Use it",
        body: `const { t } = useTranslation()

<CardTitle>{t("users.title")}</CardTitle>
<span>{t("users.count", { total: 26, page: 1, last: 3 })}</span>`,
      },
      {
        kind: "code",
        heading: "5. The switcher",
        body: `const { i18n } = useTranslation()

<DropdownMenuCheckboxItem
  checked={i18n.resolvedLanguage === "fr"}
  onSelect={() => i18n.changeLanguage("fr")}
>
  Français
</DropdownMenuCheckboxItem>`,
      },
      {
        kind: "note",
        heading: "Import the config once",
        body: "import \"@/lib/i18n\" at the top of App.tsx. The side effect initialises i18next before the first render.",
      },
    ],
  },
  {
    slug: "theming",
    title: "Dark and light mode",
    section: "frontend",
    lead: "shadcn themes with CSS variables, so switching is one class on the root element.",
    blocks: [
      {
        kind: "code",
        heading: "1. Install",
        body: `bun add thème maison`,
      },
      {
        kind: "code",
        heading: "2. Wrap the application",
        body: `<ThemeProvider attribute="class" defaultTheme="system" enableSystem disableTransitionOnChange>
  <App />
</ThemeProvider>`,
      },
      {
        kind: "text",
        body: "attribute=\"class\" puts .dark on <html>. Tailwind reads it, and every shadcn component follows because their colours are CSS variables, not hardcoded values.",
      },
      {
        kind: "code",
        heading: "3. The toggle",
        body: `const { setTheme } = useTheme()

<DropdownMenuItem onSelect={() => setTheme("light")}>Light</DropdownMenuItem>
<DropdownMenuItem onSelect={() => setTheme("dark")}>Dark</DropdownMenuItem>
<DropdownMenuItem onSelect={() => setTheme("system")}>System</DropdownMenuItem>`,
      },
      {
        kind: "code",
        heading: "4. The icon that rotates",
        body: `<Sun className="size-4 rotate-0 scale-100 transition-all dark:-rotate-90 dark:scale-0" />
<Moon className="absolute size-4 rotate-90 scale-0 transition-all dark:rotate-0 dark:scale-100" />`,
      },
      {
        kind: "note",
        heading: "disableTransitionOnChange matters",
        body: "Without it, every colour on the page animates at once when you switch, which reads as a flash rather than a change.",
      },
    ],
  },
  {
    slug: "dashboard",
    title: "The dashboard",
    section: "frontend",
    lead: "The shadcn sidebar, collapsible to icons, with a persistent state.",
    blocks: [
      {
        kind: "code",
        heading: "The layout",
        body: `<SidebarProvider>
  <AppSidebar />
  <SidebarInset>
    <header className="flex h-14 items-center gap-2 border-b px-4">
      <SidebarTrigger />
      <Separator orientation="vertical" className="mr-2 h-4" />
      <h1 className="text-sm font-medium">{heading}</h1>
    </header>
    <div className="flex flex-1 flex-col gap-4 p-4 md:p-6">
      <Outlet />
    </div>
  </SidebarInset>
</SidebarProvider>`,
      },
      {
        kind: "code",
        heading: "A menu entry",
        body: `<SidebarMenuItem>
  <SidebarMenuButton asChild isActive={pathname.startsWith(item.url)} tooltip={item.title}>
    <Link to={item.url}>
      <item.icon />
      <span>{item.title}</span>
    </Link>
  </SidebarMenuButton>
</SidebarMenuItem>`,
      },
      {
        kind: "text",
        body: "collapsible=\"icon\" shrinks the sidebar to its icons instead of hiding it; the tooltip prop is what keeps it usable in that state. SidebarRail gives the drag handle on the edge.",
      },
      {
        kind: "table",
        heading: "The pieces",
        columns: ["Component", "Role"],
        rows: [
          ["SidebarProvider", "holds the open state, persists it in a cookie"],
          ["SidebarInset", "the content area beside the sidebar"],
          ["SidebarTrigger", "the button that folds it"],
          ["SidebarRail", "the draggable edge"],
          ["SidebarGroup", "a titled section of the menu"],
        ],
      },
    ],
  },
]
