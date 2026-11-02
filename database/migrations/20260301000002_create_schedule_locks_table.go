package migrations

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"

	"omega/app/models"
	"omega/internal/database"
)

func init() {
	database.Register(&gormigrate.Migration{
		ID: "20260301000002_create_schedule_locks_table",
		// One row per scheduled task: the replica that moves its slot forward
		// is the one that runs it.
		Migrate: func(tx *gorm.DB) error {
			return tx.AutoMigrate(&models.ScheduleLock{})
		},
		Rollback: func(tx *gorm.DB) error {
			return tx.Migrator().DropTable("schedule_locks")
		},
	})
}
