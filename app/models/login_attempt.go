package models

import "time"

type LoginAttempt struct {
	ID         uint      `gorm:"primarykey" json:"id"`
	Identifier string    `gorm:"size:255;not null;index:idx_login_attempts_identifier" json:"identifier"`
	Address    string    `gorm:"size:64;not null" json:"address"`
	CreatedAt  time.Time `gorm:"not null;index" json:"created_at"`
}

func (LoginAttempt) TableName() string { return "login_attempts" }
