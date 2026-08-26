package mcp

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func Overview(version string) string {
	return `Omega ` + version + ` — Go JSON API framework.

STACK
  Fiber v2 (HTTP) · GORM (SQLite/PostgreSQL/MySQL) · REST + OpenAPI + GraphQL
  JWT auth (golang-jwt) · OAuth providers (goth) · validation (go-playground)
  Frontend: web/ is disposable — omega web:nuxt or web:react rebuilds it whole\n  (pages, auth, i18n, theme, typed client). Tokens and docs content are shared.

LAYOUT
  omega.go              public façade; application code imports this and nothing else from the core
  internal/kernel       boot, config, logger, JSON errors, middleware, project root discovery
  internal/router       named routes and URL generation
  internal/api          the registry: REST routes, OpenAPI document, GraphQL schema, embedded consoles
  internal/auth         JWT guard, revocable refresh tokens, OAuth providers
  internal/validation   binds a JSON body to a struct and reports a 422 per field
  internal/queue        database-backed jobs, worker, retries, crash recovery
  internal/events       in-process bus, synchronous or deferred listeners
  internal/scheduler    periodic and daily tasks
  internal/cache        key-value in memory with a TTL
  internal/mail         log and SMTP drivers
  internal/storage      local disk, paths confined to the root
  internal/broadcast    server-sent events with channels
  internal/observability Prometheus metrics per route
  internal/database     connection manager, migrations, seeders, factories
  app/models            GORM models — data, no business logic
  app/requests/<domain> what a client may send, with its validate tags
  app/services/<domain> what the application does; takes a context, never a fiber.Ctx
  app/presenters/<domain> what leaves the process
  app/controllers/<domain> parse, call a service, answer
  app/jobs              the queued tasks
  app/listeners         what reacts to an event
  routes/               api.go · auth.go · auth_docs.go · workers.go
  config/               app · database · auth · queue · mail · storage
  web/                  the Nuxt interface

CONVENTIONS
  · A handler returns JSON. There is no template engine and no session.
  · Bind and validate in one call: body, err := validation.Bind[requests.X](c).
    A failure returns *validation.Error, rendered by the kernel as a 422 with
    one message per field — never build that response by hand.
  · Business logic lives in app/services/<domain> and takes a context.Context.
  · Never return a model directly: app/presenters/<domain> decides what is public.
  · Everything under /api needs a bearer token except /auth/*, /health, /docs,
    /openapi.json, /metrics and /stats.
  · omega.Model[T]() binds :id to a *T before the handler runs, or answers 404.
  · api.Register[T](registry, plural, singular, hidden...) publishes a model in
    one line; every other table is discovered from the live schema at boot.
  · A route with no model behind it documents itself with registry.Document.
  · Any command may run from a subdirectory: the kernel walks up to the project
    root, the directory holding config/app.yaml.

SERVICES
  omega.Dispatch(ctx, "job_name", payload, omega.Job{Delay: time.Hour, MaxTries: 5})
  omega.Emit(ctx, "user.registered", payload)
  omega.Remember("key", 5*time.Minute, build)
  omega.Storage().PutBytes(name, bytes)
  omega.Mail().Send(omega.Message{To: []string{...}, Subject: "..."})
  omega.Broadcast().Publish("channel", "event", data)
  omega.Notify(ctx, notifications.Welcome{To: email, Name: name})
  Handlers, listeners and the schedule are registered in routes/workers.go.

AUTH
  POST /api/auth/register · login · refresh · logout · password
  GET  /api/auth/me · providers · {provider} · {provider}/callback
  Access tokens are short and stateless; refresh tokens are single-use, stored
  in refresh_tokens, revoked on logout or password change. The guard reads the
  claims only — the user row loads lazily, when a handler asks for it.

CONSOLES AND SIGNALS
  GET /api/docs    Swagger UI        GET /graphql    GraphiQL
  GET /api/metrics Prometheus text   GET /api/stats  the same as JSON
  GET /api/events  server-sent events, token required
  Both consoles are embedded in the binary — never add a CDN reference.

GENERATE RATHER THAN WRITE
  omega make:resource Post   an API resource (the presenter), as in artisan
  omega make:model Post -a   model + migration + request + service + presenter + controller
  omega make:model Post -mcr same thing (-m -c -s -f -r combine, as in artisan)
  omega make:controller Post -r   same thing, starting from the controller
  omega make:service Post · make:request Post
  omega make:job SendInvoice · make:listener NotifyAdmin
  omega make:policy Post · make:rule Siret · make:event OrderPaid
  omega make:mail Invoice · make:notification Welcome · make:observer Post
  omega make:test Pricing · make:presenter Post (alias of make:resource)
  Generated policies, rules, events and observers register themselves in init().
  omega make:migration add_slug · make:seed · make:factory · make:middleware
  A generator prints the route block to paste into routes/api.go.

RUNNING
  omega serve [--port N] · omega dev (air) · omega build
  omega migrate · migrate:status · migrate:rollback [--step N] · migrate:reset
  omega migrate:fresh [--seed] · omega db:seed
  omega queue:work [--queue --concurrency] · queue:failed · queue:retry · queue:flush
  omega schedule:run · schedule:list
  omega web:nuxt · web:react · web:next   rebuild web/ from scratch
    --force replaces it, --install runs the package manager, --pm picks one
    (bun, pnpm, yarn, npm — detected from the lockfile otherwise)
    The API client is shared: all three stacks authenticate identically.
    The proxy targets the port this project actually listens on.
  omega routes · omega mcp`
}

func Models(dir string) (string, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil || len(files) == 0 {
		return "", fmt.Errorf("no model found in %s", dir)
	}

	var out strings.Builder
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		fset := token.NewFileSet()
		parsed, err := parser.ParseFile(fset, file, nil, 0)
		if err != nil {
			continue
		}

		ast.Inspect(parsed, func(node ast.Node) bool {
			spec, ok := node.(*ast.TypeSpec)
			if !ok {
				return true
			}
			structType, ok := spec.Type.(*ast.StructType)
			if !ok {
				return true
			}

			fmt.Fprintf(&out, "%s (%s)\n", spec.Name.Name, filepath.Base(file))
			for _, field := range structType.Fields.List {
				names := make([]string, 0, len(field.Names))
				for _, name := range field.Names {
					names = append(names, name.Name)
				}
				tag := ""
				if field.Tag != nil {
					tag = "  " + strings.Trim(field.Tag.Value, "`")
				}
				fmt.Fprintf(&out, "    %-18s %-16s%s\n", strings.Join(names, ", "), typeName(field.Type), tag)
			}
			return true
		})

		for _, method := range methodsOf(parsed) {
			fmt.Fprintf(&out, "    method %s\n", method)
		}
		out.WriteString("\n")
	}
	return out.String(), nil
}

func methodsOf(file *ast.File) []string {
	var methods []string
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv == nil {
			continue
		}
		methods = append(methods, fn.Name.Name+"()")
	}
	sort.Strings(methods)
	return methods
}

func typeName(expr ast.Expr) string {
	switch typed := expr.(type) {
	case *ast.Ident:
		return typed.Name
	case *ast.StarExpr:
		return "*" + typeName(typed.X)
	case *ast.SelectorExpr:
		return typeName(typed.X) + "." + typed.Sel.Name
	case *ast.ArrayType:
		return "[]" + typeName(typed.Elt)
	case *ast.MapType:
		return "map[" + typeName(typed.Key) + "]" + typeName(typed.Value)
	default:
		return "?"
	}
}

func Search(root, term string, limit int) (string, error) {
	if strings.TrimSpace(term) == "" {
		return "", fmt.Errorf("a search term is required")
	}
	needle := strings.ToLower(term)

	var out strings.Builder
	matches := 0

	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || matches >= limit {
			return nil
		}
		if entry.IsDir() {
			switch entry.Name() {
			case "node_modules", ".git", "bin", "tmp", ".claude":
				return fs.SkipDir
			}
			return nil
		}

		if entry.Name() == "build.css" {
			return nil
		}
		switch filepath.Ext(path) {
		case ".go", ".md", ".html", ".css", ".yaml", ".stub":
		default:
			return nil
		}

		raw, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		relative, _ := filepath.Rel(root, path)
		for number, line := range strings.Split(string(raw), "\n") {
			if !strings.Contains(strings.ToLower(line), needle) {
				continue
			}
			fmt.Fprintf(&out, "%s:%d: %s\n", relative, number+1, strings.TrimSpace(line))
			if matches++; matches >= limit {
				return fs.SkipAll
			}
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if matches == 0 {
		return "no match for " + term, nil
	}
	return out.String(), nil
}

func Read(root, name string) (string, error) {
	base := filepath.Clean(root)
	clean := filepath.Clean(filepath.Join(base, name))
	if !strings.HasPrefix(clean, base+string(os.PathSeparator)) {
		return "", fmt.Errorf("%s is outside the project", name)
	}

	realBase, err := filepath.EvalSymlinks(base)
	if err != nil {
		return "", err
	}
	target, err := filepath.EvalSymlinks(clean)
	if err != nil {
		return "", err
	}
	if target != realBase && !strings.HasPrefix(target, realBase+string(os.PathSeparator)) {
		return "", fmt.Errorf("%s is outside the project", name)
	}

	raw, err := os.ReadFile(target)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}
