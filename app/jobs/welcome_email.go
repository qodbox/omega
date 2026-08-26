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

func WelcomeEmail(mailer *mail.Mailer, log zerolog.Logger) func(context.Context, []byte) error {
	return func(ctx context.Context, payload []byte) error {
		var target Welcome
		if err := json.Unmarshal(payload, &target); err != nil {
			return err
		}

		return mailer.Send(mail.Message{
			To:      []string{target.Email},
			Subject: "Welcome to Omega",
			Text:    "Hello " + target.Name + ", your account is ready.",
		})
	}
}
