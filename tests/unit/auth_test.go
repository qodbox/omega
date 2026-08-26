package unit

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"omega/internal/auth"
)

const secret = "a-test-secret-that-is-long-enough-to-be-plausible"

func newGuard(t *testing.T, store auth.Store) *auth.Guard {
	t.Helper()
	return auth.New(auth.Config{
		Secret:     secret,
		AccessTTL:  time.Minute,
		RefreshTTL: time.Hour,
		Store:      store,
	})
}

type memoryStore struct{ live map[string]string }

func newMemoryStore() *memoryStore { return &memoryStore{live: map[string]string{}} }

func (m *memoryStore) Save(id, subject string, _ time.Time) error {
	m.live[id] = subject
	return nil
}
func (m *memoryStore) Exists(id string) (bool, error) { _, ok := m.live[id]; return ok, nil }
func (m *memoryStore) Revoke(id string) error         { delete(m.live, id); return nil }
func (m *memoryStore) RevokeSubject(subject string) error {
	for id, held := range m.live {
		if held == subject {
			delete(m.live, id)
		}
	}
	return nil
}
func (m *memoryStore) Purge(time.Time) error { return nil }

func TestIssuedAccessTokenCarriesSubjectAndRole(t *testing.T) {
	guard := newGuard(t, newMemoryStore())

	tokens, err := guard.Issue("42", "admin")
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if tokens.TokenType != "Bearer" {
		t.Errorf("token type = %q, want Bearer", tokens.TokenType)
	}
	if tokens.ExpiresIn != 60 {
		t.Errorf("expires_in = %d, want 60", tokens.ExpiresIn)
	}

	claims, err := auth.Verify([]byte(secret), tokens.AccessToken, auth.KindAccess)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if claims.Subject != "42" || claims.Role != "admin" {
		t.Errorf("claims = %+v, want subject 42 and role admin", claims)
	}
}

func TestAccessTokenIsRefusedAsRefreshToken(t *testing.T) {
	guard := newGuard(t, newMemoryStore())

	tokens, err := guard.Issue("42", "user")
	if err != nil {
		t.Fatalf("issue: %v", err)
	}

	if _, err := auth.Verify([]byte(secret), tokens.AccessToken, auth.KindRefresh); err != auth.ErrKind {
		t.Errorf("verify = %v, want ErrKind", err)
	}
}

func TestVerifyRejectsAnotherSecret(t *testing.T) {
	guard := newGuard(t, newMemoryStore())

	tokens, _ := guard.Issue("42", "user")

	if _, err := auth.Verify([]byte("a-different-secret-entirely"), tokens.AccessToken, auth.KindAccess); err != auth.ErrSignature {
		t.Errorf("verify = %v, want ErrSignature", err)
	}
}

func TestVerifyRejectsExpiredToken(t *testing.T) {
	past := time.Now().Add(-time.Hour)
	stale, err := auth.Sign([]byte(secret), auth.Claims{
		Kind: auth.KindAccess,
		Role: "user",
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "42",
			IssuedAt:  jwt.NewNumericDate(past),
			ExpiresAt: jwt.NewNumericDate(past.Add(time.Minute)),
		},
	})
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	if _, err := auth.Verify([]byte(secret), stale, auth.KindAccess); err != auth.ErrExpired {
		t.Errorf("verify = %v, want ErrExpired", err)
	}
}

func TestVerifyRejectsTokenWithoutExpiry(t *testing.T) {
	forever, err := auth.Sign([]byte(secret), auth.Claims{
		Kind:             auth.KindAccess,
		RegisteredClaims: jwt.RegisteredClaims{Subject: "42"},
	})
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	if _, err := auth.Verify([]byte(secret), forever, auth.KindAccess); err == nil {
		t.Error("a token with no expiry was accepted")
	}
}

func TestVerifyRejectsAlgNone(t *testing.T) {
	encode := func(raw string) string {
		return base64.RawURLEncoding.EncodeToString([]byte(raw))
	}
	forged := encode(`{"alg":"none","typ":"JWT"}`) + "." +
		encode(`{"sub":"1","typ":"access","role":"admin","exp":9999999999}`) + "."

	if _, err := auth.Verify([]byte(secret), forged, auth.KindAccess); err == nil {
		t.Fatal("a token signed with alg=none was accepted")
	}
}

func TestVerifyRejectsTamperedPayload(t *testing.T) {
	guard := newGuard(t, newMemoryStore())
	tokens, _ := guard.Issue("42", "user")

	parts := strings.Split(tokens.AccessToken, ".")
	if len(parts) != 3 {
		t.Fatalf("expected three segments, got %d", len(parts))
	}

	parts[1] = base64.RawURLEncoding.EncodeToString(
		[]byte(`{"sub":"42","typ":"access","role":"admin","exp":9999999999}`))

	if _, err := auth.Verify([]byte(secret), strings.Join(parts, "."), auth.KindAccess); err != auth.ErrSignature {
		t.Errorf("verify = %v, want ErrSignature", err)
	}
}

func TestRefreshTokenCannotBeReplayed(t *testing.T) {
	guard := newGuard(t, newMemoryStore())

	first, err := guard.Issue("42", "user")
	if err != nil {
		t.Fatalf("issue: %v", err)
	}

	if _, err := guard.Rotate(first.RefreshToken, "user"); err != nil {
		t.Fatalf("first rotate: %v", err)
	}
	if _, err := guard.Rotate(first.RefreshToken, "user"); err == nil {
		t.Fatal("the same refresh token was accepted twice")
	}
}

func TestRevokeAllEndsEverySession(t *testing.T) {
	guard := newGuard(t, newMemoryStore())

	phone, _ := guard.Issue("42", "user")
	laptop, _ := guard.Issue("42", "user")

	if err := guard.RevokeAll("42"); err != nil {
		t.Fatalf("revoke all: %v", err)
	}

	for name, tokens := range map[string]string{"phone": phone.RefreshToken, "laptop": laptop.RefreshToken} {
		if _, err := guard.Rotate(tokens, "user"); err == nil {
			t.Errorf("%s kept a usable refresh token after RevokeAll", name)
		}
	}
}

func TestRotateAdoptsTheNewRole(t *testing.T) {
	guard := newGuard(t, newMemoryStore())

	first, _ := guard.Issue("42", "admin")

	next, err := guard.Rotate(first.RefreshToken, "user")
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}

	claims, err := auth.Verify([]byte(secret), next.AccessToken, auth.KindAccess)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if claims.Role != "user" {
		t.Errorf("role = %q, want user", claims.Role)
	}
}
