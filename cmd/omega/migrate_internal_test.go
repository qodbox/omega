package main

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"omega/internal/database"
	"omega/internal/queue"
)

func run(t *testing.T, cmd *cobra.Command, args ...string) (string, error) {
	t.Helper()
	cmd.SetArgs(args)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	var err error
	out := captureStdout(t, func() { err = cmd.Execute() })
	return out, err
}

func TestDestructiveCommandsRefuseProductionWithoutForce(t *testing.T) {
	insideProject(t)
	t.Setenv("APP_ENV", "production")

	for _, build := range []func() *cobra.Command{migrateFreshCmd, migrateResetCmd, migrateRollbackCmd, seedCmd} {
		cmd := build()
		name := strings.Fields(cmd.Use)[0]

		force := cmd.Flags().Lookup("force")
		if force == nil || force.Shorthand != "" {
			t.Errorf("%s: want a --force flag without shorthand", name)
			continue
		}
		if _, err := run(t, build()); !errors.Is(err, database.ErrProduction) {
			t.Errorf("%s in production = %v, want ErrProduction", name, err)
		}
	}

	// --force goes through.
	if out, err := run(t, migrateFreshCmd(), "--force"); err != nil || !strings.Contains(out, "database rebuilt") {
		t.Errorf("migrate:fresh --force = %q, %v", out, err)
	}
	if out, err := run(t, migrateRollbackCmd(), "--force"); err != nil || !strings.Contains(out, "rolled back") {
		t.Errorf("migrate:rollback --force = %q, %v", out, err)
	}
}

func TestMigratePretendListsWithoutApplying(t *testing.T) {
	insideProject(t)
	t.Setenv("APP_ENV", "testing")

	out, err := run(t, migrateCmd(), "--pretend")
	if err != nil || !strings.Contains(out, "would run") || !strings.Contains(out, "20260301000002_create_schedule_locks_table") {
		t.Fatalf("--pretend = %q, %v", out, err)
	}
	if out, err := run(t, migrateCmd(), "--pretend"); err != nil || !strings.Contains(out, "would run") {
		t.Fatalf("pretend applied something: %q, %v", out, err)
	}

	if out, err := run(t, migrateCmd()); err != nil || !strings.Contains(out, "migrated") {
		t.Fatalf("migrate = %q, %v", out, err)
	}
	for _, args := range [][]string{{"--pretend"}, {}} {
		if out, err := run(t, migrateCmd(), args...); err != nil || !strings.Contains(out, "nothing to migrate") {
			t.Errorf("migrate %v after migrating = %q, %v", args, out, err)
		}
	}
}

func TestServeRefusesPendingMigrationsInProduction(t *testing.T) {
	insideProject(t)

	for _, tc := range []struct {
		env, require string
		refuse       bool
	}{
		{"production", "", true},
		{"production", "false", false},
		{"local", "", false},
		{"local", "true", true},
	} {
		t.Setenv("APP_ENV", tc.env)
		t.Setenv("DB_REQUIRE_MIGRATED", tc.require)

		app, err := bootCLI()
		if err != nil {
			t.Fatal(err)
		}
		err = ensureMigrated(app)
		_ = app.Shutdown()
		if (err != nil) != tc.refuse {
			t.Errorf("env %s require %q: ensureMigrated = %v, want refusal %v", tc.env, tc.require, err, tc.refuse)
		}
	}

	t.Setenv("APP_ENV", "production")
	t.Setenv("DB_REQUIRE_MIGRATED", "")
	if _, err := run(t, migrateCmd()); err != nil {
		t.Fatal(err)
	}
	app, err := bootCLI()
	if err != nil {
		t.Fatal(err)
	}
	defer app.Shutdown()
	if err := ensureMigrated(app); err != nil {
		t.Errorf("a migrated database was refused: %v", err)
	}
}

func TestTheWorkerServesItsMetrics(t *testing.T) {
	insideProject(t)
	t.Setenv("APP_ENV", "testing")

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := listener.Addr().String()
	_ = listener.Close()

	app, err := bootWorker()
	if err != nil {
		t.Fatal(err)
	}
	defer app.Shutdown()
	if _, err := database.Migrate(app.DB); err != nil {
		t.Fatal(err)
	}

	stop := serveMetrics(addr, app)
	defer stop()

	var body []byte
	for range 50 {
		response, err := http.Get("http://" + addr + "/metrics")
		if err == nil {
			body, _ = io.ReadAll(response.Body)
			_ = response.Body.Close()
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	for _, want := range []string{"omega_uptime_seconds", "omega_queue_jobs_processed_total 0", `omega_queue_jobs{status="pending"} 0`} {
		if !strings.Contains(string(body), want) {
			t.Errorf("metrics miss %q:\n%s", want, body)
		}
	}
}

func runFor(t *testing.T, cmd *cobra.Command, d time.Duration, args ...string) (string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()
	cmd.SetArgs(args)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	var err error
	out := captureStdout(t, func() { err = cmd.ExecuteContext(ctx) })
	return out, err
}

func TestTheMaintenanceCommandsRunAgainstADatabase(t *testing.T) {
	insideProject(t)
	t.Setenv("APP_ENV", "testing")
	t.Setenv("CURRENCY_ENDPOINT", "http://127.0.0.1:1/latest")

	steps := []struct {
		cmd  func() *cobra.Command
		args []string
		want string
	}{
		{migrateStatusCmd, nil, "[pending]"},
		{migrateCmd, nil, "migrated"},
		{migrateStatusCmd, nil, "[applied]"},
		{seedCmd, nil, "seeder(s)"},
		{queueFailedCommand, nil, "no failed job"},
		{queueRetryCommand, nil, "queued 0 job(s)"},
		{queueFlushCommand, []string{"--status", ""}, "deleted 0 job(s)"},
		{func() *cobra.Command { return scheduleCommands()[1] }, nil, "purge-expired-tokens"},
		{migrateResetCmd, nil, "rolled back"},
		{func() *cobra.Command { return migrateFreshCmd() }, []string{"--seed"}, "seeders ran"},
		{migrateRollbackCmd, []string{"--step", "50"}, "nothing to roll back"},
	}
	for _, step := range steps {
		cmd := step.cmd()
		out, err := run(t, cmd, step.args...)
		if err != nil || !strings.Contains(out, step.want) {
			t.Errorf("%s %v = %q, %v; want %q", cmd.Use, step.args, out, err, step.want)
		}
	}
}

func TestWorkerAndSchedulerStopWithTheirContext(t *testing.T) {
	insideProject(t)
	t.Setenv("APP_ENV", "testing")
	if _, err := run(t, migrateCmd()); err != nil {
		t.Fatal(err)
	}

	if _, err := runFor(t, queueWorkCommand(), 300*time.Millisecond, "--queue", "default, mails", "--poll", "10ms"); err != nil {
		t.Errorf("queue:work = %v", err)
	}
	if _, err := runFor(t, scheduleCommands()[0], 300*time.Millisecond); err != nil {
		t.Errorf("schedule:run = %v", err)
	}

	t.Setenv("QUEUE_TIMEOUT", "10m")
	if _, err := runFor(t, queueWorkCommand(), time.Second); !errors.Is(err, queue.ErrOptions) {
		t.Errorf("a timeout above stuck_after = %v, want ErrOptions", err)
	}
}
