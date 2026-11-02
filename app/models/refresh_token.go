package models

import "time"

type RefreshToken struct {
	ID      string `gorm:"size:32;primarykey" json:"id"`
	Subject string `gorm:"size:64;not null;index" json:"subject"`
	// Family is the login chain the token descends from: every rotation keeps
	// it. Replaying a spent token revokes the whole family.
	Family string `gorm:"size:32;index" json:"family"`

	// UsedAt is when the token was rotated. A spent token is kept until it
	// expires, so a replay of it can be recognised.
	UsedAt *time.Time `json:"used_at,omitempty"`

	ExpiresAt time.Time `gorm:"not null;index" json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
}

func (RefreshToken) TableName() string { return "refresh_tokens" }
