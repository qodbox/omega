package migrations

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"

	"omega/app/models"
	"omega/internal/database"
)

func init() {
	database.Register(&gormigrate.Migration{
		ID: "20260101000004_create_login_attempts_table",
		Migrate: func(tx *gorm.DB) error {
			return tx.AutoMigrate(&models.LoginAttempt{})
		},
		Rollback: func(tx *gorm.DB) error {
			return tx.Migrator().DropTable("login_attempts")
		},
	})
}
