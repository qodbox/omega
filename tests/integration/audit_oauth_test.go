package integration

import (
	"context"
	"errors"
	"testing"

	"github.com/markbates/goth"

	authsvc "omega/app/services/auth"
	jwt "omega/internal/auth"
)

func authService(t *testing.T, s *server) *authsvc.Service {
	t.Helper()

	guard := jwt.New(jwt.Config{
		Secret: "an-integration-secret-long-enough-to-sign",
		Store:  jwt.NewStore(s.app.DB),
	})
	return authsvc.NewService(s.app.DB, guard, s.app.Events)
}

func TestProviderCannotTakeOverAnExistingAccount(t *testing.T) {
	s := boot(t)
	s.signUp("proprietaire@integration.test")
	service := authService(t, s)

	_, err := service.SignInWithProvider(context.Background(), goth.User{
		Provider: "un-fournisseur",
		Email:    "proprietaire@integration.test",
		Name:     "Imposteur",
	})

	if !errors.Is(err, authsvc.ErrUnverifiedProvider) {
		t.Fatalf("un email non verifie a ouvert une session sur un compte existant: %v", err)
	}
}

func TestProviderLinksAnExistingAccountOnlyWhenVerified(t *testing.T) {
	s := boot(t)
	s.signUp("verifie@integration.test")
	service := authService(t, s)

	session, err := service.SignInWithProvider(context.Background(), goth.User{
		Provider: "google",
		Email:    "verifie@integration.test",
		RawData:  map[string]any{"email_verified": true},
	})
	if err != nil {
		t.Fatalf("un email verifie a ete refuse: %v", err)
	}
	if session.User.Email != "verifie@integration.test" {
		t.Fatalf("mauvais compte: %s", session.User.Email)
	}
}

func TestProviderAcceptsTheStringFormOfTheVerifiedFlag(t *testing.T) {
	s := boot(t)
	s.signUp("chaine@integration.test")
	service := authService(t, s)

	if _, err := service.SignInWithProvider(context.Background(), goth.User{
		Email:   "chaine@integration.test",
		RawData: map[string]any{"email_verified": "true"},
	}); err != nil {
		t.Fatalf("email_verified au format chaine refuse: %v", err)
	}
}

func TestAnUnverifiedProviderCreatesAnUnverifiedAccount(t *testing.T) {
	s := boot(t)
	service := authService(t, s)

	session, err := service.SignInWithProvider(context.Background(), goth.User{
		Email: "nouveau@integration.test",
		Name:  "Nouveau",
	})
	if err != nil {
		t.Fatalf("creation refusee: %v", err)
	}
	if session.User.EmailVerifiedAt != nil {
		t.Fatal("un email non verifie a ete marque comme verifie")
	}
}

func TestAVerifiedProviderCreatesAVerifiedAccount(t *testing.T) {
	s := boot(t)
	service := authService(t, s)

	session, err := service.SignInWithProvider(context.Background(), goth.User{
		Email:   "verifie-neuf@integration.test",
		RawData: map[string]any{"verified_email": true},
	})
	if err != nil {
		t.Fatalf("creation refusee: %v", err)
	}
	if session.User.EmailVerifiedAt == nil {
		t.Fatal("un email verifie n'a pas ete marque comme verifie")
	}
}

func TestProviderWithoutAnEmailIsRefused(t *testing.T) {
	s := boot(t)
	service := authService(t, s)

	if _, err := service.SignInWithProvider(context.Background(), goth.User{Name: "Sans email"}); !errors.Is(err, authsvc.ErrNoProviderEmail) {
		t.Fatalf("attendu ErrNoProviderEmail, obtenu %v", err)
	}
}

func TestDisabledProvidersAreNotRoutable(t *testing.T) {
	s := boot(t)

	if answer := s.get("/api/auth/inexistant"); answer.status != 404 {
		t.Errorf("redirection vers un fournisseur inactif: status = %d, want 404", answer.status)
	}
	if answer := s.get("/api/auth/inexistant/callback"); answer.status != 404 {
		t.Errorf("callback d'un fournisseur inactif: status = %d, want 404", answer.status)
	}

	listed := s.get("/api/auth/providers")
	if listed.status != 200 {
		t.Fatalf("liste des fournisseurs: status = %d", listed.status)
	}
	if _, ok := listed.body["data"]; !ok {
		t.Errorf("la liste ne contient pas data: %s", truncate(listed.raw))
	}
}
