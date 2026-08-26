package listeners

import (
	"context"

	"github.com/rs/zerolog"
)

func LogRegistration(log zerolog.Logger) func(context.Context, any) error {
	return func(ctx context.Context, payload any) error {
		log.Info().Any("user", payload).Msg("a new account was created")
		return nil
	}
}
