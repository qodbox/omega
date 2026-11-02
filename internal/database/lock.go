package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"
)

// LockTimeout is how long a migration command waits for another one (a second
// replica deploying at the same time) before giving up.
var LockTimeout = 10 * time.Minute

// ErrLockTimeout is returned when the migration lock could not be taken.
var ErrLockTimeout = errors.New("database: timed out waiting for the migration lock")

const (
	// advisoryKey is the PostgreSQL advisory lock key of the migrations, an
	// arbitrary constant ("omega" + "mig" in ASCII). Advisory locks are per
	// database, so two applications on one server do not block each other.
	advisoryKey int64 = 0x6f6d6567616d6967
	lockPoll          = 250 * time.Millisecond
	// A SQLite lock file whose holder stopped touching it for this long
	// belongs to a crashed process.
	lockFileStale = 2 * time.Minute
	lockFileTouch = 20 * time.Second
)

var processLock sync.Mutex

// WithMigrationLock runs fn while holding the migration lock: a PostgreSQL
// advisory lock, a MySQL named lock (GET_LOCK), or a lock file next to the
// SQLite database. Two `migrate` started together therefore run one after the
// other, and the second finds nothing left to do.
func WithMigrationLock(db *gorm.DB, fn func() error) error {
	processLock.Lock()
	defer processLock.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), LockTimeout)
	defer cancel()

	switch strings.ToLower(db.Dialector.Name()) {
	case "postgres":
		return withSessionLock(ctx, db, fn,
			func(conn *sql.Conn) (bool, error) {
				var ok bool
				err := conn.QueryRowContext(ctx, "SELECT pg_try_advisory_lock($1)", advisoryKey).Scan(&ok)
				return ok, err
			},
			func(conn *sql.Conn) error {
				_, err := conn.ExecContext(context.Background(), "SELECT pg_advisory_unlock($1)", advisoryKey)
				return err
			})
	case "mysql":
		var name string
		return withSessionLock(ctx, db, fn,
			func(conn *sql.Conn) (bool, error) {
				if name == "" {
					var schema sql.NullString
					if err := conn.QueryRowContext(ctx, "SELECT DATABASE()").Scan(&schema); err != nil {
						return false, err
					}
					name = truncateLockName("omega_migrations:" + schema.String)
				}
				var ok sql.NullInt64
				err := conn.QueryRowContext(ctx, "SELECT GET_LOCK(?, 0)", name).Scan(&ok)
				return ok.Valid && ok.Int64 == 1, err
			},
			func(conn *sql.Conn) error {
				_, err := conn.ExecContext(context.Background(), "SELECT RELEASE_LOCK(?)", name)
				return err
			})
	default:
		return withLockFile(ctx, db, fn)
	}
}

// MySQL lock names are limited to 64 characters.
func truncateLockName(name string) string {
	if len(name) > 64 {
		return name[:64]
	}
	return name
}

// withSessionLock holds a session-level lock on a dedicated connection while
// fn runs on the pool.
func withSessionLock(ctx context.Context, db *gorm.DB, fn func() error,
	try func(*sql.Conn) (bool, error), release func(*sql.Conn) error) error {

	pool, err := db.DB()
	if err != nil {
		return err
	}
	if pool.Stats().MaxOpenConnections == 1 {
		return errors.New("database: migrations need at least two connections (one holds the lock)")
	}

	conn, err := pool.Conn(ctx)
	if err != nil {
		return fmt.Errorf("database: migration lock: %w", err)
	}
	defer conn.Close()

	for {
		acquired, err := try(conn)
		if err != nil {
			if ctx.Err() != nil {
				return ErrLockTimeout
			}
			return fmt.Errorf("database: migration lock: %w", err)
		}
		if acquired {
			break
		}
		select {
		case <-ctx.Done():
			return ErrLockTimeout
		case <-time.After(lockPoll):
		}
	}
	defer func() { _ = release(conn) }()

	return fn()
}

// withLockFile serialises SQLite migrations across processes with a lock file
// created exclusively next to the database. An in-memory database is private
// to its process, so the process mutex is enough there.
func withLockFile(ctx context.Context, db *gorm.DB, fn func() error) error {
	path, err := sqliteFile(db)
	if err != nil || path == "" {
		return fn()
	}

	lock := path + ".migrate.lock"
	for {
		file, err := os.OpenFile(lock, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			fmt.Fprintf(file, "%d\n", os.Getpid())
			_ = file.Close()
			break
		}
		if !errors.Is(err, os.ErrExist) {
			return fmt.Errorf("database: migration lock: %w", err)
		}
		if info, statErr := os.Stat(lock); statErr == nil && time.Since(info.ModTime()) > lockFileStale {
			_ = os.Remove(lock)
			continue
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("%w (%s)", ErrLockTimeout, lock)
		case <-time.After(lockPoll):
		}
	}

	stop := make(chan struct{})
	var touching sync.WaitGroup
	touching.Add(1)
	go func() {
		defer touching.Done()
		ticker := time.NewTicker(lockFileTouch)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case now := <-ticker.C:
				_ = os.Chtimes(lock, now, now)
			}
		}
	}()
	defer func() {
		close(stop)
		touching.Wait()
		_ = os.Remove(lock)
	}()

	return fn()
}

func sqliteFile(db *gorm.DB) (string, error) {
	var rows []struct {
		Seq  int
		Name string
		File string
	}
	if err := db.Raw("PRAGMA database_list").Scan(&rows).Error; err != nil {
		return "", err
	}
	for _, row := range rows {
		if row.Name == "main" {
			return row.File, nil
		}
	}
	return "", nil
}
