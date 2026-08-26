package integration

import (
	"testing"

	"omega/internal/database"
)

func TestMigrationStatusListsEveryMigrationAsApplied(t *testing.T) {
	s := boot(t)

	statuses, err := database.StatusList(s.app.DB)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if len(statuses) == 0 {
		t.Fatal("aucune migration listee")
	}
	for _, status := range statuses {
		if !status.Applied {
			t.Errorf("%s n'est pas appliquee apres Up", status.ID)
		}
	}
}

func TestRollbackLastThenUpIsReversible(t *testing.T) {
	s := boot(t)

	before, err := database.Applied(s.app.DB)
	if err != nil {
		t.Fatalf("applied: %v", err)
	}

	if err := database.RollbackLast(s.app.DB); err != nil {
		t.Fatalf("rollback: %v", err)
	}

	during, err := database.Applied(s.app.DB)
	if err != nil {
		t.Fatalf("applied: %v", err)
	}
	if len(during) != len(before)-1 {
		t.Fatalf("apres rollback: %d migrations, want %d", len(during), len(before)-1)
	}

	if err := database.Up(s.app.DB); err != nil {
		t.Fatalf("up apres rollback: %v", err)
	}

	after, err := database.Applied(s.app.DB)
	if err != nil {
		t.Fatalf("applied: %v", err)
	}
	if len(after) != len(before) {
		t.Fatalf("apres reprise: %d migrations, want %d", len(after), len(before))
	}
}

func TestUpIsIdempotent(t *testing.T) {
	s := boot(t)

	before, _ := database.Applied(s.app.DB)
	for i := 0; i < 3; i++ {
		if err := database.Up(s.app.DB); err != nil {
			t.Fatalf("passe %d: %v", i, err)
		}
	}
	after, _ := database.Applied(s.app.DB)

	if len(after) != len(before) {
		t.Fatalf("Up repete a change le compte: %d -> %d", len(before), len(after))
	}
}

func TestResetRollsEverythingBack(t *testing.T) {
	s := boot(t)

	if err := database.Reset(s.app.DB); err != nil {
		t.Fatalf("reset: %v", err)
	}

	applied, err := database.Applied(s.app.DB)
	if err != nil {
		t.Fatalf("applied: %v", err)
	}
	if len(applied) != 0 {
		t.Fatalf("apres Reset il reste %d migration(s): %v", len(applied), applied)
	}
}

func TestFreshRebuildsTheSchema(t *testing.T) {
	s := boot(t)

	if err := database.Fresh(s.app.DB); err != nil {
		t.Fatalf("fresh: %v", err)
	}

	statuses, err := database.StatusList(s.app.DB)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	for _, status := range statuses {
		if !status.Applied {
			t.Errorf("%s manquante apres Fresh", status.ID)
		}
	}
	if !s.app.DB.Migrator().HasTable("users") {
		t.Error("la table users n'existe pas apres Fresh")
	}
}
