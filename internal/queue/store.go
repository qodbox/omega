package queue

import (
	"context"
	"time"

	"gorm.io/gorm"

	"omega/app/models"
)

type gormStore struct{ db *gorm.DB }

func NewStore(db *gorm.DB) Store { return &gormStore{db: db} }

func (s *gormStore) Push(ctx context.Context, name, queue, payload string, runAt time.Time, maxTries int) error {
	return s.db.WithContext(ctx).Create(&models.Job{
		Queue:    queue,
		Name:     name,
		Payload:  payload,
		Status:   models.JobPending,
		MaxTries: maxTries,
		RunAt:    runAt,
	}).Error
}

func (s *gormStore) Claim(ctx context.Context, queues []string, now time.Time) (*Entry, error) {
	var job models.Job

	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		query := tx.Where("status = ? AND run_at <= ?", models.JobPending, now)
		if len(queues) > 0 {
			query = query.Where("queue IN ?", queues)
		}
		if err := query.Order("run_at asc").First(&job).Error; err != nil {
			return err
		}

		claimed := now
		result := tx.Model(&models.Job{}).
			Where("id = ? AND status = ?", job.ID, models.JobPending).
			Updates(map[string]any{
				"status":     models.JobRunning,
				"attempts":   job.Attempts + 1,
				"claimed_at": claimed,
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

	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}

	return &Entry{
		ID:        job.ID,
		Queue:     job.Queue,
		Name:      job.Name,
		Payload:   job.Payload,
		Attempts:  job.Attempts,
		MaxTries:  job.MaxTries,
		LastError: job.LastError,
	}, nil
}

func (s *gormStore) Release(ctx context.Context, id uint, runAt time.Time, failure string) error {
	return s.db.WithContext(ctx).Model(&models.Job{}).Where("id = ?", id).
		Updates(map[string]any{
			"status":     models.JobPending,
			"run_at":     runAt,
			"last_error": failure,
			"claimed_at": nil,
			"updated_at": time.Now(),
		}).Error
}

func (s *gormStore) Fail(ctx context.Context, id uint, failure string) error {
	return s.db.WithContext(ctx).Model(&models.Job{}).Where("id = ?", id).
		Updates(map[string]any{
			"status":     models.JobFailed,
			"last_error": failure,
			"claimed_at": nil,
			"updated_at": time.Now(),
		}).Error
}

func (s *gormStore) Complete(ctx context.Context, id uint) error {
	return s.db.WithContext(ctx).Delete(&models.Job{}, id).Error
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

func (s *gormStore) Reclaim(ctx context.Context, olderThan time.Time) (int64, error) {
	result := s.db.WithContext(ctx).Model(&models.Job{}).
		Where("status = ? AND claimed_at < ?", models.JobRunning, olderThan).
		Updates(map[string]any{
			"status":     models.JobPending,
			"claimed_at": nil,
			"updated_at": time.Now(),
		})
	return result.RowsAffected, result.Error
}

func (s *gormStore) Sweep(ctx context.Context, status string, olderThan time.Time) (int64, error) {
	query := s.db.WithContext(ctx).Where("updated_at < ?", olderThan)
	if status != "" {
		query = query.Where("status = ?", status)
	}
	result := query.Delete(&models.Job{})
	return result.RowsAffected, result.Error
}
