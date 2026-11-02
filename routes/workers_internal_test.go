package routes

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"omega"
	"omega/app/models"
	"omega/internal/database"
	"omega/internal/queue"

	_ "omega/database/migrations"
)

func bootWorkers(t *testing.T) *omega.App {
	t.Helper()
	t.Setenv("DB_CONNECTION", "sqlite")
	t.Setenv("DB_SQLITE_PATH", filepath.Join(t.TempDir(), "workers.db"))
	t.Setenv("AUTH_SECRET", "a-workers-test-secret-long-enough-to-sign")
	t.Setenv("APP_ENV", "testing")
	t.Setenv("LOG_LEVEL", "error")
	// Nothing listens there: the rate refresh fails at once, as it may in
	// production, and the task must shrug it off.
	t.Setenv("CURRENCY_ENDPOINT", "http://127.0.0.1:1/latest")

	app, err := omega.Boot(omega.WithoutHTTP())
	if err != nil {
		t.Fatalf("boot: %v", err)
	}
	t.Cleanup(func() { _ = app.Shutdown() })
	if err := database.Up(app.DB); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := RegisterWorkers(app); err != nil {
		t.Fatalf("workers: %v", err)
	}
	return app
}

func TestEveryScheduledTaskRuns(t *testing.T) {
	app := bootWorkers(t)
	ctx := context.Background()

	names := app.Scheduler.Names()
	if len(names) < 5 {
		t.Fatalf("tasks = %v", names)
	}
	for _, name := range names {
		if err := app.Scheduler.RunTask(ctx, name); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}

	// The token purge is dispatched once however often the task fires.
	if err := app.Scheduler.RunTask(ctx, "purge-expired-tokens"); err != nil {
		t.Fatal(err)
	}
	var purges int64
	app.DB.Model(&models.Job{}).Where("name = ?", "purge_tokens").Count(&purges)
	if purges != 1 {
		t.Errorf("purge_tokens queued %d times, want 1", purges)
	}

	var metrics bytes.Buffer
	app.Queue.WriteMetrics(&metrics)
	if !strings.Contains(app.Metrics.Render(), `omega_queue_jobs{status="pending"} 1`) {
		t.Errorf("the queue depth is missing from /metrics")
	}
}

func TestARegistrationEnqueuesOneWelcomePerAddress(t *testing.T) {
	app := bootWorkers(t)
	ctx := context.Background()

	for _, email := range []string{"Ada@Example.test", "ada@example.test"} {
		if err := app.Queue.Push(ctx, "welcome_email", map[string]string{"email": email, "name": "Ada"},
			queue.Options{UniqueKey: welcomeKey(map[string]string{"email": email})}); err != nil {
			t.Fatal(err)
		}
	}
	var welcomes int64
	app.DB.Model(&models.Job{}).Where("name = ?", "welcome_email").Count(&welcomes)
	if welcomes != 1 {
		t.Errorf("welcome emails queued = %d, want 1", welcomes)
	}

	if err := app.Events.Emit(ctx, "user.registered", map[string]any{"email": "grace@example.test", "name": "Grace"}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		app.DB.Model(&models.Job{}).Where("unique_key = ?", "welcome_email:grace@example.test").Count(&welcomes)
		if welcomes == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if welcomes != 1 {
		t.Error("the registration did not enqueue its welcome email")
	}

	for _, payload := range []any{nil, map[string]string{"name": "no email"}, make(chan int)} {
		if key := welcomeKey(payload); key != "" {
			t.Errorf("welcomeKey(%v) = %q, want none", payload, key)
		}
	}
}

func TestTheJobsDoTheirWork(t *testing.T) {
	app := bootWorkers(t)
	ctx := context.Background()

	expired := time.Now().Add(-time.Hour)
	if err := app.DB.Create(&models.RefreshToken{ID: "expired", Subject: "1", ExpiresAt: expired}).Error; err != nil {
		t.Fatal(err)
	}

	stop, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() {
		done <- app.Queue.Work(stop, queue.WorkerOptions{Poll: 10 * time.Millisecond})
	}()

	_ = app.Queue.Push(ctx, "purge_tokens", map[string]any{})
	_ = app.Queue.Push(ctx, "welcome_email", map[string]string{"email": "ada@example.test", "name": "Ada"})
	_ = app.Queue.Push(ctx, "welcome_email", "not an object", queue.Options{MaxTries: 1})

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) && app.Queue.Stats().Processed+app.Queue.Stats().Failed < 3 {
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}

	stats := app.Queue.Stats()
	if stats.Processed != 2 || stats.Failed != 1 {
		t.Errorf("stats = %+v, want 2 processed and 1 failed", stats)
	}
	var tokens int64
	app.DB.Table("refresh_tokens").Count(&tokens)
	if tokens != 0 {
		t.Errorf("%d expired token(s) left", tokens)
	}
}
