package main

import (
	"fmt"

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

func migrateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "migrate",
		Short: "Run the pending migrations",
		RunE: func(cmd *cobra.Command, args []string) error {
			return withDB(func(app *omega.App) error {
				pending, err := database.Pending(app.DB)
				if err != nil {
					return err
				}
				if pending == 0 {
					fmt.Println("nothing to migrate")
					return nil
				}

				if err := database.Up(app.DB); err != nil {
					return err
				}
				fmt.Printf("migrated %d migration(s)\n", pending)
				return nil
			})
		},
	}
}

func migrateRollbackCmd() *cobra.Command {
	var steps int

	cmd := &cobra.Command{
		Use:   "migrate:rollback",
		Short: "Roll back the last migration",
		RunE: func(cmd *cobra.Command, args []string) error {
			return withDB(func(app *omega.App) error {
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
	return cmd
}

func migrateResetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "migrate:reset",
		Short: "Roll back every migration",
		RunE: func(cmd *cobra.Command, args []string) error {
			return withDB(func(app *omega.App) error {
				if err := database.Reset(app.DB); err != nil {
					return err
				}
				fmt.Println("all migrations rolled back")
				return nil
			})
		},
	}
}

func migrateFreshCmd() *cobra.Command {
	var withSeed bool

	cmd := &cobra.Command{
		Use:   "migrate:fresh",
		Short: "Drop every table, then migrate",
		Long: "Unlike migrate:reset this does not replay the Rollback functions, so a\n" +
			"half-written migration cannot block it.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return withDB(func(app *omega.App) error {
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
	return &cobra.Command{
		Use:   "db:seed [seeder...]",
		Short: "Run the seeders",
		RunE: func(cmd *cobra.Command, args []string) error {
			return withDB(func(app *omega.App) error {
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
}
