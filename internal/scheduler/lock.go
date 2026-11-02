package scheduler

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"omega/app/models"
)

// DBLocker is a Locker backed by the schedule_locks table. It works the same
// on PostgreSQL, MySQL and SQLite: every decision is a single conditional
// UPDATE, which the database serialises on the row.
type DBLocker struct {
	db    *gorm.DB
	owner string
	now   func() time.Time
}

func NewDBLocker(db *gorm.DB) *DBLocker {
	return &DBLocker{db: db, owner: replicaID(), now: time.Now}
}

// Owner identifies this replica in schedule_locks.owner.
func (l *DBLocker) Owner() string { return l.owner }

var epoch = time.Unix(0, 0).UTC()

func (l *DBLocker) Acquire(ctx context.Context, name string, slot time.Time, ttl time.Duration, exclusive bool) (bool, error) {
	now := l.now().UTC()
	slot = slot.UTC().Truncate(time.Second)

	// The row is created once per task; losing that race is fine.
	if err := l.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).
		Create(&models.ScheduleLock{Name: name, Slot: epoch, ExpiresAt: epoch, UpdatedAt: now}).Error; err != nil {
		return false, err
	}

	// Moving the slot forward is the claim: of two replicas racing for the
	// same slot, the second finds slot already equal and updates nothing.
	query := l.db.WithContext(ctx).Model(&models.ScheduleLock{}).
		Where("name = ? AND slot < ?", name, slot)
	if exclusive {
		query = query.Where("expires_at <= ?", now)
	}
	result := query.Updates(map[string]any{
		"slot":       slot,
		"owner":      l.owner,
		"expires_at": now.Add(ttl),
		"updated_at": now,
	})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected == 1, nil
}

func (l *DBLocker) Release(ctx context.Context, name string, slot time.Time) error {
	now := l.now().UTC()
	return l.db.WithContext(ctx).Model(&models.ScheduleLock{}).
		Where("name = ? AND owner = ? AND slot = ?", name, l.owner, slot.UTC().Truncate(time.Second)).
		Updates(map[string]any{"expires_at": now, "updated_at": now}).Error
}

func replicaID() string {
	host, err := os.Hostname()
	if err != nil {
		host = "unknown"
	}
	suffix := make([]byte, 4)
	_, _ = rand.Read(suffix)
	return fmt.Sprintf("%s:%d:%s", host, os.Getpid(), hex.EncodeToString(suffix))
}
