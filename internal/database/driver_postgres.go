//go:build !nopostgres

package database

import (
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func init() {
	registerDialector(func(conn Connection) gorm.Dialector {
		return postgres.Open(postgresDSN(conn))
	}, "postgres", "postgresql", "pgsql")
}
