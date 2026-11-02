package auth

import (
	"errors"
	"time"

	"gorm.io/gorm"
)

const TokenTable = "refresh_tokens"

var (
	// ErrUnknownToken is a refresh token the store has no live record of.
	ErrUnknownToken = errors.New("auth: unknown refresh token")

	// ErrReused is a refresh token presented again after it was rotated. It is
	// the signature of a stolen token: either the thief or the owner is
	// replaying it, and there is no telling which, so the family is revoked.
	ErrReused = errors.New("auth: refresh token reused")
)

// FamilyStore is a Store that remembers rotated tokens instead of forgetting
// them, which is what makes reuse detectable. A Store that does not implement
// it still works, without reuse detection.
type FamilyStore interface {
	Store

	// SaveFamily records a refresh token as a member of a login family.
	SaveFamily(id, subject, family string, expires time.Time) error

	// Consume marks a live token as spent and returns its family. A token that
	// was already spent returns its family, when it was spent, and ErrReused;
	// a token that is unknown or expired returns ErrUnknownToken.
	Consume(id string, now time.Time) (family string, usedAt time.Time, err error)

	// RevokeFamily forgets every token of a family, spent or not.
	RevokeFamily(family string) error
}

type gormStore struct{ db *gorm.DB }

// NewStore keeps refresh tokens in the refresh_tokens table.
func NewStore(db *gorm.DB) FamilyStore { return &gormStore{db: db} }

func (s *gormStore) Save(id, subject string, expires time.Time) error {
	return s.SaveFamily(id, subject, id, expires)
}

func (s *gormStore) SaveFamily(id, subject, family string, expires time.Time) error {
	return s.db.Table(TokenTable).Create(map[string]any{
		"id":         id,
		"subject":    subject,
		"family":     family,
		"expires_at": expires,
		"created_at": time.Now(),
	}).Error
}

func (s *gormStore) Exists(id string) (bool, error) {
	var count int64
	err := s.db.Table(TokenTable).
		Where("id = ? AND expires_at > ? AND used_at IS NULL", id, time.Now()).
		Count(&count).Error
	return count > 0, err
}

type tokenRow struct {
	Family string
	UsedAt *time.Time
}

func (s *gormStore) Consume(id string, now time.Time) (string, time.Time, error) {
	// One conditional UPDATE: of two requests racing with the same token, only
	// one sees a row affected.
	spent := s.db.Table(TokenTable).
		Where("id = ? AND used_at IS NULL AND expires_at > ?", id, now).
		Update("used_at", now)
	if spent.Error != nil {
		return "", time.Time{}, spent.Error
	}

	var row tokenRow
	err := s.db.Table(TokenTable).Select("family", "used_at").
		Where("id = ? AND expires_at > ?", id, now).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", time.Time{}, ErrUnknownToken
	}
	if err != nil {
		return "", time.Time{}, err
	}

	family := row.Family
	if family == "" {
		family = id
	}
	if spent.RowsAffected == 1 {
		return family, time.Time{}, nil
	}

	var usedAt time.Time
	if row.UsedAt != nil {
		usedAt = *row.UsedAt
	}
	return family, usedAt, ErrReused
}

func (s *gormStore) Revoke(id string) error {
	return s.db.Table(TokenTable).Where("id = ?", id).Delete(nil).Error
}

func (s *gormStore) RevokeFamily(family string) error {
	return s.db.Table(TokenTable).Where("family = ? OR id = ?", family, family).Delete(nil).Error
}

func (s *gormStore) RevokeSubject(subject string) error {
	return s.db.Table(TokenTable).Where("subject = ?", subject).Delete(nil).Error
}

func (s *gormStore) Purge(now time.Time) error {
	return s.db.Table(TokenTable).Where("expires_at <= ?", now).Delete(nil).Error
}
