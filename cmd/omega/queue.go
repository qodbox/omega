package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"omega/internal/queue"
)

func queueCommands() []*cobra.Command {
	return []*cobra.Command{
		queueWorkCommand(),
		queueFailedCommand(),
		queueRetryCommand(),
		queueFlushCommand(),
	}
}

func queueWorkCommand() *cobra.Command {
	var (
		queues      string
		concurrency int
		poll        time.Duration
	)

	cmd := &cobra.Command{
		Use:   "queue:work",
		Short: "Process queued jobs until interrupted",
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := bootWorker()
			if err != nil {
				return err
			}
			defer app.Shutdown()

			names := strings.Split(queues, ",")
			for index := range names {
				names[index] = strings.TrimSpace(names[index])
			}

			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			app.Log.Info().
				Strs("queues", names).
				Int("concurrency", concurrency).
				Int("handlers", len(app.Queue.Names())).
				Msg("queue: worker started")

			return app.Queue.Work(ctx, queue.WorkerOptions{
				Queues:      names,
				Concurrency: concurrency,
				Poll:        poll,
				StuckAfter:  app.Cfg.Duration("queue.stuck_after"),
				Log:         app.Log,
			})
		},
	}

	cmd.Flags().StringVar(&queues, "queue", "default", "comma separated queues to consume")
	cmd.Flags().IntVar(&concurrency, "concurrency", 4, "jobs processed in parallel")
	cmd.Flags().DurationVar(&poll, "poll", time.Second, "how long to wait when the queue is empty")
	return cmd
}

func queueFailedCommand() *cobra.Command {
	var limit int

	cmd := &cobra.Command{
		Use:   "queue:failed",
		Short: "List the jobs that exhausted their attempts",
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := bootWorker()
			if err != nil {
				return err
			}
			defer app.Shutdown()

			entries, err := app.Queue.Store().Failed(cmd.Context(), limit)
			if err != nil {
				return err
			}
			if len(entries) == 0 {
				fmt.Println("no failed job")
				return nil
			}

			fmt.Printf("%-6s %-12s %-24s %-9s %s\n", "ID", "QUEUE", "JOB", "ATTEMPTS", "ERROR")
			fmt.Println(strings.Repeat("─", 96))
			for _, entry := range entries {
				failure := entry.LastError
				if len(failure) > 44 {
					failure = failure[:43] + "…"
				}
				fmt.Printf("%-6d %-12s %-24s %-9s %s\n",
					entry.ID, entry.Queue, entry.Name,
					fmt.Sprintf("%d/%d", entry.Attempts, entry.MaxTries), failure)
			}
			return nil
		},
	}

	cmd.Flags().IntVar(&limit, "limit", 50, "how many to show")
	return cmd
}

func queueRetryCommand() *cobra.Command {
	var id uint

	cmd := &cobra.Command{
		Use:   "queue:retry",
		Short: "Push failed jobs back onto the queue",
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := bootWorker()
			if err != nil {
				return err
			}
			defer app.Shutdown()

			count, err := app.Queue.Store().Retry(cmd.Context(), id)
			if err != nil {
				return err
			}
			fmt.Printf("queued %d job(s) again\n", count)
			return nil
		},
	}

	cmd.Flags().UintVar(&id, "id", 0, "a single job, or every failed one when omitted")
	return cmd
}

func queueFlushCommand() *cobra.Command {
	var status string

	cmd := &cobra.Command{
		Use:   "queue:flush",
		Short: "Delete jobs from the table",
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := bootWorker()
			if err != nil {
				return err
			}
			defer app.Shutdown()

			count, err := app.Queue.Store().Purge(cmd.Context(), status)
			if err != nil {
				return err
			}
			fmt.Printf("deleted %d job(s)\n", count)
			return nil
		},
	}

	cmd.Flags().StringVar(&status, "status", "failed", "pending, running, failed, or empty for all")
	return cmd
}

func scheduleCommands() []*cobra.Command {
	run := &cobra.Command{
		Use:   "schedule:run",
		Short: "Run the scheduled tasks until interrupted",
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := bootWorker()
			if err != nil {
				return err
			}
			defer app.Shutdown()

			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			app.Log.Info().Int("tasks", len(app.Scheduler.Tasks())).Msg("scheduler: started")
			return app.Scheduler.Run(ctx)
		},
	}

	list := &cobra.Command{
		Use:   "schedule:list",
		Short: "Show the scheduled tasks and when they run next",
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := bootWorker()
			if err != nil {
				return err
			}
			defer app.Shutdown()

			tasks := app.Scheduler.Tasks()
			if len(tasks) == 0 {
				fmt.Println("no scheduled task")
				return nil
			}
			for _, task := range tasks {
				fmt.Println(" ", task)
			}
			return nil
		},
	}

	return []*cobra.Command{run, list}
}
