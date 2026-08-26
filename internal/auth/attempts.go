package auth

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"

	"omega/app/models"
)

var ErrTooManyAttempts = errors.New("auth: too many failed attempts for this account")

type Attempts struct {
	db     *gorm.DB
	max    int
	window time.Duration
}

// A project upgraded from an earlier version does not have the table yet.
// Better to disable ourselves out loud than to fail every sign-in.
func NewAttempts(db *gorm.DB, max int, window time.Duration) *Attempts {
	if window <= 0 {
		window = 15 * time.Minute
	}
	if db != nil && max > 0 && !db.Migrator().HasTable(&models.LoginAttempt{}) {
		db.Logger.Warn(context.Background(),
			"auth: table login_attempts absente, limite par compte desactivee — lancez omega migrate")
		return &Attempts{db: nil, max: max, window: window}
	}
	return &Attempts{db: db, max: max, window: window}
}

func (a *Attempts) enabled() bool { return a != nil && a.db != nil && a.max > 0 }

func (a *Attempts) Allow(ctx context.Context, identifier string) error {
	if !a.enabled() {
		return nil
	}

	var failures int64
	err := a.db.WithContext(ctx).Model(&models.LoginAttempt{}).
		Where("identifier = ? AND created_at > ?", normalise(identifier), time.Now().Add(-a.window)).
		Count(&failures).Error
	if err != nil {
		return err
	}
	if failures >= int64(a.max) {
		return ErrTooManyAttempts
	}
	return nil
}

func (a *Attempts) Record(ctx context.Context, identifier, address string) error {
	if !a.enabled() {
		return nil
	}
	return a.db.WithContext(ctx).Create(&models.LoginAttempt{
		Identifier: normalise(identifier),
		Address:    address,
	}).Error
}

func (a *Attempts) Clear(ctx context.Context, identifier string) error {
	if !a.enabled() {
		return nil
	}
	return a.db.WithContext(ctx).
		Where("identifier = ?", normalise(identifier)).
		Delete(&models.LoginAttempt{}).Error
}

func (a *Attempts) Purge(ctx context.Context, before time.Time) (int64, error) {
	if !a.enabled() {
		return 0, nil
	}
	result := a.db.WithContext(ctx).Where("created_at < ?", before).Delete(&models.LoginAttempt{})
	return result.RowsAffected, result.Error
}

func normalise(identifier string) string {
	return strings.ToLower(strings.TrimSpace(identifier))
}
