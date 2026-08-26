//go:build !nosqlite

package database

import (
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func init() {
	registerDialector(func(conn Connection) gorm.Dialector {
		return sqlite.Open(sqliteDSN(conn))
	}, "sqlite", "sqlite3")
}
