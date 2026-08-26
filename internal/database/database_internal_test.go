package database

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rs/zerolog"
	gormlogger "gorm.io/gorm/logger"
)

func TestNewTestDBOpensAnIsolatedConnection(t *testing.T) {
	manager, err := NewTestDB()
	if err != nil {
		t.Fatalf("NewTestDB: %v", err)
	}
	if manager.DefaultName() != "test" {
		t.Errorf("DefaultName = %q, want test", manager.DefaultName())
	}

	db, err := manager.Default()
	if err != nil || db == nil {
		t.Fatalf("Default: %v", err)
	}
	if _, err := manager.Connection("inexistante"); err == nil {
		t.Error("une connexion inconnue a ete ouverte")
	}
}

func TestParseGormLevelCoversEveryName(t *testing.T) {
	cases := map[string]gormlogger.LogLevel{
		"silent": gormlogger.Silent, "off": gormlogger.Silent, "none": gormlogger.Silent,
		"error": gormlogger.Error,
		"info":  gormlogger.Info, "debug": gormlogger.Info, "all": gormlogger.Info,
		"warn": gormlogger.Warn, "inconnu": gormlogger.Warn,
	}
	for name, want := range cases {
		if got := parseGormLevel(name); got != want {
			t.Errorf("%q -> %v, want %v", name, got, want)
		}
	}
}

func TestGormLoggerHonoursItsLevel(t *testing.T) {
	base := &gormLogger{log: zerolog.Nop(), level: gormlogger.Silent}

	quiet := base.LogMode(gormlogger.Silent)
	if quiet == base {
		t.Error("LogMode doit renvoyer une copie")
	}

	loud := base.LogMode(gormlogger.Info).(*gormLogger)
	if loud.level != gormlogger.Info {
		t.Errorf("level = %v", loud.level)
	}

	ctx := context.Background()
	for _, logger := range []*gormLogger{base, loud} {
		logger.Info(ctx, "message %s", "info")
		logger.Warn(ctx, "message %s", "warn")
		logger.Error(ctx, "message %s", "error")
		logger.Trace(ctx, time.Now(), func() (string, int64) { return "SELECT 1", 1 }, nil)
		logger.Trace(ctx, time.Now(), func() (string, int64) { return "SELECT 1", 0 }, errors.New("echec"))
	}
}
