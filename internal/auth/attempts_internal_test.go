package auth

import (
	"context"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"omega/app/models"
)

func attemptsDB(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("ouverture: %v", err)
	}
	if err := db.AutoMigrate(&models.LoginAttempt{}); err != nil {
		t.Fatalf("migration: %v", err)
	}
	return db
}

func TestAttemptsPurgeRemovesOnlyWhatIsExpired(t *testing.T) {
	db := attemptsDB(t)
	tracker := NewAttempts(db, 5, time.Minute)
	ctx := context.Background()

	if err := db.Create(&[]models.LoginAttempt{
		{Identifier: "vieux@test", Address: "1.1.1.1", CreatedAt: time.Now().Add(-time.Hour)},
		{Identifier: "recent@test", Address: "1.1.1.1", CreatedAt: time.Now()},
	}).Error; err != nil {
		t.Fatal(err)
	}

	removed, err := tracker.Purge(ctx, time.Now().Add(-time.Minute))
	if err != nil {
		t.Fatalf("Purge: %v", err)
	}
	if removed != 1 {
		t.Fatalf("Purge = %d, want 1", removed)
	}

	var left int64
	db.Model(&models.LoginAttempt{}).Count(&left)
	if left != 1 {
		t.Fatalf("restants = %d, want 1", left)
	}
}

func TestAttemptsAreDisabledWhenMaxIsZero(t *testing.T) {
	db := attemptsDB(t)
	off := NewAttempts(db, 0, time.Minute)
	ctx := context.Background()

	for i := 0; i < 50; i++ {
		if err := off.Record(ctx, "cible@test", "1.1.1.1"); err != nil {
			t.Fatalf("Record: %v", err)
		}
	}
	if err := off.Allow(ctx, "cible@test"); err != nil {
		t.Fatalf("Allow doit passer quand la limite est desactivee: %v", err)
	}
	if err := off.Clear(ctx, "cible@test"); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	if removed, err := off.Purge(ctx, time.Now()); err != nil || removed != 0 {
		t.Fatalf("Purge = %d, %v", removed, err)
	}

	var stored int64
	db.Model(&models.LoginAttempt{}).Count(&stored)
	if stored != 0 {
		t.Fatalf("%d tentative(s) enregistrees alors que la limite est desactivee", stored)
	}
}

func TestAttemptsToleratesANilTracker(t *testing.T) {
	var missing *Attempts
	ctx := context.Background()

	if err := missing.Allow(ctx, "x"); err != nil {
		t.Errorf("Allow: %v", err)
	}
	if err := missing.Record(ctx, "x", "1.1.1.1"); err != nil {
		t.Errorf("Record: %v", err)
	}
	if err := missing.Clear(ctx, "x"); err != nil {
		t.Errorf("Clear: %v", err)
	}
}

func TestAttemptsIgnoreCaseAndSurroundingSpace(t *testing.T) {
	db := attemptsDB(t)
	tracker := NewAttempts(db, 2, time.Minute)
	ctx := context.Background()

	if err := tracker.Record(ctx, "  Cible@Test  ", "1.1.1.1"); err != nil {
		t.Fatal(err)
	}
	if err := tracker.Record(ctx, "CIBLE@TEST", "2.2.2.2"); err != nil {
		t.Fatal(err)
	}

	if err := tracker.Allow(ctx, "cible@test"); err == nil {
		t.Fatal("la casse permet de contourner la limite")
	}
}

func TestAttemptsWindowDefaultsWhenNotSet(t *testing.T) {
	tracker := NewAttempts(attemptsDB(t), 3, 0)
	if tracker.window <= 0 {
		t.Fatalf("fenetre = %v", tracker.window)
	}
}

func TestAttemptsDisableThemselvesWithoutTheTable(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("ouverture: %v", err)
	}

	// A database older than the migration: the limiter must go quiet, not break.
	tracker := NewAttempts(db, 5, time.Minute)
	ctx := context.Background()

	if err := tracker.Allow(ctx, "cible@test"); err != nil {
		t.Fatalf("Allow doit passer sans la table: %v", err)
	}
	if err := tracker.Record(ctx, "cible@test", "1.1.1.1"); err != nil {
		t.Fatalf("Record doit passer sans la table: %v", err)
	}
	if err := tracker.Clear(ctx, "cible@test"); err != nil {
		t.Fatalf("Clear doit passer sans la table: %v", err)
	}
}
