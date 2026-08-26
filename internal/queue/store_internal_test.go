package queue

import (
	"context"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"omega/app/models"
)

func jobsDB(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("ouverture: %v", err)
	}
	if err := db.AutoMigrate(&models.Job{}); err != nil {
		t.Fatalf("migration: %v", err)
	}
	return db
}

func TestPurgeRemovesOnlyTheAskedStatus(t *testing.T) {
	db := jobsDB(t)
	store := NewStore(db)
	ctx := context.Background()

	rows := []models.Job{
		{Name: "a", Queue: "default", Status: models.JobFailed},
		{Name: "b", Queue: "default", Status: models.JobFailed},
		{Name: "c", Queue: "default", Status: models.JobPending},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}

	removed, err := store.Purge(ctx, models.JobFailed)
	if err != nil {
		t.Fatalf("Purge: %v", err)
	}
	if removed != 2 {
		t.Fatalf("Purge = %d, want 2", removed)
	}

	left, err := store.Pending(ctx)
	if err != nil || left != 1 {
		t.Fatalf("restants = %d, %v", left, err)
	}
}

func TestPurgeWithoutStatusEmptiesTheTable(t *testing.T) {
	db := jobsDB(t)
	store := NewStore(db)

	if err := db.Create(&[]models.Job{
		{Name: "a", Queue: "default", Status: models.JobFailed},
		{Name: "b", Queue: "default", Status: models.JobPending},
	}).Error; err != nil {
		t.Fatal(err)
	}

	removed, err := store.Purge(context.Background(), "")
	if err != nil || removed != 2 {
		t.Fatalf("Purge = %d, %v", removed, err)
	}
}

func TestReclaimReturnsStaleRunningJobsToPending(t *testing.T) {
	db := jobsDB(t)
	store := NewStore(db)
	ctx := context.Background()

	stale := time.Now().Add(-time.Hour)
	fresh := time.Now()

	if err := db.Create(&[]models.Job{
		{Name: "abandonnee", Queue: "default", Status: models.JobRunning, ClaimedAt: &stale},
		{Name: "en-cours", Queue: "default", Status: models.JobRunning, ClaimedAt: &fresh},
		{Name: "en-attente", Queue: "default", Status: models.JobPending},
	}).Error; err != nil {
		t.Fatal(err)
	}

	reclaimed, err := store.Reclaim(ctx, time.Now().Add(-time.Minute))
	if err != nil {
		t.Fatalf("Reclaim: %v", err)
	}
	if reclaimed != 1 {
		t.Fatalf("Reclaim = %d, want 1", reclaimed)
	}

	pending, err := store.Pending(ctx)
	if err != nil || pending != 2 {
		t.Fatalf("en attente = %d, %v", pending, err)
	}

	var revived models.Job
	if err := db.Where("name = ?", "abandonnee").Take(&revived).Error; err != nil {
		t.Fatal(err)
	}
	if revived.ClaimedAt != nil {
		t.Error("la reprise n'a pas efface claimed_at")
	}
}
