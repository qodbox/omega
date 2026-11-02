package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"omega"
	"omega/internal/database"
)

func migrationCommands() []*cobra.Command {
	return []*cobra.Command{
		migrateCmd(),
		migrateRollbackCmd(),
		migrateResetCmd(),
		migrateFreshCmd(),
		migrateStatusCmd(),
		seedCmd(),
	}
}

func withDB(fn func(app *omega.App) error) error {
	app, err := bootCLI()
	if err != nil {
		return err
	}
	defer app.Shutdown()

	return fn(app)
}

// forceFlag adds the --force flag every destructive command takes. There is
// no -f shortcut on purpose: it must be typed.
func forceFlag(cmd *cobra.Command, force *bool) {
	cmd.Flags().BoolVar(force, "force", false, "run even when APP_ENV is production")
}

func guard(app *omega.App, command string, force bool) error {
	return database.GuardDestructive(app.Cfg.Env(), command, force)
}

func migrateCmd() *cobra.Command {
	var pretend bool

	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Run the pending migrations",
		Long: "Concurrent runs (two replicas deploying together) are serialised by a\n" +
			"database lock: the second waits, then finds nothing left to do.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return withDB(func(app *omega.App) error {
				if pretend {
					pending, err := database.PendingIDs(app.DB)
					if err != nil {
						return err
					}
					if len(pending) == 0 {
						fmt.Println("nothing to migrate")
						return nil
					}
					fmt.Printf("%d migration(s) would run:\n", len(pending))
					for _, id := range pending {
						fmt.Println("  ", id)
					}
					return nil
				}

				applied, err := database.Migrate(app.DB)
				if err != nil {
					return err
				}
				if applied == 0 {
					fmt.Println("nothing to migrate")
					return nil
				}
				fmt.Printf("migrated %d migration(s)\n", applied)
				return nil
			})
		},
	}

	cmd.Flags().BoolVar(&pretend, "pretend", false, "list the pending migrations without running them")
	return cmd
}

func migrateRollbackCmd() *cobra.Command {
	var (
		steps int
		force bool
	)

	cmd := &cobra.Command{
		Use:   "migrate:rollback",
		Short: "Roll back the last migration",
		RunE: func(cmd *cobra.Command, args []string) error {
			return withDB(func(app *omega.App) error {
				if err := guard(app, "migrate:rollback", force); err != nil {
					return err
				}
				for i := 0; i < steps; i++ {
					applied, err := database.Applied(app.DB)
					if err != nil {
						return err
					}
					if len(applied) == 0 {
						fmt.Println("nothing to roll back")
						return nil
					}
					if err := database.RollbackLast(app.DB); err != nil {
						return err
					}
					fmt.Println("rolled back", applied[len(applied)-1])
				}
				return nil
			})
		},
	}

	cmd.Flags().IntVar(&steps, "step", 1, "number of migrations to roll back")
	forceFlag(cmd, &force)
	return cmd
}

func migrateResetCmd() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "migrate:reset",
		Short: "Roll back every migration",
		RunE: func(cmd *cobra.Command, args []string) error {
			return withDB(func(app *omega.App) error {
				if err := guard(app, "migrate:reset", force); err != nil {
					return err
				}
				if err := database.Reset(app.DB); err != nil {
					return err
				}
				fmt.Println("all migrations rolled back")
				return nil
			})
		},
	}

	forceFlag(cmd, &force)
	return cmd
}

func migrateFreshCmd() *cobra.Command {
	var withSeed, force bool

	cmd := &cobra.Command{
		Use:   "migrate:fresh",
		Short: "Drop every table, then migrate",
		Long: "Unlike migrate:reset this does not replay the Rollback functions, so a\n" +
			"half-written migration cannot block it.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return withDB(func(app *omega.App) error {
				if err := guard(app, "migrate:fresh", force); err != nil {
					return err
				}
				if err := database.Fresh(app.DB); err != nil {
					return err
				}
				fmt.Println("database rebuilt")

				if withSeed {
					if err := database.Seed(app.DB); err != nil {
						return err
					}
					fmt.Println("seeders ran")
				}
				return nil
			})
		},
	}

	cmd.Flags().BoolVar(&withSeed, "seed", false, "run the seeders afterwards")
	forceFlag(cmd, &force)
	return cmd
}

func migrateStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "migrate:status",
		Short: "Show which migrations have run",
		RunE: func(cmd *cobra.Command, args []string) error {
			return withDB(func(app *omega.App) error {
				statuses, err := database.StatusList(app.DB)
				if err != nil {
					return err
				}
				if len(statuses) == 0 {
					fmt.Println("no migrations registered")
					return nil
				}

				for _, status := range statuses {
					mark := "pending"
					if status.Applied {
						mark = "applied"
					}
					fmt.Printf("  [%s]  %s\n", mark, status.ID)
				}
				return nil
			})
		},
	}
}

func seedCmd() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "db:seed [seeder...]",
		Short: "Run the seeders",
		RunE: func(cmd *cobra.Command, args []string) error {
			return withDB(func(app *omega.App) error {
				if err := guard(app, "db:seed", force); err != nil {
					return err
				}
				if err := database.Seed(app.DB, args...); err != nil {
					return err
				}

				if len(args) == 0 {
					fmt.Printf("ran %d seeder(s)\n", len(database.Seeders()))
				} else {
					fmt.Printf("ran %d seeder(s)\n", len(args))
				}
				return nil
			})
		},
	}

	forceFlag(cmd, &force)
	return cmd
}

// ensureMigrated is checked by serve: an API whose schema is behind its code
// fails in confusing ways (a missing column on the first request). With
// database.require_migrated (default: true in production) it refuses to
// start; otherwise it warns.
func ensureMigrated(app *omega.App) error {
	if app.DB == nil {
		return nil
	}

	pending, err := database.PendingIDs(app.DB)
	if err != nil {
		return fmt.Errorf("checking the migrations: %w", err)
	}
	if len(pending) == 0 {
		return nil
	}

	required := app.Cfg.Env() == "production"
	if raw := strings.TrimSpace(app.Cfg.String("database.require_migrated")); raw != "" {
		required = app.Cfg.Bool("database.require_migrated")
	}
	if required {
		return fmt.Errorf("%d migration(s) pending (%s): run omega migrate first, or set DB_REQUIRE_MIGRATED=false",
			len(pending), strings.Join(pending, ", "))
	}

	app.Log.Warn().Strs("pending", pending).
		Msg("migrations are pending: run omega migrate, the API may fail on the missing schema")
	return nil
}
