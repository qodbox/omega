<h1 align="center">Ω Omega</h1>

<p align="center">
  A Go framework for JSON APIs.<br>
  Fiber v3 · GORM · REST + OpenAPI + GraphQL · JWT
</p>

<p align="center">
  <img alt="Go 1.26" src="https://img.shields.io/badge/Go-1.26-00ADD8?logo=go&logoColor=white">
  <img alt="MIT licence" src="https://img.shields.io/badge/licence-MIT-blue">
  <img alt="Coverage 95.7%" src="https://img.shields.io/badge/coverage-95.7%25-brightgreen">
  <img alt="Version 1.0.0" src="https://img.shields.io/badge/version-1.0.0-black">
</p>

<p align="center">
  <img alt="Omega dashboard" src=".github/screenshots/02-dashboard-v2.png" width="100%">
</p>

One command seals it: `omega web:nuxt`, `omega web:react` or `omega web:next`
generates a complete frontend — dashboard, documentation, authentication —
already wired to your API.

<p align="center">
  <img alt="Omega landing page" src=".github/screenshots/01-home-v2.png" width="100%">
</p>

<table>
  <tr>
    <td width="50%"><img alt="Built-in documentation" src=".github/screenshots/04-docs-v2.png"></td>
    <td width="50%"><img alt="User list" src=".github/screenshots/03-users-v2.png"></td>
  </tr>
  <tr>
    <td align="center"><sub>Documentation served by the application</sub></td>
    <td align="center"><sub>Resources generated from the schema</sub></td>
  </tr>
  <tr>
    <td width="50%"><img alt="Dark theme" src=".github/screenshots/06-dark-theme-v2.png"></td>
    <td width="50%"><img alt="Collapsed sidebar" src=".github/screenshots/05-collapsed-sidebar-v2.png"></td>
  </tr>
  <tr>
    <td align="center"><sub>Light and dark themes</sub></td>
    <td align="center"><sub>Collapsible sidebar</sub></td>
  </tr>
</table>

> **Status: milestone 3.** The whole initial roadmap is implemented: kernel,
> schema-generated API, authentication, validation, queues, events, scheduler,
> cache, storage, mail, rate limiting, SSE, WebSocket, metrics, policies,
> `omega new`, Docker and CI.
> See [Roadmap](#roadmap) for what is left.

## Getting started

```bash
cp .env.example .env
# Required: without a secret of at least 32 bytes, the server refuses to boot.
echo "AUTH_SECRET=$(openssl rand -base64 48)" >> .env

go run ./cmd/omega migrate:fresh --seed
go run ./cmd/omega serve
```

The application listens on `http://localhost:3000` (`--port` to change it).

| | |
|---|---|
| REST documentation | http://localhost:3000/api/docs |
| GraphQL console | http://localhost:3000/graphql |
| OpenAPI document | http://localhost:3000/api/openapi.json |

There is **no Node dependency**: both consoles are embedded in the binary by
`go:embed`, never loaded from a CDN.

The consoles and the OpenAPI document are public in `local` and `testing` only.
Elsewhere they answer 404, except to a signed-in caller holding the `docs.view`
ability (admins hold every ability); `API_DOCS_PUBLIC=true` opens them again.

### Development

```bash
go install github.com/air-verse/air@latest
go run ./cmd/omega dev
```

## The documentation follows the migrations

Omega reads the schema of the database it connects to at boot and can publish
one resource per table. Creating a table is enough to get its read endpoints,
its OpenAPI description and its GraphQL schema — without writing a line of code:

```bash
go run ./cmd/omega make:model Post   # model + migration
go run ./cmd/omega migrate
go run ./cmd/omega routes            # /api/posts is already there
```

### Discovery modes

Discovery is convenient while prototyping and dangerous in production: every
table it publishes is reachable by any caller the policies let through. It is
set in `config/api.yaml` (or `API_DISCOVERY`):

| `api.discovery` | Published |
|---|---|
| `all` | every table that is not internal |
| `none` | nothing: only resources declared with `api.Register` |
| `allowlist` | only the tables listed in `api.discovery_tables` (`API_DISCOVERY_TABLES=rates,providers`) |

Left empty, the mode is `all` in local and testing, and **`none` when
`APP_ENV=production`**. An unknown value stops the boot. Every table published by
discovery is logged at warn level at each boot, so it shows up in the logs an
operator reads.

A discovered table is read-only (list and show). Internal tables are never
published, even when allow-listed: `migrations`, `jobs`, `refresh_tokens`,
`login_attempts`, the billing tables, and the like.

### Declaring a resource

A table declared by hand is always published, whatever the mode:

```go
api.Register[models.User](registry, "users", "user", "password") // hidden columns

api.RegisterWith[models.Order](registry, "orders", "order",
	api.Owner("user_id"),                          // row-level ownership
	api.Actions(api.ActionList, api.ActionView, api.ActionCreate),
	api.Guarded("status"),                         // written by <plural>.any only
	api.Hidden("provider_reference"),
	api.Visible("phone"),                          // lift a default PII hiding
)

api.RegisterWith[models.Rate](registry, "rates", "rate", api.ReadOnly())
```

The same options reach a discovered table: `registry.Configure("payouts",
api.Owner("user_id"))`. `registry.AllowWrites("notes")` still reopens the writes
of a discovered table.

| Option | Effect |
|---|---|
| `ReadOnly()` | list and show only; no POST, PATCH, DELETE route, no GraphQL mutation |
| `Actions(...)` | only the actions named (`list`, `view`, `create`, `update`, `delete`) are routed, documented and present in GraphQL |
| `Owner("user_id")` | list, show, update, delete scoped to `WHERE user_id = <actor id>`; create stamps the column with the actor id, whatever the client sent; update cannot move a row to someone else. An actor holding `<plural>.any` is not scoped and may choose the owner |
| `Guarded(...)` | shown, but only written by actors holding `<plural>.any` |
| `Hidden(...)` | never served, filtered, sorted on or written |
| `Visible(...)` | serves a column the default PII rules hide (never one `Hidden` named) |

A row outside the actor's scope answers **404**, never 403: its existence is not
theirs to learn. The actor id is the `ID` field (or `ID()` method) of the actor,
then its `Key()`; `registry.IdentifyWith(func(actor any) any)` overrides it.

### What is hidden by default

- Secrets, on every resource: `password`, `secret`, `token`, `api_key`, `pin`,
  `otp`, `iban`, `*_hash`, `*_key`, `*_token`, `*_code`...
- Personal data, on every resource: `phone*`, `mobile*`, `msisdn`, `*_phone`,
  `address*`, `*_address` (a wallet address included), `ip`, `ip_address`,
  `*_ip`, `date_of_birth`, `dob`, `national_id`, `passport*`, `document*`,
  `kyc*`. `Visible(...)` lifts it on a resource that must serve one.
- Emails, on discovered tables only: a registered model is a deliberate act
  (the users resource serves its email on purpose), a table that merely exists
  is not.
- A registered model only ever serves the fields it describes: a column the
  struct leaves out or tags `json:"-"` never leaves the server.

### Writes

The generated writes validate every value against its column:

- unknown, read-only, hidden or guarded fields are **ignored** and logged at
  debug level — rejecting them would break clients that send back what they
  read (`id`, `created_at`), and ignoring them writes nothing the resource does
  not allow;
- objects and arrays are refused (422) for scalar columns;
- text longer than the column (`VARCHAR(n)`, `gorm:"size:n"`) is refused;
- integers, booleans and floats must be of their type (a number written as a
  string is accepted);
- `DECIMAL`/`NUMERIC` columns are exact: bodies are decoded with
  `json.Number`, never `float64`; the value travels as a string in both
  directions (`"amount": "0.000000000000000001"`), is checked against the
  declared precision and scale, and PostgreSQL reads it back as text. SQLite
  stores NUMERIC as a 15-digit REAL, so on SQLite a decimal with more than 15
  significant digits is refused rather than rounded.

### Queries

`?name=Ada` filters on any visible column; suffixes add operators: `__gt`,
`__gte`, `__lt`, `__lte`, `__ne`, `__in` (comma separated, at most 100 values),
`__contains`, `__starts`, `__ends` (the text is literal: `%` and `_` match
themselves) and `__like` (`%` and `_` are wildcards, `\%` and `\_` the literal
characters, at most 8 wildcards). At most 10 filters per request, patterns of
128 characters, `per_page` up to 200. Sorting on a hidden column is refused.
`?count=false` skips the `COUNT(*)` and omits `meta.total` and `meta.last_page`
on tables too large to count at every page.

### Policies on generated resources

Every generated route asks the policies `<plural>.<action>`, with `list`,
`view`, `create`, `update`, `delete`, and deny by default. List and create are
asked before the handler; view, update and delete are asked once the row is
loaded, **with the row as subject** — the model itself for a registered
resource, the column map for a discovered table:

```go
gate.Define("orders.update", func(ctx context.Context, actor, subject any) bool {
	order, ok := subject.(*models.Order)
	return ok && order.Status != "settled"
})
gate.Grant(models.RoleUser, "orders.list", "orders.view", "orders.create")
gate.Grant(models.RoleSupport, "orders.any") // every user's orders
```

Beware of `orders.*`: a resource wildcard grants `orders.any` too, and lifts the
ownership scope.

The users resource is declared with `Owner("id")`: a member lists and reads
their own account only, and cannot write their `role`.

### GraphQL

`/graphql` serves the same registry, through the same checks: abilities,
ownership, row subjects, hidden columns and write validation are shared code,
not a copy. Decimals are `String` in the schema. Before anything executes, a
request is bounded:

| `config/api.yaml` | Default | Refuses |
|---|---|---|
| `graphql.max_depth` | 15 | deeper nesting (GraphiQL's introspection needs 15) |
| `graphql.max_fields` | 500 | more fields, fragments expanded |
| `graphql.max_root_fields` | 10 | more top-level fields — aliases count |
| `graphql.max_complexity` | 10000 | a list costs its `limit` times the fields it selects |

One JSON object per request: batches (a JSON array), queries in the URL and
non-JSON bodies are refused.

One honest limit: SQLite declares booleans as bare `numeric`, so a discovered
column of that type is typed `number` (a `NUMERIC(p,s)` is a decimal). On
PostgreSQL and MySQL the type is exact. Declaring the model with `api.Register`
gives the right type everywhere.

## Authentication

JWT tokens signed by [golang-jwt](https://github.com/golang-jwt/jwt), OAuth
providers by [goth](https://github.com/markbates/goth).

```
POST /api/auth/register    create an account
POST /api/auth/login       sign in
POST /api/auth/refresh     rotate the pair
POST /api/auth/logout      revoke this sign-in
POST /api/auth/logout-all  sign out on every device
POST /api/auth/password    change the password
GET  /api/auth/csrf        (re)set the CSRF cookie
GET  /api/auth/me          the signed-in user
GET  /api/auth/providers   the active providers
GET  /api/auth/{provider}          start an OAuth sign-in
GET  /api/auth/{provider}/callback finish it
```

### Two transports: cookies for browsers, bearer for API clients

**Cookie mode** (the default, `AUTH_COOKIES=true`): no token is ever readable by
JavaScript. Login, register, refresh and the OAuth callback set three cookies and
answer `{"token_type":"Cookie","expires_in":900,"user":{...}}`, without tokens:

| Cookie | Holds | Attributes |
|---|---|---|
| `omega_access` | the access JWT | HttpOnly · Secure · SameSite=Lax · Path=/ · Max-Age = access TTL |
| `omega_refresh` | the refresh JWT | HttpOnly · Secure · SameSite=Strict · Path=/api/auth · Max-Age = refresh TTL |
| `omega_csrf` | 32 random bytes, base64url | readable by JS · Secure · SameSite=Lax · Path=/ |

A request with no `Authorization` header authenticates with `omega_access`. On
`POST`, `PUT`, `PATCH` and `DELETE` it must also send `X-CSRF-Token` equal to the
`omega_csrf` cookie (compared in constant time), or it is refused with 403
`{"error":"CSRF token missing or invalid."}` — refresh and logout included, which
read the refresh token from `omega_refresh` and take no body. A browser client
calls `GET /api/auth/csrf` once at boot (204, keeps a valid cookie or sets one),
fetches with `credentials: 'include'`, and reads its signed-in state from
`/api/auth/me`. The SSE stream (`/api/events`) and the WebSocket (`/api/ws`)
authenticate with the same cookie; the WebSocket upgrade is refused from an
origin outside `CORS_ORIGINS` (cross-site WebSocket hijacking).

**Bearer mode** is unchanged for mobile apps, server-to-server calls and tests:
send `X-Omega-Auth: bearer` on login/register (or turn cookies off) and the body
carries `access_token`, `refresh_token`, `token_type: "Bearer"`, `expires_in`;
then send `Authorization: Bearer <access_token>`. A bearer request is never
asked for a CSRF token, and `/refresh` and `/logout` still accept
`{"refresh_token": "..."}` in the body.

> **API on another origin.** With the frontend on `app.example.com` and the API
> on `api.example.com`, set `AUTH_COOKIE_DOMAIN=example.com` (the parent domain),
> otherwise the frontend cannot read `omega_csrf`. List the frontend in
> `CORS_ORIGINS`; credentials are allowed for those origins only, and a `*` is
> refused at boot while `CORS_CREDENTIALS=true`.

### Sessions and revocation

- The **refresh token is single-use** and belongs to a *family*: every token
  rotated from the same sign-in. A spent token is kept (marked used) until it
  expires; presenting it again means it was copied, so the **whole family is
  revoked** and a `refresh_token_reuse` security warning is logged.
  `AUTH_REUSE_GRACE` (default `0s`, strict) tolerates two tabs racing on the
  same cookie: inside it a replay is refused but revokes nothing.
- Every access token carries the account's **token version** (`tv`), checked
  against `users.token_version` on each authenticated request — the user is
  loaded there anyway. `logout-all` and a password change bump it, so every
  access token already handed out stops working on its next request, not at
  expiry. The role is read from the loaded user too, so a demotion applies at
  once. Logout of a single sign-in retires its refresh family; its access token
  lives out its TTL (15 minutes by default).
- **Passwords**: 12 characters at least, 72 bytes at most (bcrypt's limit,
  refused rather than silently truncated), not a common one. New hashes use
  bcrypt cost 12; an older, cheaper hash still verifies and is upgraded on the
  next successful sign-in. An unknown email is checked against a dummy hash of
  the same cost, and gets the same `Invalid credentials.` as a wrong password.
- **The secret** must be at least 32 bytes (measured decoded when written in
  standard base64, as `openssl rand -base64 48` produces); a shorter one refuses
  to boot outside `APP_ENV=testing`.

Everything under `/api` requires authentication, except `/auth/*`, `/health`,
`/currencies` and, in local and testing, `/docs` and `/openapi.json`.

### OAuth providers

Fill in two variables and the provider turns itself on; leave them empty and it
is ignored.

```bash
GOOGLE_CLIENT_ID=...
GOOGLE_CLIENT_SECRET=...
```

Known: `google`, `github`, `gitlab`, `microsoftonline`, `facebook`, `discord`,
`linkedin`. The callback URL is built from `APP_URL`.

## Architecture

```
omega/
├── cmd/omega/          Cobra CLI + generator stubs (go:embed)
├── internal/
│   ├── kernel/         config, logger, container, bootstrap, JSON errors
│   ├── router/         Fiber wrapper, named URLs, route model binding
│   ├── api/            registry: REST, OpenAPI, GraphQL, embedded consoles
│   ├── auth/           JWT guard, revocable refresh tokens, OAuth providers
│   ├── validation/     bind + validate, uniform 422
│   ├── queue/          database-backed queue, worker, retries, crash recovery
│   ├── events/         in-process bus, synchronous or deferred listeners
│   ├── scheduler/      periodic and daily tasks
│   ├── cache/          in-memory key-value with TTL
│   ├── mail/           log and SMTP drivers
│   ├── storage/        local disk, paths confined to the root
│   ├── broadcast/      SSE with channels and heartbeat
│   ├── observability/  Prometheus metrics per route
│   └── database/       multi-connection GORM, migrations, seeds, factories
├── app/
│   ├── models/         rows, nothing more
│   ├── requests/<domain>/     what a client is allowed to send, with its rules
│   ├── services/<domain>/     what the application does; takes a context.Context
│   ├── presenters/<domain>/   what leaves the process
│   ├── controllers/<domain>/  parse, call, answer
│   ├── policies/       who may do what
│   ├── jobs/           the queue's tasks
│   └── listeners/      the reactions to events
├── database/           migrations/ seeds/ factories/ and the SQLite file
├── web/                Nuxt 4 interface (SPA, own components, i18n, theme)
├── routes/             api.go · auth.go · auth_docs.go · workers.go
├── config/             app.yaml · database.yaml · auth.yaml · queue.yaml · mail.yaml · storage.yaml
└── tests/unit/
```

### The path of a request

```go
// 1. the controller does exactly three things
func (a *AuthController) Register(c fiber.Ctx) error {
    body, err := validation.Bind[requests.Register](c)  // parse + validate
    if err != nil {
        return err                                       // 422 rendered by the kernel
    }
    session, err := a.auth.Register(c.Context(), *body)  // call
    if err != nil {
        return translate(err)
    }
    return c.Status(201).JSON(sessionResponse(session))   // answer
}
```

Rules live in a tag, never in an `if`:

```go
type Register struct {
    Name     string `json:"name" validate:"required,max=120"`
    Email    string `json:"email" validate:"required,email,max=255"`
    Password string `json:"password" validate:"required,min=8,max=72"`
}
```

A failure always produces the same shape, without anyone writing it:

```json
{
  "error": "Validation failed.",
  "status": 422,
  "errors": { "email": "A valid email address is required." }
}
```

### Structural decisions

**The framework lives in `internal/`.** The Omega repository *is* the
application skeleton: `app/`, `routes/` and `config/` are part of the same
module. Application code never imports `internal/` directly — except
`validation`, whose error shape is part of the public contract.

**Pure-Go SQLite** (`github.com/glebarez/sqlite`). No cgo dependency:
`omega build` produces a static binary that cross-compiles and runs on a
`scratch` image.

**The consoles are embedded.** Swagger UI and GraphiQL travel inside the binary.
A server with no internet access still documents its API, and nobody on the
outside sees who reads the documentation.

**Interpolation before parsing.** `${VAR}` and `${VAR:-default}` are substituted
on the YAML bytes *before* viper reads them.

## Commands

```
omega serve [--port N]        Start the server
omega dev                     Hot reload through air
omega build [-o bin/app]      Static binary (CGO disabled)
omega test [--coverage]       Tests
omega routes                  List the named routes
omega mcp                     Serve the Model Context Protocol

omega migrate                 Apply the pending migrations
omega migrate:rollback [--step N]
omega migrate:reset · migrate:fresh [--seed] · migrate:status
omega db:seed [Seeder...]

omega make:model Post         model + create migration
omega make:service Post · make:request Post · make:controller Post
omega make:resource Post      an API resource (the presenter) — alias make:presenter
omega make:migration add_slug_to_posts · make:seed (alias make:seeder) · make:factory
omega make:policy Post        who may do what
omega make:rule Siret         a custom validation rule
omega make:event OrderPaid · make:listener NotifyAdmin
omega make:job SendInvoice · make:mail Invoice · make:notification Welcome
omega make:observer Post · make:middleware EnsureAdmin · make:test Pricing

omega queue:work [--queue default --concurrency 4]
omega queue:failed · queue:retry [--id N] · queue:flush [--status failed]
omega schedule:run · schedule:list
```

### Generating a complete resource

Like `artisan make:model -a`, a single command produces the model, its
migration, the request, the service and the CRUD controller — each in its own
layer, under a folder named after the domain.

The flags of `make:model` follow `artisan`'s, including when combined:

| Flag | Adds |
|---|---|
| `-m` `--migration` | the create migration (already implicit) |
| `-c` `--controller` | a controller |
| `-s` `--seeder` | a seeder |
| `-f` `--factory` | a factory |
| `-r` `--resource` | the five CRUD actions, with the request and the service |
| `-a` `--all` | everything above |

```bash
omega make:model Post -mcr    # model + migration + request + service + CRUD controller
omega make:model Post -a      # identical
omega make:model Post -sf     # model + migration + seeder + factory
omega make:controller Post -r # same scaffold, starting from the controller
omega make:resource Post      # an API resource alone (the presenter)
```

`--force` overwrites an existing file. It has **no** `-f` shorthand, which is
reserved for `--factory` as in Laravel.

```
app/models/post.go
database/migrations/<timestamp>_create_posts_table.go
app/requests/post/requests.go        type Payload
app/services/post/service.go         type Service — All, Create, Update, Delete
app/presenters/post/presenter.go     type View — what leaves the process
app/controllers/post/controller.go   Index, Store, Show, Update, Destroy
```

The generated controller goes through the presenter: it never returns a raw
model, in keeping with the framework's rule.

`omega make:resource Post` follows `artisan` and creates **an API resource
alone** — the presenter, nothing else. The full scaffold is `-a`, `-r` or
`-mcr`.

The command then prints the block of routes to paste into `routes/api.go`.

Without a flag, each generator writes **only** its file, and that file compiles
on its own: `make:controller Post` produces a standalone controller, with no
service or model to create first. Add `-f` to overwrite an existing file.

### Generators wire themselves in

Like migrations and seeders, generated files register through `init()`: nothing
to wire by hand.

| Command | Writes to | Wires itself through |
|---|---|---|
| `make:policy Post` | `app/policies/post.go` | `policies.Extend` |
| `make:rule Siret` | `app/rules/siret.go` | `validation.Rule` |
| `make:event OrderPaid` | `app/events/order_paid.go` | a typed emitter on `omega.Emit` |
| `make:mail Invoice` | `app/mails/invoice.go` | a `Message() mail.Message` method |
| `make:notification Welcome` | `app/notifications/welcome.go` | `omega.Notify` — `mail` and `broadcast` channels |
| `make:observer Post` | `app/observers/post.go` | `Register(db)` on the GORM callbacks |
| `make:test Pricing` | `tests/unit/pricing_test.go` | — |

Notifications declare their channels and implement what is needed:

```go
func (n Welcome) Channels() []string { return []string{"mail", "broadcast"} }
func (n Welcome) ToMail() mail.Message { ... }
func (n Welcome) ToBroadcast() (string, string, any) { ... }

omega.Notify(ctx, Welcome{To: user.Email, Name: user.Name})
```

## Queues, events, scheduler

The queue lives **in your database**, not in Redis: the binary stays
self-contained.

```go
omega.Dispatch(ctx, "welcome_email", map[string]any{"email": user.Email})
omega.Dispatch(ctx, "report", payload, omega.Job{Delay: time.Hour, MaxTries: 5})
```

A failing job is pushed back with exponential backoff capped at 10 minutes; past
`MaxTries` it goes to `failed` and waits for `queue:retry`. A worker killed
mid-flight loses nothing: its jobs are reclaimed after `stuck_after`.

```go
app.Events.Listen("user.registered", "log", listeners.LogRegistration(app.Log))
app.Events.ListenAsync("user.registered", "welcome", enqueueWelcome)

app.Scheduler.
    Every("purge-tokens", time.Hour, purgeTokens).
    DailyAt("report", "03:00", nightlyReport)
```

## Cache, storage, mail

```go
users, err := omega.Remember("users.count", 5*time.Minute, countUsers)

omega.Storage().PutBytes("invoices/42.pdf", pdf)
url := omega.Storage().URL("invoices/42.pdf")

omega.Mail().Send(omega.Message{To: []string{"ada@example.com"}, Subject: "Hello"})
```

Mail goes to `log` by default — nothing is sent until `MAIL_DRIVER=smtp` is set.
Storage refuses any path that escapes its root.

## Currency

Amounts are stored in one currency and read in another. Converting in storage
would make every total depend on the day it was written, and two sums over the
same month would stop agreeing — so nothing stored is ever converted.

```
GET /api/currencies          the catalogue and the day's rates (public)
PUT /api/settings/currency   what the caller reads amounts in
```

```go
omega.Currency().Convert(ctx, 1900, "USD")            // from the base currency
omega.Currency().ConvertFrom(ctx, 1900, "GBP", "USD") // between any two
omega.Currency().Format(1900, "EUR")                  // "€19.00"
```

Thirty currencies, all of them quoted by the European Central Bank — which is
what makes the catalogue a promise rather than a wish list: offering one the
provider does not quote would offer a choice that converts nothing. Rates are
fetched once, cached for twelve hours and refreshed on a schedule, so no visitor
waits on a remote call.

When the provider is down, the last table still held is served; with none at
all, amounts are returned unchanged in the base currency. An unavailable rate is
never a failure — the base beats nothing.

```yaml
# config/currency.yaml
base: ${CURRENCY_BASE:-EUR}
available: ${CURRENCY_AVAILABLE:-}   # empty offers the whole catalogue
```

## Billing

Stripe subscriptions, written against `net/http`: no billing SDK enters a project
built on Omega, and no SDK release can change what this layer does behind its
back.

```bash
BILLING_ENABLED=true
STRIPE_SECRET_KEY=sk_test_...
STRIPE_WEBHOOK_SECRET=whsec_...
STRIPE_PRICE_PRO=price_...
```

Enabling it registers six routes. A caller asks for a plan **by name** — the
price ids never leave the server, so a crafted body cannot subscribe someone to
a price that is not on sale:

| | |
|---|---|
| `GET /api/billing/plans` | what is on sale, priced in the caller's currency |
| `POST /api/billing/checkout` | opens a hosted checkout, answers with its URL |
| `POST /api/billing/portal` | opens Stripe's portal: card, invoices, cancelling |
| `GET /api/billing/subscription` | the caller's subscription, or `null` |
| `POST /api/billing/cancel` | at the end of the paid period, or `immediately` |
| `POST /api/billing/resume` | undoes a cancellation that has not taken effect |
| `POST /api/billing/webhook` | public, and authenticated by its signature |

The plans on sale are declared in `config/billing.yaml`:

```yaml
plans:
  - name: pro
    price: ${STRIPE_PRICE_PRO:-}
    trial_days: 14
```

**Stripe owns the truth.** Nothing here decides that someone is subscribed: a
checkout that succeeded is not a subscription until the webhook that follows it
says so. The rest of the application asks one question:

```go
billed := omega.C().Get("billing").(*billing.Service)
subscribed, err := billed.Subscribed(ctx, user.ID)
```

What the webhook endpoint guarantees:

- **The signature is checked on the raw body**, before anything is parsed, with a
  constant-time compare and a five-minute tolerance that bounds the replay
  window (booting with more than 15 minutes is refused). A body over
  `billing.webhook_max_bytes` (256 KiB) is refused with 413 before any
  signature is computed. Several `v1` signatures are accepted, so a secret can be rotated
  without dropping an event.
- **An event is applied exactly once.** Stripe delivers at least once and retries
  for days; the event id is the primary key of a ledger table, and the insert
  happens *inside the same transaction* as the change it describes. Either both
  land, or neither does and the next retry starts clean.
- **A test event against live keys is refused**, and the reverse too — the usual
  shape of handing out a paid plan for a payment that never happened.
- **The tables never reach the router.** `billing_customers`,
  `billing_subscriptions` and `billing_events` are excluded from schema
  discovery: published as resources they would let any authenticated caller POST
  themselves a subscription.

### Prices in the caller's currency

`GET /api/billing/plans` quotes each plan in the currency its reader chose, and
says plainly which of two things it is doing:

- the price is **declared in that currency on Stripe** (a multi-currency price):
  the amount is Stripe's own, `charged` is true, and the checkout bills in it;
- the price is **not**: the amount is converted at the day's rate for reading,
  `charged` is false, and `settles` names the currency the card is really
  charged in.

That distinction is the whole point. A converted figure is a guide, not a price,
and a customer is owed the difference before they click. Checkout names a
currency to Stripe only where the price declares one — naming another has the
session refused — and the subscription records what was actually billed.

Every write to Stripe carries an idempotency key, so a retry — ours or the
caller's — replays the first answer instead of creating a second customer.

Billing stays off until `BILLING_ENABLED=true`. Turning it on without a webhook
secret is refused at boot rather than accepted: an unsigned webhook is
indistinguishable from a forged one.

## Realtime and observability

```
GET /api/events?channels=orders   SSE stream, token or cookie required
GET /api/ws                       WebSocket, token or cookie, Origin checked
GET /api/metrics                  Prometheus format, protected
GET /api/stats                    the same as JSON, protected
GET /api/health                   public; the version only in local and testing
```

The metrics are not public. Prometheus sends the static token from
`METRICS_TOKEN` (`observability.metrics_token`, compared in constant time):

```yaml
scrape_configs:
  - job_name: omega
    metrics_path: /api/metrics
    authorization:
      credentials_file: /etc/prometheus/omega-token
```

A signed-in caller holding the `metrics.view` ability (admins hold every
ability) reads them too, unless `METRICS_ADMINS=false`. With no token and admins
off, both endpoints are not served at all.

```go
omega.Broadcast().Publish("orders", "created", order)
```

### Rate limiting

Three independent layers:

| Scope | Default | Variables |
|---|---|---|
| Every route, per address | **off** | `RATELIMIT_MAX` · `RATELIMIT_WINDOW` |
| Credentials, per address | **10 / min**, on | `RATELIMIT_AUTH_MAX` · `RATELIMIT_AUTH_WINDOW` |
| Account creation, per address | **10 / 10 min**, on | `RATELIMIT_REGISTER_MAX` · `RATELIMIT_REGISTER_WINDOW` |
| Token refresh, per address | **30 / min**, on | `RATELIMIT_REFRESH_MAX` · `RATELIMIT_REFRESH_WINDOW` |
| Failed sign-ins, per account | **8 / 15 min**, on | `RATELIMIT_ACCOUNT_MAX` · `RATELIMIT_ACCOUNT_WINDOW` |

The credentials limit covers `login`, `register` and `password`. Every limit is
keyed on the client address alone: an unverified bearer token is never a key,
or anyone could open a fresh bucket per request with random tokens.

The per-account limit is counted **in the database**, in `login_attempts`: it
therefore holds across replicas and restarts, and a distributed attack from
thousands of addresses does not get around it. The counter drops back to zero on
the first successful sign-in; an hourly task purges expired attempts.

Behind a reverse proxy, set `APP_PROXY_HEADER` and `APP_TRUSTED_PROXIES` (comma-
separated addresses or CIDRs) — otherwise every request shares the proxy's
address and the per-address limit becomes useless. The header is read only on
requests coming from an address in `APP_TRUSTED_PROXIES`, so setting
`APP_PROXY_HEADER` alone has no effect (a warning says so at boot). Prefer a
header the proxy overwrites (`X-Real-IP`) to one it appends to
(`X-Forwarded-For`), whose first entry the client writes.

### Security headers

Every answer carries `X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY`,
`Referrer-Policy: strict-origin-when-cross-origin`,
`Cross-Origin-Opener-Policy: same-origin`, `Cross-Origin-Resource-Policy:
same-site` and a `Permissions-Policy` that turns off camera, microphone,
geolocation, payment and the like (Fiber's helmet middleware). Outside local
development it also carries `Strict-Transport-Security: max-age=63072000;
includeSubDomains`.

The API (`/api/*`, `/graphql`) answers with `Content-Security-Policy:
default-src 'none'; frame-ancestors 'none'`. The two consoles set their own
policy: scripts and styles from the binary itself, their inline script pinned by
its hash, requests to the same origin only, no `eval`. Paths outside the API
(a single-page application served from the binary) set their own CSP.

Everything under `/api/auth` and every answer to an authenticated request is
sent with `Cache-Control: no-store`.

## Choosing your frontend

`web/` is disposable. Delete it, run one command, and the whole setup comes
back — home page, documentation, sign-in, dashboard, users, i18n, theme, typed
API client.

```bash
omega web:nuxt                    # Nuxt 4 (SPA) + shadcn-vue
omega web:react                   # React 19 (Vite) + shadcn/ui
omega web:next                    # Next.js 15 (App Router) + shadcn/ui
omega web:nuxt --force --install  # replace and install everything
```

| Flag | Effect |
|---|---|
| `--force` | replaces the existing frontend |
| `--install` | installs and adds the shadcn components |
| `--pm` | `bun`, `pnpm`, `yarn` or `npm` — detected otherwise |
| `--dir` | writes somewhere other than `web/` |

### Any package manager

Nothing is wired to bun. Omega picks in this order: the `--pm` flag, then the
lockfile present in `web/`, then the first installed manager. The templates ship
no lockfile and their npm scripts are neutral.

| | install | components | dev |
|---|---|---|---|
| bun | `bun install` | `bunx --bun` | `bun run dev` |
| pnpm | `pnpm install` | `pnpm dlx` | `pnpm run dev` |
| yarn | `yarn install` | `yarn dlx` (npx on v1) | `yarn run dev` |
| npm | `npm install` | `npx --yes` | `npm run dev` |

The API port is **read from your configuration** at generation time: if
`APP_PORT=3300`, the development proxy aims at 3300, not 3000.

The design-system tokens, **the API client**, the OpenAPI types, the
documentation content and the translations are **shared** between the stacks:
one source file, copied to the location each framework expects. Change a colour
and all three frontends follow.

### Signing in is identical everywhere

`api.ts` is written once and knows no framework. All three frontends therefore
authenticate in exactly the same way: tokens in `localStorage`, `Bearer` header,
refresh rotation on the fly on a 401, sign-out propagated to the other tabs.

Each stack supplies only the reactivity: a `ref` on the Nuxt side, a context on
the React and Next sides, both wired to the same `subscribe()`. A test refuses
to let a stack ship its own copy of the client.

`components.json` is preconfigured, so `bunx shadcn-vue add <component>` (or
`shadcn` on the React side) works immediately and inherits the tokens.

## The Nuxt frontend

The repository carries a Nuxt 4 application that consumes the API — `web/`. It
is separate from the Go binary: Omega serves JSON only.

```bash
cd web
bun install
bun run dev          # http://localhost:5173
bun run build        # static output in .output/public
```

**SPA, no SSR** (`ssr: false`): `bun run build` produces static files, so no
Node server in production — any static host, or Omega itself, can serve them.
Vite forwards `/api` and `/graphql` to Omega in development (`OMEGA_URL` to
change the port). In production, `CORS_ORIGINS` names who may call the API from
a browser.

| | |
|---|---|
| Nuxt 4 · Vue 3 · TypeScript | the base |
| Tailwind 4 | the design-system tokens |
| Own components | 17 files in `web/app/components/ui`, no UI dependency |
| `useAsyncData` | caching and loading states, supplied by Nuxt |
| Own i18n | a 50-line composable — English, French, Spanish |
| lucide-vue-next | the icons, tree-shaken |

Overlays lean on the native `<dialog>` element: focus trapping and backdrop
handled by the browser, without a library.

### The client is typed from the API

```bash
curl -s http://127.0.0.1:3000/api/openapi.json > openapi.json
bun x openapi-typescript openapi.json -o app/lib/api-types.ts
```

Rename a column in a migration, regenerate: TypeScript points at every line that
no longer holds. The document comes from the registry that serves the routes, so
it cannot describe an endpoint that does not exist.

### What the frontend contains

```
/                 landing page
/docs             documentation, 20 pages across 5 sections
/login            sign-in and sign-up
/dashboard        statistics, collapsible sidebar
/users            paginated and filtered list, detail view
```

Token refresh is transparent: a 401 triggers a rotation, then replays the
request. Only one rotation is in flight at a time, even if ten requests fail
together.

## Configuration

Each file in `config/` is namespaced under its base name: `config/app.yaml` →
`app.port`, `config/auth.yaml` → `auth.secret`.

```go
omega.Cfg().Int("app.port")
omega.Cfg().String("auth.secret")
```

Environment variables win, and `.env` is loaded automatically. The generated
API reads `config/api.yaml`:

```yaml
discovery: ${API_DISCOVERY:-}                # all | none | allowlist; empty: none in production
discovery_tables: ${API_DISCOVERY_TABLES:-}  # for allowlist
limits:
  max_filters: 10
  max_in_values: 100
  max_pattern_length: 128
  max_wildcards: 8
graphql:
  max_depth: 15
  max_fields: 500
  max_root_fields: 10
  max_complexity: 10000
```

For SQLite, `DB_SQLITE_PATH` is a file path; `DB_DATABASE` remains the database
name for PostgreSQL and MySQL.

The security settings, and their defaults:

| Key | Variable | Default |
|---|---|---|
| `auth.secret` | `AUTH_SECRET` | none; 32 bytes at least outside testing |
| `auth.reuse_grace` | `AUTH_REUSE_GRACE` | `0s` (strict) |
| `auth.cookies.enabled` | `AUTH_COOKIES` | `true` |
| `auth.cookies.domain` | `AUTH_COOKIE_DOMAIN` | empty (host-only) |
| `auth.cookies.secure` | `AUTH_COOKIE_SECURE` | on outside local and testing |
| `auth.cookies.same_site` | `AUTH_COOKIE_SAMESITE` | `Lax` (refresh cookie always `Strict`) |
| `app.cors.origins` | `CORS_ORIGINS` | `http://localhost:5173`; `*` refused with credentials |
| `app.cors.credentials` | `CORS_CREDENTIALS` | `true` |
| `app.ratelimit.register.*` | `RATELIMIT_REGISTER_MAX` · `_WINDOW` | `10` / `10m` |
| `app.ratelimit.refresh.*` | `RATELIMIT_REFRESH_MAX` · `_WINDOW` | `30` / `1m` |
| `api.docs.public` | `API_DOCS_PUBLIC` | on in local and testing, off elsewhere |
| `api.public_introspection` | `API_PUBLIC_INTROSPECTION` | on in local and testing, off elsewhere |
| `observability.metrics_token` | `METRICS_TOKEN` | empty |
| `observability.metrics_admins` | `METRICS_ADMINS` | `true` |
| `billing.webhook_max_bytes` | — | `262144` |

## Extensibility

```go
omega.Bind("payments", NewStripeGateway(...))
gateway, err := omega.Make[*StripeGateway]("payments")
```

No reflection, no autowiring: the kernel's services are typed fields on the
container.

## Roadmap

| Piece | Status |
|---|---|
| Kernel, router, database, JSON renderer, CLI | done |
| REST + OpenAPI + GraphQL generated from the schema | done |
| JWT auth, revocable refresh tokens, OAuth providers | done |
| Validation, services, presenters | done |
| Queue (database), events, scheduler | done |
| Cache, storage, mail, rate limiting | done |
| Broadcast (SSE) | done |
| Observability (Prometheus metrics) | done |
| Policies and fine-grained permissions | done |
| WebSocket broadcast | done |
| `omega new myapp` | done |
| HTTP integration tests · CI | done |
| Translation of the /docs content (en · fr · es) | done |
| Rate limit per token, not per IP | done |
| Automatic purge of failed jobs | done |
| Policies wired to the generated routes | done |
| Tests for the eight service pieces | done |
| Billing: Stripe subscriptions, signed and idempotent webhooks | done |
| Currency: ECB rates, display conversion, per-account choice | done |

## MCP server

The binary exposes the application to an agent, through the Model Context
Protocol:

```bash
omega mcp                      # JSON-RPC 2.0 over stdin/stdout
claude mcp add omega -- omega mcp
```

The repository already contains an `.mcp.json`, so Claude Code detects it on its
own. Eight tools:

| Tool | Answers with |
|---|---|
| `omega_overview` | stack, layout and conventions — read this first |
| `omega_routes` | the routes actually registered, with their names |
| `omega_models` | the GORM models, columns, tags and methods |
| `omega_openapi` | the generated OpenAPI document |
| `omega_schema` | the tables and columns of the connected database |
| `omega_commands` | every CLI command with its flags |
| `omega_search` | grep across the code and the documentation |
| `omega_read` | the contents of a project file |

The protocol is implemented by hand in `internal/mcp`: no extra dependency
enters a project built on the framework.

## Contributing

Contributions are welcome — fixes, tests, translations, database drivers.

```bash
go test ./...          # the full suite
go test -race ./...    # with the race detector
go vet ./...
```

Two conventions are worth knowing before opening a pull request:

- **No decorative comments.** The code explains itself through its names. A
  comment only survives if it says *why*, never *what*.
- **One defect, one test.** Every fix arrives with a test that fails without it.

Tests that reach unexported identifiers live next to the code they test, under
the `_internal_test.go` suffix. The others are in `tests/unit` and
`tests/integration`.

## Licence

[MIT](LICENSE) — Copyright (c) 2026 Qodbox.

Free to use, modify and redistribute, including commercially.
