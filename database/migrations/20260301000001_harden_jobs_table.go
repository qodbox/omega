package migrations

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"

	"omega/app/models"
	"omega/internal/database"
)

func init() {
	database.Register(&gormigrate.Migration{
		ID: "20260301000001_harden_jobs_table",
		// unique_key makes a dispatch idempotent (one pending or running job
		// per key, enforced by a unique index: NULLs never collide, so jobs
		// without a key are unaffected). timeout_ms bounds an attempt.
		// idx_jobs_claim serves the worker's claim query.
		//
		// Only what is missing is added: on a database created after these
		// fields existed, the jobs migration already built them.
		Migrate: func(tx *gorm.DB) error {
			job := &models.Job{}
			for _, field := range []string{"UniqueKey", "TimeoutMs"} {
				if err := addColumn(tx, job, field); err != nil {
					return err
				}
			}
			for _, index := range []string{"idx_jobs_unique_key", "idx_jobs_claim"} {
				if tx.Migrator().HasIndex(job, index) {
					continue
				}
				if err := tx.Migrator().CreateIndex(job, index); err != nil {
					return err
				}
			}
			return nil
		},
		Rollback: func(tx *gorm.DB) error {
			job := &models.Job{}
			for _, index := range []string{"idx_jobs_claim", "idx_jobs_unique_key"} {
				if !tx.Migrator().HasIndex(job, index) {
					continue
				}
				if err := tx.Migrator().DropIndex(job, index); err != nil {
					return err
				}
			}
			if err := dropColumn(tx, job, "TimeoutMs"); err != nil {
				return err
			}
			return dropColumn(tx, job, "UniqueKey")
		},
	})
}
