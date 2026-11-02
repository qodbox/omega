package migrations

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"

	"omega/app/models"
	"omega/internal/database"
)

// Refresh token families (reuse detection) and the per-account token version
// (immediate access token revocation). The create-table migrations build from
// the current models, so on a fresh database the columns already exist: each
// one is only added when missing.
func init() {
	database.Register(&gormigrate.Migration{
		ID: "20260201000001_add_token_family_and_version",
		Migrate: func(tx *gorm.DB) error {
			migrator := tx.Migrator()

			for _, column := range []string{"Family", "UsedAt"} {
				if !migrator.HasColumn(&models.RefreshToken{}, column) {
					if err := migrator.AddColumn(&models.RefreshToken{}, column); err != nil {
						return err
					}
				}
			}
			if !migrator.HasIndex(&models.RefreshToken{}, "Family") {
				if err := migrator.CreateIndex(&models.RefreshToken{}, "Family"); err != nil {
					return err
				}
			}

			// A token issued before families existed is its own family.
			if err := tx.Table("refresh_tokens").
				Where("family IS NULL OR family = ''").
				Update("family", gorm.Expr("id")).Error; err != nil {
				return err
			}

			if !migrator.HasColumn(&models.User{}, "TokenVersion") {
				return migrator.AddColumn(&models.User{}, "TokenVersion")
			}
			return nil
		},
		Rollback: func(tx *gorm.DB) error {
			migrator := tx.Migrator()
			if migrator.HasIndex(&models.RefreshToken{}, "Family") {
				if err := migrator.DropIndex(&models.RefreshToken{}, "Family"); err != nil {
					return err
				}
			}
			for _, column := range []string{"Family", "UsedAt"} {
				if migrator.HasColumn(&models.RefreshToken{}, column) {
					if err := migrator.DropColumn(&models.RefreshToken{}, column); err != nil {
						return err
					}
				}
			}
			if migrator.HasColumn(&models.User{}, "TokenVersion") {
				return migrator.DropColumn(&models.User{}, "TokenVersion")
			}
			return nil
		},
	})
}
