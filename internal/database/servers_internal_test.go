package database

// Engine helpers for the tests that run on a real PostgreSQL or MySQL server.
// They live in a test file so that they never ship in the binary.

import (
	"io"
	"strings"

	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

// openPostgresSchema opens dsn on a fresh schema of its own, so test packages
// running in parallel against one server never see each other's tables. drop
// removes the schema and closes the connections.
func openPostgresSchema(dsn, schema string) (db *gorm.DB, drop func(), err error) {
	admin, err := Open(Connection{Driver: "postgres", DSN: dsn, MaxOpenConns: 2}, zerolog.New(io.Discard), "silent", 0)
	if err != nil {
		return nil, nil, err
	}
	closeAdmin := func() {
		if pool, err := admin.DB(); err == nil {
			_ = pool.Close()
		}
	}

	quoted := `"` + strings.ReplaceAll(schema, `"`, `""`) + `"`
	if err := admin.Exec("DROP SCHEMA IF EXISTS " + quoted + " CASCADE").Error; err != nil {
		closeAdmin()
		return nil, nil, err
	}
	if err := admin.Exec("CREATE SCHEMA " + quoted).Error; err != nil {
		closeAdmin()
		return nil, nil, err
	}

	scoped := dsn
	if strings.Contains(dsn, "://") {
		separator := "?"
		if strings.Contains(dsn, "?") {
			separator = "&"
		}
		scoped += separator + "search_path=" + schema
	} else {
		scoped += " search_path=" + schema
	}

	db, err = Open(Connection{Driver: "postgres", DSN: scoped, MaxOpenConns: 20}, zerolog.New(io.Discard), "silent", 0)
	if err != nil {
		closeAdmin()
		return nil, nil, err
	}

	drop = func() {
		if pool, err := db.DB(); err == nil {
			_ = pool.Close()
		}
		_ = admin.Exec("DROP SCHEMA IF EXISTS " + quoted + " CASCADE").Error
		closeAdmin()
	}
	return db, drop, nil
}

// openMySQLDatabase creates a database of its own on the server dsn points at
// and opens it. drop removes it.
func openMySQLDatabase(dsn, name string) (db *gorm.DB, drop func(), err error) {
	base, _, _ := strings.Cut(dsn, "/")
	admin, err := Open(Connection{Driver: "mysql", DSN: base + "/?parseTime=true", MaxOpenConns: 2}, zerolog.New(io.Discard), "silent", 0)
	if err != nil {
		return nil, nil, err
	}
	closeAdmin := func() {
		if pool, err := admin.DB(); err == nil {
			_ = pool.Close()
		}
	}

	quoted := "`" + strings.ReplaceAll(name, "`", "") + "`"
	if err := admin.Exec("DROP DATABASE IF EXISTS " + quoted).Error; err != nil {
		closeAdmin()
		return nil, nil, err
	}
	if err := admin.Exec("CREATE DATABASE " + quoted).Error; err != nil {
		closeAdmin()
		return nil, nil, err
	}

	db, err = Open(Connection{Driver: "mysql", DSN: base + "/" + name + "?parseTime=true&loc=UTC", MaxOpenConns: 20},
		zerolog.New(io.Discard), "silent", 0)
	if err != nil {
		closeAdmin()
		return nil, nil, err
	}
	drop = func() {
		if pool, err := db.DB(); err == nil {
			_ = pool.Close()
		}
		_ = admin.Exec("DROP DATABASE IF EXISTS " + quoted).Error
		closeAdmin()
	}
	return db, drop, nil
}
