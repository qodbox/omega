package auth

import (
	"time"

	"gorm.io/gorm"
)

const TokenTable = "refresh_tokens"

type gormStore struct{ db *gorm.DB }

func NewStore(db *gorm.DB) Store { return &gormStore{db: db} }

func (s *gormStore) Save(id, subject string, expires time.Time) error {
	return s.db.Table(TokenTable).Create(map[string]any{
		"id":         id,
		"subject":    subject,
		"expires_at": expires,
		"created_at": time.Now(),
	}).Error
}

func (s *gormStore) Exists(id string) (bool, error) {
	var count int64
	err := s.db.Table(TokenTable).
		Where("id = ? AND expires_at > ?", id, time.Now()).
		Count(&count).Error
	return count > 0, err
}

func (s *gormStore) Revoke(id string) error {
	return s.db.Table(TokenTable).Where("id = ?", id).Delete(nil).Error
}

func (s *gormStore) RevokeSubject(subject string) error {
	return s.db.Table(TokenTable).Where("subject = ?", subject).Delete(nil).Error
}

func (s *gormStore) Purge(now time.Time) error {
	return s.db.Table(TokenTable).Where("expires_at <= ?", now).Delete(nil).Error
}
