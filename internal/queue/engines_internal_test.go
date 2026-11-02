package queue

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"gorm.io/gorm"

	"omega/app/models"
	"omega/internal/database"
)

// engine opens a fresh jobs table on one database engine.
type engine struct {
	name string
	open func(t *testing.T) *gorm.DB
}

// engines are SQLite (always), PostgreSQL when OMEGA_TEST_POSTGRES_DSN is set
// and MySQL when OMEGA_TEST_MYSQL_DSN is: the claim path differs between them.
func engines() []engine {
	list := []engine{{name: "sqlite", open: sqliteFile}}
	if os.Getenv(database.PostgresTestEnv) != "" {
		list = append(list, engine{name: "postgres", open: postgresSchema})
	}
	if os.Getenv(database.MySQLTestEnv) != "" {
		list = append(list, engine{name: "mysql", open: mysqlDatabase})
	}
	return list
}

func eachEngine(t *testing.T, test func(t *testing.T, db *gorm.DB)) {
	for _, engine := range engines() {
		t.Run(engine.name, func(t *testing.T) { test(t, engine.open(t)) })
	}
}

func sqliteFile(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := database.Open(database.Connection{
		Driver: "sqlite",
		DSN:    filepath.Join(t.TempDir(), "jobs.db"),
	}, zerolog.New(io.Discard), "silent", 0)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() {
		if pool, err := db.DB(); err == nil {
			_ = pool.Close()
		}
	})
	migrateJobs(t, db)
	return db
}

func postgresSchema(t *testing.T) *gorm.DB {
	t.Helper()
	schema := fmt.Sprintf("queue_%s_%d", strings.ToLower(strings.NewReplacer("/", "_", " ", "_", "-", "_").Replace(t.Name())), time.Now().UnixNano()%1e6)
	if len(schema) > 60 {
		schema = schema[len(schema)-60:]
		schema = "q" + schema
	}
	db, drop, err := openPostgresSchema(os.Getenv(database.PostgresTestEnv), schema)
	if err != nil {
		t.Fatalf("postgres: %v", err)
	}
	t.Cleanup(drop)
	migrateJobs(t, db)
	return db
}

func mysqlDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	db, drop, err := openMySQLDatabase(os.Getenv(database.MySQLTestEnv), fmt.Sprintf("queue_test_%d", time.Now().UnixNano()))
	if err != nil {
		t.Fatalf("mysql: %v", err)
	}
	t.Cleanup(drop)
	migrateJobs(t, db)
	return db
}

func migrateJobs(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := db.AutoMigrate(&models.Job{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
}
