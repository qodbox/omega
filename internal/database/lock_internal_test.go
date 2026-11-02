package database

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-gormigrate/gormigrate/v2"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

type widget struct {
	ID   uint
	Name string
}

type gadget struct {
	ID uint
}

var registerWidgets sync.Once

// widgetsMigration is registered once for the whole test binary: the
// registry is global, as it is in an application.
func widgetsMigration() {
	registerWidgets.Do(func() {
		Register(&gormigrate.Migration{
			ID:       "29990101000001_create_widgets",
			Migrate:  func(tx *gorm.DB) error { return tx.AutoMigrate(&widget{}) },
			Rollback: func(tx *gorm.DB) error { return tx.Migrator().DropTable(&widget{}) },
		})
	})
}

func sqliteAt(t *testing.T, path string) *gorm.DB {
	t.Helper()
	db, err := Open(Connection{Driver: "sqlite", DSN: path}, zerolog.New(io.Discard), "silent", 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if pool, err := db.DB(); err == nil {
			_ = pool.Close()
		}
	})
	return db
}

func postgresOrSkip(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv(PostgresTestEnv)
	if dsn == "" {
		t.Skipf("%s is not set", PostgresTestEnv)
	}
	db, drop, err := openPostgresSchema(dsn, fmt.Sprintf("dbtest_%d", time.Now().UnixNano()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(drop)
	return db
}

func TestGuardDestructiveOnlyStopsProductionWithoutForce(t *testing.T) {
	if err := GuardDestructive(" Production ", "migrate:fresh", false); !errors.Is(err, ErrProduction) {
		t.Errorf("production = %v, want ErrProduction", err)
	}
	for _, env := range []string{"local", "testing", "staging", ""} {
		if err := GuardDestructive(env, "db:seed", false); err != nil {
			t.Errorf("%q = %v", env, err)
		}
	}
	if err := GuardDestructive("production", "db:seed", true); err != nil {
		t.Errorf("forced = %v", err)
	}
}

func TestMigrateReportsWhatItAppliedAndCleansItsLock(t *testing.T) {
	widgetsMigration()
	path := filepath.Join(t.TempDir(), "app.db")
	db := sqliteAt(t, path)

	pending, err := PendingIDs(db)
	if err != nil || len(pending) == 0 {
		t.Fatalf("PendingIDs = %v, %v", pending, err)
	}

	applied, err := Migrate(db)
	if err != nil || applied != len(pending) {
		t.Fatalf("Migrate = %d, %v; want %d", applied, err, len(pending))
	}
	if again, err := Migrate(db); err != nil || again != 0 {
		t.Fatalf("second Migrate = %d, %v; want 0", again, err)
	}
	if _, err := os.Stat(path + ".migrate.lock"); !os.IsNotExist(err) {
		t.Errorf("the lock file was left behind: %v", err)
	}

	if err := RollbackLast(db); err != nil {
		t.Fatalf("RollbackLast: %v", err)
	}
	if err := Fresh(db); err != nil {
		t.Fatalf("Fresh: %v", err)
	}
	if !db.Migrator().HasTable(&widget{}) {
		t.Error("Fresh did not migrate again")
	}
	if err := Reset(db); err != nil {
		t.Fatalf("Reset: %v", err)
	}
	if left, _ := Applied(db); len(left) != 0 {
		t.Errorf("after Reset: %v still applied", left)
	}
}

func TestTheSqliteLockFileSerialisesProcesses(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shared.db")
	var active, overlaps, done atomic.Int32

	var waiting sync.WaitGroup
	for range 4 {
		waiting.Add(1)
		go func() {
			defer waiting.Done()
			// Each goroutine has its own pool, as a separate process would.
			db := sqliteAt(t, path)
			err := withLockFile(context.Background(), db, func() error {
				if active.Add(1) > 1 {
					overlaps.Add(1)
				}
				time.Sleep(30 * time.Millisecond)
				active.Add(-1)
				done.Add(1)
				return nil
			})
			if err != nil {
				t.Errorf("withLockFile: %v", err)
			}
		}()
	}
	waiting.Wait()

	if overlaps.Load() != 0 || done.Load() != 4 {
		t.Errorf("overlaps %d, done %d; want 0 and 4", overlaps.Load(), done.Load())
	}
}

func TestAStaleLockFileIsTakenOverAndAFreshOneWaitedFor(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.db")
	db := sqliteAt(t, path)
	lock := path + ".migrate.lock"

	if err := os.WriteFile(lock, []byte("4242\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(lock, old, old); err != nil {
		t.Fatal(err)
	}
	ran := false
	if err := WithMigrationLock(db, func() error { ran = true; return nil }); err != nil || !ran {
		t.Fatalf("stale lock = %v, ran %v", err, ran)
	}

	if err := os.WriteFile(lock, []byte("4242\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	previous := LockTimeout
	LockTimeout = 100 * time.Millisecond
	defer func() { LockTimeout = previous }()

	err := WithMigrationLock(db, func() error { t.Error("ran without the lock"); return nil })
	if !errors.Is(err, ErrLockTimeout) {
		t.Errorf("held lock = %v, want ErrLockTimeout", err)
	}
}

func TestAnInMemoryDatabaseNeedsNoLockFile(t *testing.T) {
	db := sqliteAt(t, ":memory:")
	file, err := sqliteFile(db)
	if err != nil || file != "" {
		t.Errorf("sqliteFile = %q, %v", file, err)
	}
	if err := WithMigrationLock(db, func() error { return nil }); err != nil {
		t.Error(err)
	}
}

// Problem l: one transaction for the batch; a failure leaves nothing.
func failingBatch() []*gormigrate.Migration {
	return []*gormigrate.Migration{
		{ID: "1_widgets", Migrate: func(tx *gorm.DB) error { return tx.AutoMigrate(&widget{}) }},
		{ID: "2_gadgets_then_fail", Migrate: func(tx *gorm.DB) error {
			if err := tx.AutoMigrate(&gadget{}); err != nil {
				return err
			}
			return errors.New("the migration breaks halfway")
		}},
	}
}

func assertNothingApplied(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := migratorFor(db, failingBatch()).Migrate(); err == nil {
		t.Fatal("the failing batch reported success")
	}
	for _, model := range []any{&widget{}, &gadget{}} {
		if db.Migrator().HasTable(model) {
			t.Errorf("%T survived the rollback", model)
		}
	}
	if applied, err := Applied(db); err != nil || len(applied) != 0 {
		t.Errorf("applied = %v, %v; want none", applied, err)
	}
}

func TestAFailingMigrationRollsBackOnSqlite(t *testing.T) {
	assertNothingApplied(t, sqliteAt(t, filepath.Join(t.TempDir(), "tx.db")))
}

func TestAFailingMigrationRollsBackOnPostgres(t *testing.T) {
	assertNothingApplied(t, postgresOrSkip(t))
}

func TestPostgresMigrationsWaitForTheAdvisoryLock(t *testing.T) {
	db := postgresOrSkip(t)
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}

	// Another replica holds the lock on its own session.
	holder, err := pool.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Close()
	if _, err := holder.ExecContext(context.Background(), "SELECT pg_advisory_lock($1)", advisoryKey); err != nil {
		t.Fatal(err)
	}

	previous := LockTimeout
	LockTimeout = 300 * time.Millisecond
	defer func() { LockTimeout = previous }()

	if err := WithMigrationLock(db, func() error { t.Error("ran while another replica migrates"); return nil }); !errors.Is(err, ErrLockTimeout) {
		t.Fatalf("held = %v, want ErrLockTimeout", err)
	}

	released := make(chan struct{})
	go func() {
		time.Sleep(100 * time.Millisecond)
		_, _ = holder.ExecContext(context.Background(), "SELECT pg_advisory_unlock($1)", advisoryKey)
		close(released)
	}()
	LockTimeout = 5 * time.Second
	ran := false
	if err := WithMigrationLock(db, func() error { ran = true; return nil }); err != nil || !ran {
		t.Fatalf("after release = %v, ran %v", err, ran)
	}
	<-released

	pool.SetMaxOpenConns(1)
	if err := WithMigrationLock(db, func() error { return nil }); err == nil {
		t.Error("a single connection pool was accepted: the migration would wait for the lock's own connection")
	}
}

func TestMySQLMigrationsWaitForTheNamedLock(t *testing.T) {
	dsn := os.Getenv(MySQLTestEnv)
	if dsn == "" {
		t.Skipf("%s is not set", MySQLTestEnv)
	}
	db, drop, err := openMySQLDatabase(dsn, fmt.Sprintf("dbtest_%d", time.Now().UnixNano()))
	if err != nil {
		t.Fatal(err)
	}
	defer drop()

	pool, _ := db.DB()
	holder, err := pool.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Close()
	var schema string
	if err := holder.QueryRowContext(context.Background(), "SELECT DATABASE()").Scan(&schema); err != nil {
		t.Fatal(err)
	}
	name := truncateLockName("omega_migrations:" + schema)
	if _, err := holder.ExecContext(context.Background(), "SELECT GET_LOCK(?, 0)", name); err != nil {
		t.Fatal(err)
	}

	previous := LockTimeout
	LockTimeout = 300 * time.Millisecond
	defer func() { LockTimeout = previous }()
	if err := WithMigrationLock(db, func() error { t.Error("ran without the lock"); return nil }); !errors.Is(err, ErrLockTimeout) {
		t.Fatalf("held = %v, want ErrLockTimeout", err)
	}

	if _, err := holder.ExecContext(context.Background(), "SELECT RELEASE_LOCK(?)", name); err != nil {
		t.Fatal(err)
	}
	ran := false
	if err := WithMigrationLock(db, func() error { ran = true; return nil }); err != nil || !ran {
		t.Fatalf("after release = %v, ran %v", err, ran)
	}
}
