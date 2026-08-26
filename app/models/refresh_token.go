package models

import "time"

type RefreshToken struct {
	ID        string    `gorm:"size:32;primarykey" json:"id"`
	Subject   string    `gorm:"size:64;not null;index" json:"subject"`
	ExpiresAt time.Time `gorm:"not null;index" json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
}

func (RefreshToken) TableName() string { return "refresh_tokens" }
