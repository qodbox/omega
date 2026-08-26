//go:build !nomysql

package database

import (
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

func init() {
	registerDialector(func(conn Connection) gorm.Dialector {
		return mysql.Open(mysqlDSN(conn))
	}, "mysql", "mariadb")
}
