package main

import (
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"omega"
	"omega/routes"
)

func bootWeb() (*omega.App, error) {
	app, err := omega.Boot(omega.WithConfigDir(configDir))
	if err != nil {
		return nil, err
	}

	if err := routes.RegisterAPI(app); err != nil {
		return nil, err
	}
	return app, nil
}

func bootWorker() (*omega.App, error) {
	app, err := omega.Boot(omega.WithConfigDir(configDir), omega.WithoutHTTP())
	if err != nil {
		return nil, err
	}
	if err := routes.RegisterWorkers(app); err != nil {
		return nil, err
	}
	return app, nil
}

func bootCLI() (*omega.App, error) {
	return omega.Boot(omega.WithConfigDir(configDir), omega.WithoutHTTP())
}

func serveCmd() *cobra.Command {
	var port int

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the HTTP server",
		RunE: func(cmd *cobra.Command, args []string) error {
			if port > 0 {
				os.Setenv("APP_PORT", fmt.Sprint(port))
			}

			app, err := bootWeb()
			if err != nil {
				return err
			}

			if behind, stale := staleBinary(); stale && app.Cfg.IsLocal() {
				app.Log.Warn().
					Str("behind", behind.Round(time.Second).String()).
					Msg("this binary is older than the sources — run go install ./cmd/omega, or use omega dev")
			}

			return app.Run()
		},
	}

	cmd.Flags().IntVarP(&port, "port", "p", 0, "override the configured port")
	return cmd
}

func devCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "dev",
		Short: "Run the server with hot reload (air)",
		Long:  "Recompiles and restarts on every Go change.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if _, err := exec.LookPath("air"); err != nil {
				return fmt.Errorf("air is not installed — run: go install github.com/air-verse/air@latest")
			}

			air := exec.Command("air")
			air.Stdout, air.Stderr, air.Stdin = os.Stdout, os.Stderr, os.Stdin
			return air.Run()
		},
	}
}

func buildCmd() *cobra.Command {
	var output string
	var drivers []string

	cmd := &cobra.Command{
		Use:   "build",
		Short: "Build a production binary",
		Long: "Builds with CGO disabled. The SQLite driver is pure Go, so the result\n" +
			"is a static binary that cross-compiles and runs on a scratch image.\n\n" +
			"--drivers keeps only the database drivers you ship with: leaving out\n" +
			"postgres and mysql takes about 4 MB off the binary.",
		RunE: func(cmd *cobra.Command, args []string) error {
			args = []string{"build", "-trimpath", "-ldflags", "-s -w"}

			if tags := excludedDriverTags(drivers); tags != "" {
				args = append(args, "-tags", tags)
			}
			args = append(args, "-o", output, "./cmd/omega")

			build := exec.Command("go", args...)
			build.Env = append(os.Environ(), "CGO_ENABLED=0")
			build.Stdout, build.Stderr = os.Stdout, os.Stderr

			if err := build.Run(); err != nil {
				return err
			}
			fmt.Println("built", output)
			return nil
		},
	}

	cmd.Flags().StringSliceVar(&drivers, "drivers", nil,
		"database drivers to keep: sqlite, postgres, mysql (all of them by default)")
	cmd.Flags().StringVarP(&output, "output", "o", "bin/app", "output path")
	return cmd
}

func testCmd() *cobra.Command {
	var coverage bool

	cmd := &cobra.Command{
		Use:   "test",
		Short: "Run the test suite",
		RunE: func(cmd *cobra.Command, args []string) error {
			testArgs := []string{"test", "./..."}
			if coverage {
				testArgs = append(testArgs,
					"-coverprofile=coverage.out",
					"-covermode=atomic",
					"-coverpkg=./...",
				)
			}
			testArgs = append(testArgs, args...)

			test := exec.Command("go", testArgs...)
			test.Stdout, test.Stderr = os.Stdout, os.Stderr
			if err := test.Run(); err != nil {
				return err
			}

			if coverage {
				fmt.Println("\ncoverage profile written to coverage.out")
				fmt.Println("inspect it with: go tool cover -html=coverage.out")
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&coverage, "coverage", false, "collect a coverage profile")
	return cmd
}

func routesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "routes",
		Short: "List the registered routes",
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := bootWeb()
			if err != nil {
				return err
			}
			defer app.Shutdown()

			type entry struct {
				path    string
				methods []string
			}
			var order []string
			byName := map[string]*entry{}

			for _, stack := range app.Fiber.Stack() {
				for _, route := range stack {
					if route.Name == "" {
						continue
					}
					found, ok := byName[route.Name]
					if !ok {
						found = &entry{path: route.Path}
						byName[route.Name] = found
						order = append(order, route.Name)
					}
					if !slices.Contains(found.methods, route.Method) {
						found.methods = append(found.methods, route.Method)
					}
				}
			}

			fmt.Printf("%-14s %-28s %s\n", "METHOD", "PATH", "NAME")
			fmt.Println(strings.Repeat("─", 72))
			for _, name := range order {
				route := byName[name]
				fmt.Printf("%-14s %-28s %s\n", strings.Join(route.methods, "|"), route.path, name)
			}
			return nil
		},
	}
}

// The build embeds every driver; exclude the ones that were not asked for.
func excludedDriverTags(kept []string) string {
	if len(kept) == 0 {
		return ""
	}

	wanted := map[string]bool{}
	for _, name := range kept {
		wanted[strings.ToLower(strings.TrimSpace(name))] = true
	}

	tags := []string{}
	for _, driver := range []string{"sqlite", "postgres", "mysql"} {
		if !wanted[driver] {
			tags = append(tags, "no"+driver)
		}
	}
	return strings.Join(tags, ",")
}
