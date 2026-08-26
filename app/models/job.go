package models

import "time"

const (
	JobPending = "pending"
	JobRunning = "running"
	JobFailed  = "failed"
)

type Job struct {
	ID        uint       `gorm:"primarykey" json:"id"`
	Queue     string     `gorm:"size:64;not null;index" json:"queue"`
	Name      string     `gorm:"size:128;not null;index" json:"name"`
	Payload   string     `gorm:"type:text" json:"payload"`
	Status    string     `gorm:"size:16;not null;default:pending;index" json:"status"`
	Attempts  int        `gorm:"not null;default:0" json:"attempts"`
	MaxTries  int        `gorm:"not null;default:3" json:"max_tries"`
	LastError string     `gorm:"type:text" json:"last_error,omitempty"`
	RunAt     time.Time  `gorm:"not null;index" json:"run_at"`
	ClaimedAt *time.Time `json:"claimed_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

func (Job) TableName() string { return "jobs" }
