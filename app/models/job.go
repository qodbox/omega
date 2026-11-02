package models

import "time"

const (
	JobPending = "pending"
	JobRunning = "running"
	JobFailed  = "failed"
)

// Job is a row of the database queue.
//
// The payload is stored in clear: never put a secret (a token, a password, a
// card number) in it. Pass an identifier and let the handler load the rest.
type Job struct {
	ID        uint    `gorm:"primarykey" json:"id"`
	Queue     string  `gorm:"size:64;not null;index;index:idx_jobs_claim,priority:2" json:"queue"`
	Name      string  `gorm:"size:128;not null;index" json:"name"`
	Payload   string  `gorm:"type:text" json:"payload"`
	Status    string  `gorm:"size:16;not null;default:pending;index;index:idx_jobs_claim,priority:1" json:"status"`
	Attempts  int     `gorm:"not null;default:0" json:"attempts"`
	MaxTries  int     `gorm:"not null;default:3" json:"max_tries"`
	LastError string  `gorm:"type:text" json:"last_error,omitempty"`
	UniqueKey *string `gorm:"size:191;uniqueIndex:idx_jobs_unique_key" json:"unique_key,omitempty"`
	// TimeoutMs bounds one attempt; 0 means the worker's default.
	TimeoutMs int64     `gorm:"not null;default:0" json:"timeout_ms"`
	RunAt     time.Time `gorm:"not null;index;index:idx_jobs_claim,priority:3" json:"run_at"`
	// ClaimedAt is refreshed by the worker's heartbeat while the job runs: a
	// running row whose ClaimedAt is stale belongs to a dead worker.
	ClaimedAt *time.Time `json:"claimed_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

func (Job) TableName() string { return "jobs" }
