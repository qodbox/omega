package unit

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"
	"gorm.io/gorm"

	"omega/app/models"
	"omega/internal/auth"
)

const aqsCovSecret = "aqs-cover-secret-long-enough-to-sign-tokens"

var aqsCovBoom = errors.New("aqs: panne du store")

type aqsCovStore struct {
	live       map[string]string
	failSave   bool
	failExists bool
	failRevoke bool
	failAll    bool
}

func aqsCovNewStore() *aqsCovStore { return &aqsCovStore{live: map[string]string{}} }

func (s *aqsCovStore) Save(id, subject string, _ time.Time) error {
	if s.failSave {
		return aqsCovBoom
	}
	s.live[id] = subject
	return nil
}

func (s *aqsCovStore) Exists(id string) (bool, error) {
	if s.failExists {
		return false, aqsCovBoom
	}
	_, ok := s.live[id]
	return ok, nil
}

func (s *aqsCovStore) Revoke(id string) error {
	if s.failRevoke {
		return aqsCovBoom
	}
	delete(s.live, id)
	return nil
}

func (s *aqsCovStore) RevokeSubject(subject string) error {
	if s.failAll {
		return aqsCovBoom
	}
	for id, held := range s.live {
		if held == subject {
			delete(s.live, id)
		}
	}
	return nil
}

func (s *aqsCovStore) Purge(time.Time) error { return nil }

func aqsCovGuard(store auth.Store) *auth.Guard {
	return auth.New(auth.Config{
		Secret:     aqsCovSecret,
		AccessTTL:  time.Minute,
		RefreshTTL: time.Hour,
		Store:      store,
	})
}

func aqsCovCall(t *testing.T, app *fiber.App, path, header string) *http.Response {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, path, nil)
	if header != "" {
		request.Header.Set(fiber.HeaderAuthorization, header)
	}
	answer, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("appel %s: %v", path, err)
	}
	return answer
}

func TestAqsCovIssueSurfacesTheStoreFailure(t *testing.T) {
	store := aqsCovNewStore()
	store.failSave = true

	if _, err := aqsCovGuard(store).Issue("7", "user"); !errors.Is(err, aqsCovBoom) {
		t.Fatalf("Issue = %v, want la panne du store", err)
	}
}

func TestAqsCovRotateRejectsAnUnreadableToken(t *testing.T) {
	if _, err := aqsCovGuard(aqsCovNewStore()).Rotate("pas-un-jeton", "user"); err != auth.ErrMalformed {
		t.Fatalf("Rotate = %v, want ErrMalformed", err)
	}
}

func TestAqsCovRotateSurfacesTheLookupFailure(t *testing.T) {
	store := aqsCovNewStore()
	guard := aqsCovGuard(store)

	tokens, err := guard.Issue("7", "user")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	store.failExists = true
	if _, err := guard.Rotate(tokens.RefreshToken, "user"); !errors.Is(err, aqsCovBoom) {
		t.Fatalf("Rotate = %v, want la panne du store", err)
	}
}

func TestAqsCovRotateRefusesAJetonDejaRevoque(t *testing.T) {
	store := aqsCovNewStore()
	guard := aqsCovGuard(store)

	tokens, err := guard.Issue("7", "user")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if err := guard.Revoke(tokens.RefreshToken); err != nil {
		t.Fatalf("Revoke: %v", err)
	}

	if _, err := guard.Rotate(tokens.RefreshToken, "user"); err != auth.ErrSignature {
		t.Fatalf("Rotate = %v, want ErrSignature", err)
	}
}

func TestAqsCovRotateSurfacesTheRevokeFailure(t *testing.T) {
	store := aqsCovNewStore()
	guard := aqsCovGuard(store)

	tokens, err := guard.Issue("7", "user")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	store.failRevoke = true
	if _, err := guard.Rotate(tokens.RefreshToken, "user"); !errors.Is(err, aqsCovBoom) {
		t.Fatalf("Rotate = %v, want la panne du store", err)
	}
}

func TestAqsCovRevokeRejectsAnUnreadableToken(t *testing.T) {
	if err := aqsCovGuard(aqsCovNewStore()).Revoke("pas-un-jeton"); err != auth.ErrMalformed {
		t.Fatalf("Revoke = %v, want ErrMalformed", err)
	}
}

func TestAqsCovRevokeWithoutStoreIsANoOp(t *testing.T) {
	guard := aqsCovGuard(nil)

	tokens, err := guard.Issue("7", "user")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if err := guard.Revoke(tokens.RefreshToken); err != nil {
		t.Fatalf("Revoke sans store = %v, want nil", err)
	}
	if err := guard.RevokeAll("7"); err != nil {
		t.Fatalf("RevokeAll sans store = %v, want nil", err)
	}

	if _, err := guard.Rotate(tokens.RefreshToken, "user"); err != nil {
		t.Fatalf("le jeton reste rejouable sans store, Rotate = %v", err)
	}
}

func TestAqsCovRevokeAllSurfacesTheStoreFailure(t *testing.T) {
	store := aqsCovNewStore()
	store.failAll = true

	if err := aqsCovGuard(store).RevokeAll("7"); !errors.Is(err, aqsCovBoom) {
		t.Fatalf("RevokeAll = %v, want la panne du store", err)
	}
}

func TestAqsCovOptionalAttachesTheClaimsWhenTheTokenIsValid(t *testing.T) {
	guard := aqsCovGuard(aqsCovNewStore())

	tokens, err := guard.Issue("7", "admin")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	app := fiber.New()
	app.Get("/libre", guard.Optional(), func(c *fiber.Ctx) error {
		claims, ok := auth.ClaimsOf(c)
		if !ok {
			return c.SendStatus(fiber.StatusTeapot)
		}
		return c.SendString(claims.Subject)
	})

	answer := aqsCovCall(t, app, "/libre", "Bearer "+tokens.AccessToken)
	if answer.StatusCode != fiber.StatusOK {
		t.Fatalf("Optional avec un jeton valide = %d, want 200", answer.StatusCode)
	}
}

func TestAqsCovRolesSeparatesAllowedFromForbidden(t *testing.T) {
	guard := aqsCovGuard(aqsCovNewStore())

	tokens, err := guard.Issue("7", "editor")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	app := fiber.New()
	app.Get("/editor", guard.Required(), guard.Roles("editor"), func(c *fiber.Ctx) error {
		return c.SendStatus(fiber.StatusNoContent)
	})
	app.Get("/admin", guard.Required(), guard.Roles("admin"), func(c *fiber.Ctx) error {
		return c.SendStatus(fiber.StatusNoContent)
	})

	if answer := aqsCovCall(t, app, "/editor", "Bearer "+tokens.AccessToken); answer.StatusCode != fiber.StatusNoContent {
		t.Fatalf("role autorise = %d, want 204", answer.StatusCode)
	}
	if answer := aqsCovCall(t, app, "/admin", "Bearer "+tokens.AccessToken); answer.StatusCode != fiber.StatusForbidden {
		t.Fatalf("role refuse = %d, want 403", answer.StatusCode)
	}
}

func TestAqsCovExpiredAccessTokenIsReportedAsExpired(t *testing.T) {
	guard := aqsCovGuard(aqsCovNewStore())

	stale, err := auth.Sign([]byte(aqsCovSecret), auth.Claims{
		Kind: auth.KindAccess,
		Role: "user",
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "7",
			IssuedAt:  jwt.NewNumericDate(time.Now().Add(-2 * time.Hour)),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Hour)),
		},
	})
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	app := fiber.New()
	app.Get("/prive", guard.Required(), func(c *fiber.Ctx) error {
		return c.SendStatus(fiber.StatusNoContent)
	})

	answer := aqsCovCall(t, app, "/prive", "Bearer "+stale)
	if answer.StatusCode != fiber.StatusUnauthorized {
		t.Fatalf("jeton expire = %d, want 401", answer.StatusCode)
	}
}

func TestAqsCovUserWalksEveryLoaderOutcome(t *testing.T) {
	loaded := map[string]any{"id": 7}

	cases := []struct {
		name   string
		loader auth.Loader
		claims bool
		cached bool
		want   string
	}{
		{name: "cache", claims: false, cached: true, want: "cache"},
		{name: "sans claims", claims: false, want: ""},
		{name: "sans loader", claims: true, want: ""},
		{name: "loader en erreur", claims: true, loader: func(string) (any, error) { return nil, aqsCovBoom }, want: ""},
		{name: "loader vide", claims: true, loader: func(string) (any, error) { return nil, nil }, want: ""},
		{name: "loader complet", claims: true, loader: func(string) (any, error) { return loaded, nil }, want: "charge"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			guard := auth.New(auth.Config{Secret: aqsCovSecret, Loader: tc.loader})

			app := fiber.New()
			app.Get("/moi", func(c *fiber.Ctx) error {
				if tc.claims {
					c.Locals(auth.LocalsClaims, auth.Claims{Role: "user"})
				}
				if tc.cached {
					c.Locals(auth.LocalsUser, "cache")
				}
				user := guard.User(c)
				if user == nil {
					return c.SendString("")
				}
				if tc.cached {
					return c.SendString(user.(string))
				}
				return c.SendString("charge")
			})

			answer := aqsCovCall(t, app, "/moi", "")
			body := make([]byte, 32)
			read, _ := answer.Body.Read(body)
			if got := string(body[:read]); got != tc.want {
				t.Fatalf("User = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestAqsCovUserCachesTheLoadedValue(t *testing.T) {
	calls := 0
	guard := auth.New(auth.Config{
		Secret: aqsCovSecret,
		Loader: func(string) (any, error) { calls++; return map[string]any{"id": 7}, nil },
	})

	app := fiber.New()
	app.Get("/moi", func(c *fiber.Ctx) error {
		c.Locals(auth.LocalsClaims, auth.Claims{})
		guard.User(c)
		guard.User(c)
		return c.SendStatus(fiber.StatusNoContent)
	})

	aqsCovCall(t, app, "/moi", "")
	if calls != 1 {
		t.Fatalf("le loader a ete appele %d fois, want 1", calls)
	}
}

func TestAqsCovFingerprintHandlesEveryHeaderShape(t *testing.T) {
	guard := aqsCovGuard(aqsCovNewStore())

	tokens, err := guard.Issue("7", "user")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	app := fiber.New()
	app.Get("/empreinte", func(c *fiber.Ctx) error { return c.SendString(auth.Fingerprint(c)) })

	cases := []struct {
		name   string
		header string
		want   int
	}{
		{name: "sans en-tete", header: "", want: 0},
		{name: "signature trop courte", header: "Bearer aaa.bbb.court", want: 0},
		{name: "en-tete avec espaces", header: "Bearer un jeton", want: 0},
		{name: "jeton complet", header: "Bearer " + tokens.AccessToken, want: 16},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			answer := aqsCovCall(t, app, "/empreinte", tc.header)
			body := make([]byte, 64)
			read, _ := answer.Body.Read(body)
			if read != tc.want {
				t.Fatalf("Fingerprint = %q (%d octets), want %d", string(body[:read]), read, tc.want)
			}
		})
	}
}

func TestAqsCovBearerStripsAtMostTwoPrefixes(t *testing.T) {
	app := fiber.New()
	app.Get("/empreinte", func(c *fiber.Ctx) error { return c.SendString(auth.Fingerprint(c)) })

	answer := aqsCovCall(t, app, "/empreinte", "Bearer Bearer aaa.bbb.0123456789abcdef0")
	body := make([]byte, 64)
	read, _ := answer.Body.Read(body)
	if got := string(body[:read]); got != "0123456789abcdef" {
		t.Fatalf("un double prefixe Bearer donne %q, want 0123456789abcdef", got)
	}
}

func aqsCovAttemptsDB(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "attempts.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("ouverture: %v", err)
	}
	if err := db.AutoMigrate(&models.LoginAttempt{}); err != nil {
		t.Fatalf("migration: %v", err)
	}
	return db
}

func TestAqsCovAllowSurfacesTheCountFailure(t *testing.T) {
	db := aqsCovAttemptsDB(t)
	tracker := auth.NewAttempts(db, 3, time.Minute)

	if err := db.Migrator().DropTable(&models.LoginAttempt{}); err != nil {
		t.Fatalf("suppression de la table: %v", err)
	}

	if err := tracker.Allow(context.Background(), "cible@test"); err == nil {
		t.Fatal("Allow = nil alors que la table a disparu, want une erreur")
	}
}

func TestAqsCovDisabledAttemptsNeverBlock(t *testing.T) {
	tracker := auth.NewAttempts(nil, 3, 0)
	ctx := context.Background()

	if err := tracker.Allow(ctx, "cible@test"); err != nil {
		t.Fatalf("Allow desactive = %v, want nil", err)
	}
	if err := tracker.Record(ctx, "cible@test", "1.1.1.1"); err != nil {
		t.Fatalf("Record desactive = %v, want nil", err)
	}
	if err := tracker.Clear(ctx, "cible@test"); err != nil {
		t.Fatalf("Clear desactive = %v, want nil", err)
	}
	removed, err := tracker.Purge(ctx, time.Now())
	if err != nil || removed != 0 {
		t.Fatalf("Purge desactive = (%d, %v), want (0, nil)", removed, err)
	}
}

func TestAqsCovEveryKnownSocialProviderBuilds(t *testing.T) {
	socials := []auth.Social{
		{Name: "google", Key: "k", Secret: "s", Callback: "https://t.test/g"},
		{Name: "github", Key: "k", Secret: "s", Callback: "https://t.test/gh"},
		{Name: "gitlab", Key: "k", Secret: "s", Callback: "https://t.test/gl"},
		{Name: "microsoftonline", Key: "k", Secret: "s", Callback: "https://t.test/ms"},
		{Name: "facebook", Key: "k", Secret: "s", Callback: "https://t.test/fb"},
		{Name: "discord", Key: "k", Secret: "s", Callback: "https://t.test/dc"},
		{Name: "linkedin", Key: "k", Secret: "s", Callback: "https://t.test/li", Scopes: []string{"r_liteprofile"}},
	}

	names, err := auth.UseProviders(socials)
	if err != nil {
		t.Fatalf("UseProviders: %v", err)
	}
	if len(names) != len(socials) {
		t.Fatalf("providers = %v, want %d entrees", names, len(socials))
	}

	if _, err := auth.UseProviders(nil); err != nil {
		t.Fatalf("remise a zero: %v", err)
	}
}
