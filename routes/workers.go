package routes

import (
	"context"
	"encoding/json"
	"time"

	"omega"
	"omega/app/jobs"
	"omega/app/listeners"
	jwt "omega/internal/auth"
	"omega/internal/queue"
)

func RegisterWorkers(app *omega.App) error {
	if app.Queue != nil {
		app.Queue.Handle("welcome_email", jobs.WelcomeEmail(app.Mail, app.Log))
		app.Queue.Handle("purge_tokens", jobs.PurgeTokens(app.DB, app.Log))
	}

	app.Events.Listen("user.registered", "log", listeners.LogRegistration(app.Log))
	app.Events.ListenAsync("user.registered", "welcome", func(ctx context.Context, payload any) error {
		return app.Queue.Push(ctx, "welcome_email", payload)
	})

	app.Scheduler.
		Every("purge-expired-tokens", time.Hour, func(ctx context.Context) error {
			return app.Queue.Push(ctx, "purge_tokens", map[string]any{})
		}).
		Every("purge-login-attempts", time.Hour, func(ctx context.Context) error {
			window := app.Cfg.Duration("app.ratelimit.account.window")
			if window <= 0 {
				window = 15 * time.Minute
			}

			removed, err := jwt.NewAttempts(app.DB, 1, window).Purge(ctx, time.Now().Add(-window))
			if err != nil {
				return err
			}
			if removed > 0 {
				app.Log.Info().Int64("attempts", removed).Msg("purged expired login attempts")
			}
			return nil
		}).
		Every("sweep-old-jobs", 6*time.Hour, func(ctx context.Context) error {
			removed, err := app.Queue.Store().Sweep(ctx, "failed", time.Now().AddDate(0, 0, -7))
			if err != nil {
				return err
			}
			if removed > 0 {
				app.Log.Info().Int64("jobs", removed).Msg("swept failed jobs older than a week")
			}
			return nil
		}).
		DailyAt("report", "03:00", func(ctx context.Context) error {
			pending, err := app.Queue.Store().Pending(ctx)
			if err != nil {
				return err
			}
			app.Log.Info().Int64("pending_jobs", pending).Msg("nightly report")
			return nil
		})

	return nil
}

func decode[T any](payload []byte) (T, error) {
	var into T
	err := json.Unmarshal(payload, &into)
	return into, err
}

var _ = queue.Options{}
