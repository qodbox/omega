package migrations

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"

	"omega/app/models"
	"omega/internal/database"
)

func init() {
	database.Register(&gormigrate.Migration{
		ID: "20260101000006_add_currency_columns",
		// A user reads in the currency they chose; a subscription records the
		// one Stripe actually billed in. Existing rows take the base currency,
		// which is what they were already being read as.
		//
		// The columns are added only where they are missing: the earlier
		// migrations AutoMigrate the *current* model, so on a database created
		// after this field existed they are already there, and adding them
		// again is an error rather than a no-op.
		Migrate: func(tx *gorm.DB) error {
			if err := addColumn(tx, &models.User{}, "Currency"); err != nil {
				return err
			}
			return addColumn(tx, &models.BillingSubscription{}, "Currency")
		},
		Rollback: func(tx *gorm.DB) error {
			if err := dropColumn(tx, &models.BillingSubscription{}, "Currency"); err != nil {
				return err
			}
			return dropColumn(tx, &models.User{}, "Currency")
		},
	})
}

func addColumn(tx *gorm.DB, model any, field string) error {
	if tx.Migrator().HasColumn(model, field) {
		return nil
	}
	return tx.Migrator().AddColumn(model, field)
}

func dropColumn(tx *gorm.DB, model any, field string) error {
	if !tx.Migrator().HasColumn(model, field) {
		return nil
	}
	return tx.Migrator().DropColumn(model, field)
}
