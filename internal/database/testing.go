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
