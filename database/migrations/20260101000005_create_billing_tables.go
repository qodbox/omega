package migrations

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"

	"omega/app/models"
	"omega/internal/database"
)

func init() {
	database.Register(&gormigrate.Migration{
		ID: "20260101000005_create_billing_tables",
		Migrate: func(tx *gorm.DB) error {
			return tx.AutoMigrate(
				&models.BillingCustomer{},
				&models.BillingSubscription{},
				&models.BillingEvent{},
			)
		},
		Rollback: func(tx *gorm.DB) error {
			return tx.Migrator().DropTable(
				"billing_events",
				"billing_subscriptions",
				"billing_customers",
			)
		},
	})
}
