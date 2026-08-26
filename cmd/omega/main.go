package main

import (
	"fmt"
	"net/http"
	_ "net/http/pprof"
	"os"

	"github.com/spf13/cobra"

	"omega"
	_ "omega/app/rules"
	_ "omega/database/migrations"
	_ "omega/database/seeds"
)

var configDir = "config"

func init() {
	if os.Getenv("OMEGA_PPROF") != "" {
		go func() { _ = http.ListenAndServe("127.0.0.1:6060", nil) }()
	}
}

func main() {
	root := &cobra.Command{
		Use:     "omega",
		Short:   "Omega — Go JSON API framework (Fiber · GORM · OpenAPI · GraphQL)",
		Version: omega.Version,
		CompletionOptions: cobra.CompletionOptions{
			HiddenDefaultCmd: true,
		},
		SilenceUsage: true,
	}

	root.PersistentFlags().StringVar(&configDir, "config", "config", "configuration directory")

	root.AddCommand(
		serveCmd(),
		devCmd(),
		buildCmd(),
		testCmd(),
		routesCmd(),
		mcpCmd(),
	)
	root.AddCommand(newCommand())
	root.AddCommand(migrationCommands()...)
	root.AddCommand(makeCommands()...)
	root.AddCommand(webCommands()...)
	root.AddCommand(queueCommands()...)
	root.AddCommand(scheduleCommands()...)

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
