package main

import (
	"bytes"
	"embed"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"strings"
	"text/template"
	"time"
	"unicode"

	"github.com/jinzhu/inflection"
	"github.com/spf13/cobra"
)

//go:embed stubs/*.stub
var stubs embed.FS

type generator struct {
	name  string
	short string
	stub  string
	path  func(n names) string
}

type names struct {
	Name      string
	Var       string
	Snake     string
	Plural    string
	Title     string
	Timestamp string
	Package   string
}

var generators = []generator{
	{
		name:  "controller",
		short: "Create a controller",
		stub:  "controller",
		path:  func(n names) string { return filepath.Join("app", "controllers", n.Snake, "controller.go") },
	},
	{
		name:  "model",
		short: "Create a model and its migration",
		stub:  "model",
		path:  func(n names) string { return filepath.Join("app", "models", n.Snake+".go") },
	},
	{
		name:  "seed",
		short: "Create a seeder",
		stub:  "seed",
		path:  func(n names) string { return filepath.Join("database", "seeds", n.Snake+"_seed.go") },
	},
	{
		name:  "factory",
		short: "Create a factory",
		stub:  "factory",
		path:  func(n names) string { return filepath.Join("database", "factories", n.Snake+"_factory.go") },
	},
	{
		name:  "service",
		short: "Create a service",
		stub:  "service",
		path:  func(n names) string { return filepath.Join("app", "services", n.Snake, "service.go") },
	},
	{
		name:  "resource",
		short: "Create an API resource — the shape that leaves the process",
		stub:  "presenter",
		path:  func(n names) string { return filepath.Join("app", "presenters", n.Snake, "presenter.go") },
	},
	{
		name:  "request",
		short: "Create a request with its validation rules",
		stub:  "request",
		path:  func(n names) string { return filepath.Join("app", "requests", n.Snake, "requests.go") },
	},
	{
		name:  "policy",
		short: "Create a policy — who may do what",
		stub:  "policy",
		path:  func(n names) string { return filepath.Join("app", "policies", n.Snake+".go") },
	},
	{
		name:  "rule",
		short: "Create a custom validation rule",
		stub:  "rule",
		path:  func(n names) string { return filepath.Join("app", "rules", n.Snake+".go") },
	},
	{
		name:  "event",
		short: "Create an event payload and its emitter",
		stub:  "event",
		path:  func(n names) string { return filepath.Join("app", "events", n.Snake+".go") },
	},
	{
		name:  "notification",
		short: "Create a notification — mail and broadcast channels",
		stub:  "notification",
		path:  func(n names) string { return filepath.Join("app", "notifications", n.Snake+".go") },
	},
	{
		name:  "mail",
		short: "Create a mailable",
		stub:  "mail",
		path:  func(n names) string { return filepath.Join("app", "mails", n.Snake+".go") },
	},
	{
		name:  "observer",
		short: "Create a model observer",
		stub:  "observer",
		path:  func(n names) string { return filepath.Join("app", "observers", n.Snake+".go") },
	},
	{
		name:  "test",
		short: "Create a unit test",
		stub:  "test",
		path:  func(n names) string { return filepath.Join("tests", "unit", n.Snake+"_test.go") },
	},
	{
		name:  "job",
		short: "Create a queued job",
		stub:  "job",
		path:  func(n names) string { return filepath.Join("app", "jobs", n.Snake+".go") },
	},
	{
		name:  "listener",
		short: "Create an event listener",
		stub:  "listener",
		path:  func(n names) string { return filepath.Join("app", "listeners", n.Snake+".go") },
	},
	{
		name:  "middleware",
		short: "Create a middleware",
		stub:  "middleware",
		path:  func(n names) string { return filepath.Join("app", "middleware", n.Snake+".go") },
	},
}

func makeCommands() []*cobra.Command {
	commands := make([]*cobra.Command, 0, len(generators)+1)

	for _, gen := range generators {
		commands = append(commands, makeCommand(gen))
	}
	commands = append(commands, makeMigrationCommand())
	commands = append(commands, aliasCommand("make:seeder", "make:seed"), aliasCommand("make:presenter", "make:resource"))
	return commands
}

func scaffoldResource(n names, force bool) error {
	steps := []struct{ stub, dest string }{
		{"model", filepath.Join("app", "models", n.Snake+".go")},
		{"migration_create", migrationPath(n, "create_"+n.Plural+"_table")},
		{"request", filepath.Join("app", "requests", n.Snake, "requests.go")},
		{"service_resource", filepath.Join("app", "services", n.Snake, "service.go")},
		{"presenter_resource", filepath.Join("app", "presenters", n.Snake, "presenter.go")},
		{"controller_resource", filepath.Join("app", "controllers", n.Snake, "controller.go")},
	}
	for _, step := range steps {
		if err := generate(step.stub, step.dest, n, force); err != nil {
			return err
		}
	}

	printRoutes(n)
	return nil
}

func printRoutes(n names) {
	fmt.Printf(`
Add the routes to routes/api.go:

	%s := controller.NewController(service.NewService(app.DB))
	group.Get("/%s", %s.Index).Name("%s.index")
	group.Post("/%s", %s.Store).Name("%s.store")
	group.Get("/%s/:id", omega.Model[models.%s](), %s.Show).Name("%s.show")
	group.Put("/%s/:id", omega.Model[models.%s](), %s.Update).Name("%s.update")
	group.Delete("/%s/:id", omega.Model[models.%s](), %s.Destroy).Name("%s.destroy")

Or, to get them without writing a controller at all:

	api.Register[models.%s](registry, "%s", "%s")

Then: omega migrate
`, n.Plural,
		n.Plural, n.Plural, n.Plural,
		n.Plural, n.Plural, n.Plural,
		n.Plural, n.Name, n.Plural, n.Plural,
		n.Plural, n.Name, n.Plural, n.Plural,
		n.Plural, n.Name, n.Plural, n.Plural,
		n.Name, n.Plural, n.Snake)
}

func makeCommand(gen generator) *cobra.Command {
	var force, resource, all, migration, controller, seeder, factory bool

	scaffolds := gen.name == "model" || gen.name == "controller"

	cmd := &cobra.Command{
		Use:   fmt.Sprintf("make:%s <name>", gen.name),
		Short: gen.short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			n := parseNames(args[0])

			if scaffolds && (all || resource) {
				return scaffoldResource(n, force)
			}

			if err := generate(gen.stub, gen.path(n), n, force); err != nil {
				return err
			}

			if gen.name == "model" || migration {
				if err := generate("migration_create", migrationPath(n, "create_"+n.Plural+"_table"), n, force); err != nil {
					return err
				}
			}
			if controller {
				if err := generate("controller", filepath.Join("app", "controllers", n.Snake, "controller.go"), n, force); err != nil {
					return err
				}
			}
			if seeder {
				if err := generate("seed", filepath.Join("database", "seeds", n.Snake+"_seed.go"), n, force); err != nil {
					return err
				}
			}
			if factory {
				if err := generate("factory", filepath.Join("database", "factories", n.Snake+"_factory.go"), n, force); err != nil {
					return err
				}
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "overwrite an existing file")
	if scaffolds {
		cmd.Flags().BoolVarP(&all, "all", "a", false, "model, migration, request, service and controller")
		cmd.Flags().BoolVarP(&resource, "resource", "r", false, "the five CRUD actions, with the model, request and service they need")
	}
	if gen.name == "model" {
		cmd.Flags().BoolVarP(&migration, "migration", "m", false, "also create the migration (implied)")
		cmd.Flags().BoolVarP(&controller, "controller", "c", false, "also create a controller")
		cmd.Flags().BoolVarP(&seeder, "seeder", "s", false, "also create a seeder")
		cmd.Flags().BoolVarP(&factory, "factory", "f", false, "also create a factory")
	}
	return cmd
}

func makeMigrationCommand() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "make:migration <name>",
		Short: "Create a blank migration",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			raw := args[0]
			n := parseNames(raw)
			return generate("migration", migrationPath(n, toSnake(raw)), n, force)
		},
	}

	cmd.Flags().BoolVarP(&force, "force", "f", false, "overwrite an existing file")
	return cmd
}

func migrationPath(n names, slug string) string {
	return filepath.Join("database", "migrations", n.Timestamp+"_"+slug+".go")
}

func generate(stub, dest string, n names, force bool) error {
	if _, err := os.Stat(dest); err == nil && !force {
		return fmt.Errorf("%s already exists (use --force to overwrite)", dest)
	}

	raw, err := stubs.ReadFile("stubs/" + stub + ".stub")
	if err != nil {
		return fmt.Errorf("unknown stub %q: %w", stub, err)
	}

	tmpl, err := template.New(stub).Parse(string(raw))
	if err != nil {
		return fmt.Errorf("parse stub %q: %w", stub, err)
	}

	n.Package = filepath.Base(filepath.Dir(dest))

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, n); err != nil {
		return fmt.Errorf("render stub %q: %w", stub, err)
	}

	out := buf.Bytes()
	if strings.HasSuffix(dest, ".go") {
		formatted, err := format.Source(out)
		if err != nil {
			return fmt.Errorf("stub %q produced invalid Go: %w", stub, err)
		}
		out = formatted
	}

	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(dest, out, 0o644); err != nil {
		return err
	}

	fmt.Println("created", dest)
	return nil
}

func parseNames(raw string) names {
	pascal := toPascal(raw)
	snake := toSnake(pascal)

	return names{
		Name:   pascal,
		Var:    lowerFirst(pascal),
		Snake:  snake,
		Plural: inflection.Plural(snake),
		Title:  upperFirst(strings.ReplaceAll(snake, "_", " ")),

		Timestamp: time.Now().Format("20060102150405"),
	}
}

func toPascal(raw string) string {
	var out strings.Builder
	upperNext := true

	for _, r := range raw {
		switch {
		case r == '_' || r == '-' || r == ' ' || r == '.':
			upperNext = true
		case upperNext:
			out.WriteRune(unicode.ToUpper(r))
			upperNext = false
		default:
			out.WriteRune(r)
		}
	}
	return out.String()
}

func toSnake(raw string) string {
	var out strings.Builder
	runes := []rune(raw)

	for i, r := range runes {
		switch {
		case r == '-' || r == ' ' || r == '.':
			out.WriteRune('_')
		case unicode.IsUpper(r):

			if i > 0 && (unicode.IsLower(runes[i-1]) || (i+1 < len(runes) && unicode.IsLower(runes[i+1]))) {
				out.WriteRune('_')
			}
			out.WriteRune(unicode.ToLower(r))
		default:
			out.WriteRune(r)
		}
	}
	return strings.Trim(strings.ReplaceAll(out.String(), "__", "_"), "_")
}

func upperFirst(s string) string {
	if s == "" {
		return s
	}
	runes := []rune(s)
	runes[0] = unicode.ToUpper(runes[0])
	return string(runes)
}

func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	runes := []rune(s)
	runes[0] = unicode.ToLower(runes[0])
	return string(runes)
}

func aliasCommand(name, target string) *cobra.Command {
	return &cobra.Command{
		Use:                name + " <name>",
		Short:              "Alias of " + target,
		Args:               cobra.ArbitraryArgs,
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			root := cmd.Root()
			root.SetArgs(append([]string{strings.TrimPrefix(target, "")}, args...))
			return root.Execute()
		},
	}
}
