package scheduler

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"gorm.io/gorm"

	"omega/app/models"
	"omega/internal/database"
)

func eachEngine(t *testing.T, test func(t *testing.T, db *gorm.DB)) {
	t.Run("sqlite", func(t *testing.T) {
		db, err := database.Open(database.Connection{Driver: "sqlite", DSN: filepath.Join(t.TempDir(), "locks.db")},
			zerolog.New(io.Discard), "silent", 0)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if pool, err := db.DB(); err == nil {
				_ = pool.Close()
			}
		})
		migrateLocks(t, db)
		test(t, db)
	})

	if dsn := os.Getenv(database.MySQLTestEnv); dsn != "" {
		t.Run("mysql", func(t *testing.T) {
			db, drop, err := openMySQLDatabase(dsn, fmt.Sprintf("sched_test_%d", time.Now().UnixNano()))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(drop)
			migrateLocks(t, db)
			test(t, db)
		})
	}

	dsn := os.Getenv(database.PostgresTestEnv)
	if dsn == "" {
		return
	}
	t.Run("postgres", func(t *testing.T) {
		schema := strings.ToLower(fmt.Sprintf("sched_%d_%s", time.Now().UnixNano()%1e6,
			strings.NewReplacer("/", "_", "-", "_").Replace(t.Name())))
		if len(schema) > 60 {
			schema = schema[:60]
		}
		db, drop, err := openPostgresSchema(dsn, schema)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(drop)
		migrateLocks(t, db)
		test(t, db)
	})
}

func migrateLocks(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := db.AutoMigrate(&models.ScheduleLock{}); err != nil {
		t.Fatal(err)
	}
}

func TestOnlyOneReplicaAcquiresASlot(t *testing.T) {
	eachEngine(t, func(t *testing.T, db *gorm.DB) {
		ctx := context.Background()
		slot := time.Now().Truncate(time.Minute)

		var won atomic.Int32
		var waiting sync.WaitGroup
		for range 12 {
			waiting.Add(1)
			go func() {
				defer waiting.Done()
				ok, err := NewDBLocker(db).Acquire(ctx, "refresh-quotes", slot, time.Minute, true)
				if err != nil {
					t.Errorf("acquire: %v", err)
				}
				if ok {
					won.Add(1)
				}
			}()
		}
		waiting.Wait()
		if won.Load() != 1 {
			t.Fatalf("%d replicas acquired the slot, want 1", won.Load())
		}
	})
}

func TestTheLeaseHonoursOverlap(t *testing.T) {
	eachEngine(t, func(t *testing.T, db *gorm.DB) {
		ctx := context.Background()
		first, second := NewDBLocker(db), NewDBLocker(db)
		slot := time.Now().Truncate(time.Minute)
		next := slot.Add(time.Minute)

		if ok, err := first.Acquire(ctx, "task", slot, time.Hour, true); !ok || err != nil {
			t.Fatalf("first = %v, %v", ok, err)
		}
		if ok, _ := second.Acquire(ctx, "task", slot, time.Hour, true); ok {
			t.Fatal("the same slot was acquired twice")
		}
		// No overlap: the next slot waits for the first run to end.
		if ok, _ := second.Acquire(ctx, "task", next, time.Hour, true); ok {
			t.Fatal("the next slot started while the lease of the previous run is alive")
		}
		// Overlap allowed: it may start.
		if ok, _ := second.Acquire(ctx, "overlapping", slot, time.Hour, false); !ok {
			t.Fatal("first run of an overlapping task refused")
		}
		if ok, _ := first.Acquire(ctx, "overlapping", next, time.Hour, false); !ok {
			t.Fatal("an overlapping task could not start its next slot")
		}

		// Only the owner can release.
		if err := second.Release(ctx, "task", slot); err != nil {
			t.Fatal(err)
		}
		if ok, _ := second.Acquire(ctx, "task", next, time.Hour, true); ok {
			t.Fatal("a replica released a lease it does not own")
		}
		if err := first.Release(ctx, "task", slot); err != nil {
			t.Fatal(err)
		}
		if ok, err := second.Acquire(ctx, "task", next, time.Hour, true); !ok || err != nil {
			t.Fatalf("after release = %v, %v", ok, err)
		}

		// An expired lease (a dead replica) frees the task.
		third := NewDBLocker(db)
		third.now = func() time.Time { return time.Now().Add(2 * time.Hour) }
		if ok, _ := third.Acquire(ctx, "task", next.Add(time.Minute), time.Hour, true); !ok {
			t.Fatal("an expired lease still blocks the task")
		}

		var row models.ScheduleLock
		if err := db.Where("name = ?", "task").Take(&row).Error; err != nil {
			t.Fatal(err)
		}
		if row.Owner != third.Owner() {
			t.Errorf("owner = %q, want %q", row.Owner, third.Owner())
		}
	})
}

// Two scheduler replicas, one task: it runs once per slot.
func TestTwoReplicasRunATaskOncePerSlot(t *testing.T) {
	eachEngine(t, func(t *testing.T, db *gorm.DB) {
		var runs atomic.Int32
		task := func(context.Context) error {
			runs.Add(1)
			time.Sleep(20 * time.Millisecond)
			return nil
		}

		replicas := []*Scheduler{New(silent), New(silent), New(silent)}
		for _, replica := range replicas {
			replica.UseLocker(NewDBLocker(db)).Every("refresh", time.Minute, task)
		}

		slot := time.Now().Truncate(time.Minute)
		for round := range 3 {
			now := slot.Add(time.Duration(round) * time.Minute)
			for _, replica := range replicas {
				replica.mu.Lock()
				replica.entries[0].nextRun = now
				replica.mu.Unlock()
			}
			var ticking sync.WaitGroup
			for _, replica := range replicas {
				ticking.Add(1)
				go func() {
					defer ticking.Done()
					replica.tick(context.Background(), now.Add(time.Second))
				}()
			}
			ticking.Wait()
			for _, replica := range replicas {
				replica.inflight.Wait()
			}
			if got := runs.Load(); got != int32(round+1) {
				t.Fatalf("after slot %d the task ran %d times, want %d", round, got, round+1)
			}
		}
	})
}

type failingLocker struct{ calls atomic.Int32 }

func (l *failingLocker) Acquire(context.Context, string, time.Time, time.Duration, bool) (bool, error) {
	l.calls.Add(1)
	return false, errors.New("database is down")
}

func (l *failingLocker) Release(context.Context, string, time.Time) error { return nil }

func TestALockErrorSkipsTheRun(t *testing.T) {
	var runs atomic.Int32
	locker := &failingLocker{}
	s := New(silent).UseLocker(locker)
	s.Every("guarded", time.Minute, func(context.Context) error { runs.Add(1); return nil })

	s.mu.Lock()
	s.entries[0].nextRun = time.Now().Add(-time.Second)
	s.mu.Unlock()
	s.tick(context.Background(), time.Now())
	s.inflight.Wait()

	if locker.calls.Load() != 1 || runs.Load() != 0 {
		t.Errorf("lock calls %d, runs %d; want 1 and 0 (fail closed)", locker.calls.Load(), runs.Load())
	}
}
