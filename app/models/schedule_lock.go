package models

import "time"

// ScheduleLock is the lease a scheduler replica takes before running a task,
// so that a task runs once per slot however many `schedule:run` are alive.
type ScheduleLock struct {
	Name string `gorm:"primaryKey;size:191" json:"name"`
	// Slot is the scheduled time of the last run that was claimed.
	Slot  time.Time `gorm:"not null" json:"slot"`
	Owner string    `gorm:"size:191;not null;default:''" json:"owner"`
	// ExpiresAt ends the lease: past it, the run is considered finished (or
	// its replica dead).
	ExpiresAt time.Time `gorm:"not null" json:"expires_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (ScheduleLock) TableName() string { return "schedule_locks" }
