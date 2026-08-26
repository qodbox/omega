package jobs

import (
	"context"
	"time"

	"github.com/rs/zerolog"
	"gorm.io/gorm"

	"omega/app/models"
)

func PurgeTokens(db *gorm.DB, log zerolog.Logger) func(context.Context, []byte) error {
	return func(ctx context.Context, payload []byte) error {
		result := db.WithContext(ctx).
			Where("expires_at <= ?", time.Now()).
			Delete(&models.RefreshToken{})

		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected > 0 {
			log.Info().Int64("tokens", result.RowsAffected).Msg("purged expired refresh tokens")
		}
		return nil
	}
}
