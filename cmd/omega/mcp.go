package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"omega"
	"omega/internal/api"
	"omega/internal/mcp"
)

func mcpCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "mcp",
		Short: "Serve the Model Context Protocol on stdin/stdout",
		Long: "Exposes the application to an agent: its routes, models, database schema,\n" +
			"commands and conventions. Register it with:\n" +
			`  claude mcp add omega -- omega mcp`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return mcp.New(omega.Version, mcpTools(cmd.Root())).Serve(os.Stdin, os.Stdout)
		},
	}
}

func mcpTools(root *cobra.Command) []mcp.Tool {
	return []mcp.Tool{
		{
			Name:        "omega_overview",
			Title:       "How this framework works",
			Description: "Stack, directory layout and the conventions every other tool assumes. Read this first.",
			InputSchema: mcp.Schema(nil),
			Run: func(map[string]any) (string, error) {
				return mcp.Overview(omega.Version), nil
			},
		},
		{
			Name:        "omega_routes",
			Title:       "Registered routes",
			Description: "Every route the application serves, with its method, path and name.",
			InputSchema: mcp.Schema(nil),
			Run:         func(map[string]any) (string, error) { return routeTable() },
		},
		{
			Name:        "omega_models",
			Title:       "GORM models",
			Description: "Models in app/models with their columns, tags and methods.",
			InputSchema: mcp.Schema(nil),
			Run:         func(map[string]any) (string, error) { return mcp.Models("app/models") },
		},
		{
			Name:        "omega_openapi",
			Title:       "OpenAPI description",
			Description: "The generated OpenAPI document: every path, its schemas and its security.",
			InputSchema: mcp.Schema(nil),
			Run:         func(map[string]any) (string, error) { return openAPIDocument() },
		},
		{
			Name:        "omega_schema",
			Title:       "Database schema",
			Description: "Tables and columns as they exist in the connected database.",
			InputSchema: mcp.Schema(nil),
			Run:         func(map[string]any) (string, error) { return databaseSchema() },
		},
		{
			Name:        "omega_commands",
			Title:       "CLI reference",
			Description: "Every omega command with its flags, including the generators.",
			InputSchema: mcp.Schema(nil),
			Run:         func(map[string]any) (string, error) { return commandTree(root), nil },
		},
		{
			Name:        "omega_search",
			Title:       "Search the project",
			Description: "Grep the sources, templates and documentation for a term.",
			InputSchema: mcp.Schema(map[string]string{"term": "what to look for"}, "term"),
			Run: func(args map[string]any) (string, error) {
				return mcp.Search(".", text(args, "term"), 60)
			},
		},
		{
			Name:        "omega_read",
			Title:       "Read a project file",
			Description: "Return one file of the project, by path relative to its root.",
			InputSchema: mcp.Schema(map[string]string{"path": "path relative to the project root"}, "path"),
			Run: func(args map[string]any) (string, error) {
				return mcp.Read(".", text(args, "path"))
			},
		},
	}
}

func text(args map[string]any, key string) string {
	value, _ := args[key].(string)
	return strings.TrimSpace(value)
}

func routeTable() (string, error) {
	app, err := bootWeb()
	if err != nil {
		return "", err
	}
	defer func() { _ = app.Shutdown() }()

	type entry struct{ method, path, name string }
	var entries []entry
	seen := map[string]bool{}

	for _, stack := range app.Fiber.Stack() {
		for _, route := range stack {
			if route.Method == "HEAD" || route.Name == "" {
				continue
			}
			key := route.Method + " " + route.Path
			if seen[key] {
				continue
			}
			seen[key] = true
			entries = append(entries, entry{route.Method, route.Path, route.Name})
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].path == entries[j].path {
			return entries[i].method < entries[j].method
		}
		return entries[i].path < entries[j].path
	})

	var out strings.Builder
	for _, e := range entries {
		fmt.Fprintf(&out, "%-7s %-28s %s\n", e.method, e.path, e.name)
	}
	return out.String(), nil
}

func openAPIDocument() (string, error) {
	app, err := bootWeb()
	if err != nil {
		return "", err
	}
	defer func() { _ = app.Shutdown() }()

	registry, err := omega.Make[*api.Registry]("api")
	if err != nil {
		return "", err
	}

	document := registry.OpenAPI(app.Cfg.StringOr("app.name", "Omega")+" API", omega.Version, "/api")
	raw, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func databaseSchema() (string, error) {
	app, err := bootCLI()
	if err != nil {
		return "", err
	}
	defer func() { _ = app.Shutdown() }()

	migrator := app.DB.Migrator()
	tables, err := migrator.GetTables()
	if err != nil {
		return "", err
	}
	sort.Strings(tables)

	var out strings.Builder
	for _, table := range tables {
		fmt.Fprintf(&out, "%s\n", table)
		columns, err := migrator.ColumnTypes(table)
		if err != nil {
			continue
		}
		for _, column := range columns {
			nullable, _ := column.Nullable()
			fmt.Fprintf(&out, "    %-24s %-14s null=%v\n", column.Name(), column.DatabaseTypeName(), nullable)
		}
	}
	return out.String(), nil
}

func commandTree(root *cobra.Command) string {
	var out strings.Builder
	for _, cmd := range root.Commands() {
		if cmd.Hidden {
			continue
		}
		fmt.Fprintf(&out, "%-26s %s\n", cmd.Use, cmd.Short)
		cmd.LocalFlags().VisitAll(func(flag *pflag.Flag) {
			fmt.Fprintf(&out, "      --%-20s %s\n", flag.Name, flag.Usage)
		})
	}
	return out.String()
}
