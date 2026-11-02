package database

import (
	"fmt"
	"io"

	"github.com/rs/zerolog"
)

const TestDSN = "file::memory:?cache=shared&_pragma=foreign_keys(1)"

func NewTestDB() (*Manager, error) {
	manager := NewManager(Config{
		Default:  "test",
		LogLevel: "silent",
		Connections: map[string]Connection{
			"test": {Driver: "sqlite", DSN: TestDSN, MaxOpenConns: 1},
		},
	}, zerolog.New(io.Discard))

	db, err := manager.Default()
	if err != nil {
		return nil, err
	}
	if err := Up(db); err != nil {
		return nil, fmt.Errorf("test database: migrate: %w", err)
	}
	return manager, nil
}

// PostgresTestEnv names the variable holding a PostgreSQL DSN for the tests
// that need the real engine (row locks, SKIP LOCKED, advisory locks). Without
// it those tests skip.
const PostgresTestEnv = "OMEGA_TEST_POSTGRES_DSN"

// MySQLTestEnv names the variable holding a MySQL DSN (user:pass@tcp(host)/)
// for the tests of the MySQL paths. Without it those tests skip.
const MySQLTestEnv = "OMEGA_TEST_MYSQL_DSN"
