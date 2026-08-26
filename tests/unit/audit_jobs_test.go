package unit

import (
	"context"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"omega/app/jobs"
	"omega/app/listeners"
	"omega/app/models"
	"omega/internal/mail"
)

func memoryDB(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("ouverture: %v", err)
	}
	if err := db.Migrator().DropTable(&models.RefreshToken{}); err != nil {
		t.Fatalf("nettoyage: %v", err)
	}
	if err := db.AutoMigrate(&models.RefreshToken{}); err != nil {
		t.Fatalf("migration: %v", err)
	}
	return db
}

func TestPurgeTokensRemovesOnlyTheExpiredOnes(t *testing.T) {
	db := memoryDB(t)

	rows := []models.RefreshToken{
		{ID: "perime", Subject: "1", ExpiresAt: time.Now().Add(-time.Hour)},
		{ID: "juste", Subject: "1", ExpiresAt: time.Now().Add(-time.Second)},
		{ID: "valide", Subject: "1", ExpiresAt: time.Now().Add(time.Hour)},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatalf("insertion: %v", err)
	}

	if err := jobs.PurgeTokens(db, quiet)(context.Background(), nil); err != nil {
		t.Fatalf("purge: %v", err)
	}

	var left []models.RefreshToken
	if err := db.Find(&left).Error; err != nil {
		t.Fatal(err)
	}
	if len(left) != 1 || left[0].ID != "valide" {
		t.Fatalf("restent %d jeton(s): %+v", len(left), left)
	}
}

func TestPurgeTokensIsIdempotent(t *testing.T) {
	db := memoryDB(t)
	purge := jobs.PurgeTokens(db, quiet)

	for i := 0; i < 3; i++ {
		if err := purge(context.Background(), nil); err != nil {
			t.Fatalf("passe %d: %v", i, err)
		}
	}
}

func TestWelcomeEmailRejectsAMalformedPayload(t *testing.T) {
	handler := jobs.WelcomeEmail(mail.New(mail.Config{Driver: "log", From: "a@b.test"}, quiet), quiet)

	if err := handler(context.Background(), []byte("pas du json")); err == nil {
		t.Fatal("un payload invalide a ete accepte")
	}
}

func TestWelcomeEmailSendsThroughTheMailer(t *testing.T) {
	handler := jobs.WelcomeEmail(mail.New(mail.Config{Driver: "log", From: "a@b.test"}, quiet), quiet)

	if err := handler(context.Background(), []byte(`{"email":"cible@example.test","name":"Ada"}`)); err != nil {
		t.Fatalf("envoi: %v", err)
	}
}

func TestWelcomeEmailFailsWithoutARecipient(t *testing.T) {
	handler := jobs.WelcomeEmail(mail.New(mail.Config{Driver: "log", From: "a@b.test"}, quiet), quiet)

	if err := handler(context.Background(), []byte(`{"name":"Ada"}`)); err == nil {
		t.Fatal("un envoi sans destinataire a reussi")
	}
}

func TestLogRegistrationNeverFails(t *testing.T) {
	if err := listeners.LogRegistration(quiet)(context.Background(), map[string]any{"id": 1}); err != nil {
		t.Fatalf("listener: %v", err)
	}
	if err := listeners.LogRegistration(quiet)(context.Background(), nil); err != nil {
		t.Fatalf("listener avec payload nil: %v", err)
	}
}
