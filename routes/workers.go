package routes

import (
	"context"
	"encoding/json"
	"strings"
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
		if app.Metrics != nil {
			app.Metrics.Collect(app.Queue.WriteMetrics)
		}
	}

	app.Events.Listen("user.registered", "log", listeners.LogRegistration(app.Log))
	app.Events.ListenAsync("user.registered", "welcome", func(ctx context.Context, payload any) error {
		return app.Queue.Push(ctx, "welcome_email", payload, queue.Options{
			UniqueKey: welcomeKey(payload),
		})
	})

	app.Scheduler.
		Every("purge-expired-tokens", time.Hour, func(ctx context.Context) error {
			// One purge in the queue is enough, however late the workers are.
			return app.Queue.Push(ctx, "purge_tokens", map[string]any{},
				queue.Options{UniqueKey: "purge_tokens"})
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
		Every("refresh-exchange-rates", 6*time.Hour, func(ctx context.Context) error {
			// Rates are fetched on demand and cached; refreshing on a schedule
			// is what keeps a visitor from ever paying for the remote call.
			if err := app.Currency.Refresh(ctx); err != nil {
				// A stale rate is not an incident: the last table still serves.
				app.Log.Warn().Err(err).Msg("could not refresh the exchange rates")
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

// welcomeKey dedupes the welcome email per address: an event delivered twice
// enqueues one email.
func welcomeKey(payload any) string {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	target, err := decode[jobs.Welcome](encoded)
	if err != nil || target.Email == "" {
		return ""
	}
	return "welcome_email:" + strings.ToLower(target.Email)
}
