package queue

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"omega/app/models"
)

type gormStore struct{ db *gorm.DB }

func NewStore(db *gorm.DB) Store { return &gormStore{db: db} }

func (s *gormStore) dialect() string { return strings.ToLower(s.db.Dialector.Name()) }

func (s *gormStore) Push(ctx context.Context, job NewJob) (bool, error) {
	row := models.Job{
		Queue:     job.Queue,
		Name:      job.Name,
		Payload:   job.Payload,
		Status:    models.JobPending,
		MaxTries:  job.MaxTries,
		TimeoutMs: job.Timeout.Milliseconds(),
		RunAt:     job.RunAt,
	}

	query := s.db.WithContext(ctx)
	if job.UniqueKey != "" {
		key := job.UniqueKey
		row.UniqueKey = &key
		// The unique index on unique_key is the arbiter: two API replicas
		// dispatching the same key at the same instant write one row.
		query = query.Clauses(clause.OnConflict{DoNothing: true})
	}

	result := query.Create(&row)
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

// Claim marks the oldest due job as running and returns it.
//
// PostgreSQL does it in one statement with FOR UPDATE SKIP LOCKED, so
// concurrent workers each get a different row instead of colliding on the
// same one. MySQL 8 has SKIP LOCKED but no UPDATE ... RETURNING, so it locks
// the row in a transaction. SQLite has a single writer: one UPDATE with a
// subquery is atomic there.
func (s *gormStore) Claim(ctx context.Context, queues []string, now time.Time) (*Entry, error) {
	var (
		job models.Job
		err error
	)

	switch s.dialect() {
	case "postgres":
		job, err = s.claimReturning(ctx, queues, now, " FOR UPDATE SKIP LOCKED")
	case "sqlite":
		job, err = s.claimReturning(ctx, queues, now, "")
	default:
		job, err = s.claimLocked(ctx, queues, now)
	}

	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return toEntry(job), nil
}

func (s *gormStore) claimReturning(ctx context.Context, queues []string, now time.Time, lock string) (models.Job, error) {
	filter, args := dueFilter(queues, now)

	statement := `UPDATE jobs SET status = ?, attempts = attempts + 1, claimed_at = ?, updated_at = ?
WHERE status = ? AND id = (SELECT id FROM jobs WHERE ` + filter + ` ORDER BY run_at, id LIMIT 1` + lock + `)
RETURNING id, queue, name, payload, attempts, max_tries, last_error, unique_key, timeout_ms`

	values := append([]any{models.JobRunning, now, now, models.JobPending}, args...)

	var rows []models.Job
	if err := s.db.WithContext(ctx).Raw(statement, values...).Scan(&rows).Error; err != nil {
		return models.Job{}, err
	}
	if len(rows) == 0 {
		return models.Job{}, gorm.ErrRecordNotFound
	}
	return rows[0], nil
}

func (s *gormStore) claimLocked(ctx context.Context, queues []string, now time.Time) (models.Job, error) {
	var job models.Job

	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		filter, args := dueFilter(queues, now)
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where(filter, args...).Order("run_at, id").Limit(1).Take(&job).Error; err != nil {
			return err
		}

		result := tx.Model(&models.Job{}).
			Where("id = ? AND status = ?", job.ID, models.JobPending).
			Updates(map[string]any{
				"status":     models.JobRunning,
				"attempts":   gorm.Expr("attempts + 1"),
				"claimed_at": now,
				"updated_at": now,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		job.Attempts++
		return nil
	})
	return job, err
}

func dueFilter(queues []string, now time.Time) (string, []any) {
	filter := "status = ? AND run_at <= ?"
	args := []any{models.JobPending, now}
	if len(queues) > 0 {
		filter += " AND queue IN ?"
		args = append(args, queues)
	}
	return filter, args
}

func toEntry(job models.Job) *Entry {
	entry := &Entry{
		ID:        job.ID,
		Queue:     job.Queue,
		Name:      job.Name,
		Payload:   job.Payload,
		Attempts:  job.Attempts,
		MaxTries:  job.MaxTries,
		LastError: job.LastError,
		Timeout:   time.Duration(job.TimeoutMs) * time.Millisecond,
	}
	if job.UniqueKey != nil {
		entry.UniqueKey = *job.UniqueKey
	}
	return entry
}

// claimed scopes a write to the claim this worker holds: the row is still
// running and its attempt counter has not moved since the claim. A reclaim
// followed by another worker's claim bumps the counter, which fences off the
// first worker.
func (s *gormStore) claimed(ctx context.Context, entry *Entry) *gorm.DB {
	return s.db.WithContext(ctx).Model(&models.Job{}).
		Where("id = ? AND status = ? AND attempts = ?", entry.ID, models.JobRunning, entry.Attempts)
}

func affected(result *gorm.DB) error {
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrLost
	}
	return nil
}

func (s *gormStore) Heartbeat(ctx context.Context, entry *Entry, now time.Time) error {
	return affected(s.claimed(ctx, entry).Updates(map[string]any{"claimed_at": now, "updated_at": now}))
}

func (s *gormStore) Release(ctx context.Context, entry *Entry, runAt time.Time, failure string, refund bool) error {
	attempts := gorm.Expr("attempts")
	if refund {
		attempts = gorm.Expr("attempts - 1")
	}
	return affected(s.claimed(ctx, entry).Updates(map[string]any{
		"status":     models.JobPending,
		"run_at":     runAt,
		"attempts":   attempts,
		"last_error": failure,
		"claimed_at": nil,
		"updated_at": time.Now(),
	}))
}

func (s *gormStore) Fail(ctx context.Context, entry *Entry, failure string) error {
	return affected(s.claimed(ctx, entry).Updates(map[string]any{
		"status":     models.JobFailed,
		"last_error": failure,
		"claimed_at": nil,
		// A failed job no longer holds its key: the next dispatch must go in.
		"unique_key": nil,
		"updated_at": time.Now(),
	}))
}

func (s *gormStore) Complete(ctx context.Context, entry *Entry) error {
	return affected(s.db.WithContext(ctx).
		Where("id = ? AND status = ? AND attempts = ?", entry.ID, models.JobRunning, entry.Attempts).
		Delete(&models.Job{}))
}

func (s *gormStore) Failed(ctx context.Context, limit int) ([]Entry, error) {
	var jobs []models.Job
	if err := s.db.WithContext(ctx).Where("status = ?", models.JobFailed).
		Order("updated_at desc").Limit(limit).Find(&jobs).Error; err != nil {
		return nil, err
	}

	entries := make([]Entry, 0, len(jobs))
	for _, job := range jobs {
		entries = append(entries, Entry{
			ID: job.ID, Queue: job.Queue, Name: job.Name,
			Attempts: job.Attempts, MaxTries: job.MaxTries, LastError: job.LastError,
		})
	}
	return entries, nil
}

func (s *gormStore) Retry(ctx context.Context, id uint) (int64, error) {
	query := s.db.WithContext(ctx).Model(&models.Job{}).Where("status = ?", models.JobFailed)
	if id > 0 {
		query = query.Where("id = ?", id)
	}

	result := query.Updates(map[string]any{
		"status":     models.JobPending,
		"attempts":   0,
		"run_at":     time.Now(),
		"last_error": "",
		"updated_at": time.Now(),
	})
	return result.RowsAffected, result.Error
}

func (s *gormStore) Purge(ctx context.Context, status string) (int64, error) {
	query := s.db.WithContext(ctx)
	if status != "" {
		query = query.Where("status = ?", status)
	} else {
		query = query.Session(&gorm.Session{AllowGlobalUpdate: true})
	}
	result := query.Delete(&models.Job{})
	return result.RowsAffected, result.Error
}

func (s *gormStore) Pending(ctx context.Context) (int64, error) {
	var total int64
	err := s.db.WithContext(ctx).Model(&models.Job{}).
		Where("status = ?", models.JobPending).Count(&total).Error
	return total, err
}

func (s *gormStore) Counts(ctx context.Context) (map[string]int64, error) {
	var rows []struct {
		Status string
		Total  int64
	}
	if err := s.db.WithContext(ctx).Model(&models.Job{}).
		Select("status, COUNT(*) AS total").Group("status").Scan(&rows).Error; err != nil {
		return nil, err
	}

	counts := map[string]int64{models.JobPending: 0, models.JobRunning: 0, models.JobFailed: 0}
	for _, row := range rows {
		counts[row.Status] = row.Total
	}
	return counts, nil
}

// LostWorker is the last_error of a job whose worker died on its last attempt.
const LostWorker = "queue: the worker running this job stopped responding (heartbeat lost)"

func (s *gormStore) Reclaim(ctx context.Context, olderThan time.Time) (int64, int64, error) {
	now := time.Now()
	// The dead run counted as an attempt when it was claimed: a job that
	// crashes its worker every time ends in failed instead of looping.
	exhausted := s.db.WithContext(ctx).Model(&models.Job{}).
		Where("status = ? AND claimed_at < ? AND attempts >= max_tries", models.JobRunning, olderThan).
		Updates(map[string]any{
			"status":     models.JobFailed,
			"last_error": LostWorker,
			"claimed_at": nil,
			"unique_key": nil,
			"updated_at": now,
		})
	if exhausted.Error != nil {
		return 0, 0, exhausted.Error
	}

	requeued := s.db.WithContext(ctx).Model(&models.Job{}).
		Where("status = ? AND claimed_at < ?", models.JobRunning, olderThan).
		Updates(map[string]any{
			"status":     models.JobPending,
			"last_error": LostWorker,
			"claimed_at": nil,
			"run_at":     now,
			"updated_at": now,
		})
	return requeued.RowsAffected, exhausted.RowsAffected, requeued.Error
}

func (s *gormStore) Sweep(ctx context.Context, status string, olderThan time.Time) (int64, error) {
	query := s.db.WithContext(ctx).Where("updated_at < ?", olderThan)
	if status != "" {
		query = query.Where("status = ?", status)
	}
	result := query.Delete(&models.Job{})
	return result.RowsAffected, result.Error
}
