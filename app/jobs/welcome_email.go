package jobs

import (
	"context"
	"encoding/json"

	"github.com/rs/zerolog"

	"omega/internal/mail"
)

type Welcome struct {
	Email string `json:"email"`
	Name  string `json:"name"`
}

// WelcomeEmail sends the welcome message.
//
// Delivery is at least once: if the worker dies between the SMTP answer and
// the job's completion, the email goes out again. That is acceptable for a
// welcome; it is dispatched with a unique key per address, so a duplicated
// event does not enqueue it twice.
func WelcomeEmail(mailer *mail.Mailer, log zerolog.Logger) func(context.Context, []byte) error {
	return func(ctx context.Context, payload []byte) error {
		var target Welcome
		if err := json.Unmarshal(payload, &target); err != nil {
			return err
		}

		// The mailer takes no context: check it before the point of no return,
		// so a shutdown or a timeout does not start a send it cannot stop.
		if err := ctx.Err(); err != nil {
			return err
		}

		return mailer.Send(mail.Message{
			To:      []string{target.Email},
			Subject: "Welcome to Omega",
			Text:    "Hello " + target.Name + ", your account is ready.",
		})
	}
}
