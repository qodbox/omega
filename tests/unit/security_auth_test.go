package unit

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"

	"omega/internal/auth"
	"omega/internal/validation"
)

// familyStore is an in-memory FamilyStore.
type familyStore struct {
	rows map[string]*familyRow
}

type familyRow struct {
	subject, family string
	used            time.Time
}

func newFamilyStore() *familyStore { return &familyStore{rows: map[string]*familyRow{}} }

func (f *familyStore) Save(id, subject string, expires time.Time) error {
	return f.SaveFamily(id, subject, id, expires)
}
func (f *familyStore) SaveFamily(id, subject, family string, _ time.Time) error {
	f.rows[id] = &familyRow{subject: subject, family: family}
	return nil
}
func (f *familyStore) Exists(id string) (bool, error) {
	row, ok := f.rows[id]
	return ok && row.used.IsZero(), nil
}
func (f *familyStore) Consume(id string, now time.Time) (string, time.Time, error) {
	row, ok := f.rows[id]
	if !ok {
		return "", time.Time{}, auth.ErrUnknownToken
	}
	if !row.used.IsZero() {
		return row.family, row.used, auth.ErrReused
	}
	row.used = now
	return row.family, time.Time{}, nil
}
func (f *familyStore) Revoke(id string) error { delete(f.rows, id); return nil }
func (f *familyStore) RevokeFamily(family string) error {
	for id, row := range f.rows {
		if row.family == family {
			delete(f.rows, id)
		}
	}
	return nil
}
func (f *familyStore) RevokeSubject(subject string) error {
	for id, row := range f.rows {
		if row.subject == subject {
			delete(f.rows, id)
		}
	}
	return nil
}
func (f *familyStore) Purge(time.Time) error { return nil }

func TestRotationKeepsTheFamilyAndReuseRevokesIt(t *testing.T) {
	store := newFamilyStore()
	var reported string
	guard := auth.New(auth.Config{
		Secret: secret, Store: store,
		OnReuse: func(subject, family string) { reported = subject + "/" + family },
	})

	first, err := guard.Issue("7", "user")
	if err != nil {
		t.Fatal(err)
	}
	claims, _ := auth.Verify([]byte(secret), first.RefreshToken, auth.KindRefresh)
	family := claims.Family
	if family == "" {
		t.Fatal("a fresh login has no family")
	}

	second, err := guard.Rotate(first.RefreshToken, "user")
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}
	next, _ := auth.Verify([]byte(secret), second.RefreshToken, auth.KindRefresh)
	if next.Family != family {
		t.Errorf("family changed on rotation: %q -> %q", family, next.Family)
	}

	if _, err := guard.Rotate(first.RefreshToken, "user"); !errors.Is(err, auth.ErrReused) {
		t.Fatalf("replay = %v, want ErrReused", err)
	}
	if reported != "7/"+family {
		t.Errorf("OnReuse got %q", reported)
	}
	if _, err := guard.Rotate(second.RefreshToken, "user"); err == nil {
		t.Error("the family survived the reuse")
	}
}

func TestReuseInsideTheGraceIsRefusedQuietly(t *testing.T) {
	store := newFamilyStore()
	called := false
	guard := auth.New(auth.Config{
		Secret: secret, Store: store, ReuseGrace: time.Minute,
		OnReuse: func(string, string) { called = true },
	})

	first, _ := guard.Issue("7", "user")
	second, _ := guard.Rotate(first.RefreshToken, "user")

	if _, err := guard.Rotate(first.RefreshToken, "user"); !errors.Is(err, auth.ErrSignature) {
		t.Fatalf("replay in grace = %v, want ErrSignature", err)
	}
	if called {
		t.Error("a replay inside the grace was reported as theft")
	}
	if _, err := guard.RotateGrant(second.RefreshToken, "user", 0); err != nil {
		t.Errorf("the family must survive a replay inside the grace: %v", err)
	}
}

func TestRevokeRetiresTheWholeFamily(t *testing.T) {
	store := newFamilyStore()
	guard := auth.New(auth.Config{Secret: secret, Store: store})

	first, _ := guard.Issue("7", "user")
	second, _ := guard.Rotate(first.RefreshToken, "user")
	if err := guard.Revoke(second.RefreshToken); err != nil {
		t.Fatal(err)
	}
	if len(store.rows) != 0 {
		t.Errorf("%d tokens of the family remain", len(store.rows))
	}
	if _, err := guard.Rotate(second.RefreshToken, "user"); !errors.Is(err, auth.ErrSignature) {
		t.Errorf("rotate after revoke = %v", err)
	}
}

type versionedUser struct {
	version uint64
	role    string
}

func (u *versionedUser) AuthVersion() uint64 { return u.version }
func (u *versionedUser) AuthRole() string    { return u.role }

func guardedApp(guard *auth.Guard) *fiber.App {
	app := fiber.New()
	app.Get("/me", guard.Required(), func(c fiber.Ctx) error {
		claims, _ := auth.ClaimsOf(c)
		return c.SendString(claims.Role + ":" + auth.Transport(c))
	})
	app.Post("/act", guard.Required(), func(c fiber.Ctx) error { return c.SendStatus(fiber.StatusNoContent) })
	app.Post("/maybe", guard.Optional(), func(c fiber.Ctx) error {
		if _, ok := auth.ClaimsOf(c); ok {
			return c.SendString("signed-in")
		}
		return c.SendString("anonymous")
	})
	app.Get("/csrf", func(c fiber.Ctx) error {
		token, err := guard.EnsureCSRF(c)
		if err != nil {
			return err
		}
		return c.SendString(token)
	})
	app.Post("/session", func(c fiber.Ctx) error {
		if !guard.CookieMode(c) {
			return c.SendString("bearer")
		}
		tokens, _ := guard.Issue("7", "user")
		return guard.SetSession(c, tokens)
	})
	app.Post("/clear", func(c fiber.Ctx) error {
		guard.ClearSession(c)
		return nil
	})
	app.Post("/guarded", guard.CSRF(), func(c fiber.Ctx) error { return c.SendString(guard.RefreshToken(c)) })
	return app
}

func call(t *testing.T, app *fiber.App, request *http.Request) (int, string, *http.Response) {
	t.Helper()
	result, err := app.Test(request, fiber.TestConfig{Timeout: 0, FailOnTimeout: false})
	if err != nil {
		t.Fatal(err)
	}
	raw := make([]byte, 512)
	n, _ := result.Body.Read(raw)
	return result.StatusCode, string(raw[:n]), result
}

func TestStaleTokenVersionIsRefusedAndTheRoleIsReadFromTheUser(t *testing.T) {
	user := &versionedUser{version: 3, role: "admin"}
	guard := auth.New(auth.Config{
		Secret: secret,
		Loader: func(string) (any, error) { return user, nil },
	})

	tokens, err := guard.IssueGrant(auth.Grant{Subject: "7", Role: "admin", Version: 3})
	if err != nil {
		t.Fatal(err)
	}
	app := guardedApp(guard)

	request := httptest.NewRequest(http.MethodGet, "/me", nil)
	request.Header.Set("Authorization", "Bearer "+tokens.AccessToken)
	if status, body, _ := call(t, app, request); status != http.StatusOK || body != "admin:bearer" {
		t.Fatalf("current version: %d %q", status, body)
	}

	user.role = "user"
	request = httptest.NewRequest(http.MethodGet, "/me", nil)
	request.Header.Set("Authorization", "Bearer "+tokens.AccessToken)
	if _, body, _ := call(t, app, request); body != "user:bearer" {
		t.Errorf("the demotion was not applied: %q", body)
	}

	user.version = 4
	request = httptest.NewRequest(http.MethodGet, "/me", nil)
	request.Header.Set("Authorization", "Bearer "+tokens.AccessToken)
	if status, _, _ := call(t, app, request); status != http.StatusUnauthorized {
		t.Errorf("stale version: status = %d, want 401", status)
	}

	// Issue reads the version through the Loader.
	fresh, _ := guard.Issue("7", "user")
	claims, _ := auth.Verify([]byte(secret), fresh.AccessToken, auth.KindAccess)
	if claims.Version != 4 {
		t.Errorf("Issue signed version %d, want 4", claims.Version)
	}
	rotated, err := guard.Rotate(fresh.RefreshToken, "user")
	if err != nil {
		t.Fatal(err)
	}
	if claims, _ := auth.Verify([]byte(secret), rotated.AccessToken, auth.KindAccess); claims.Version != 4 {
		t.Errorf("Rotate signed version %d, want 4", claims.Version)
	}
}

func TestAMissingAccountIsRefused(t *testing.T) {
	guard := auth.New(auth.Config{
		Secret: secret,
		Loader: func(string) (any, error) { return nil, errors.New("gone") },
	})
	tokens, _ := guard.Issue("7", "user")

	request := httptest.NewRequest(http.MethodGet, "/me", nil)
	request.Header.Set("Authorization", "Bearer "+tokens.AccessToken)
	if status, _, _ := call(t, guardedApp(guard), request); status != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", status)
	}
}

func cookieGuard() *auth.Guard {
	return auth.New(auth.Config{
		Secret: secret, AccessTTL: time.Minute, RefreshTTL: time.Hour,
		Cookies: auth.Cookies{Enabled: true, Domain: "example.test", Secure: true, SameSite: "strict"},
	})
}

func TestCookieTransportAndCSRF(t *testing.T) {
	guard := cookieGuard()
	app := guardedApp(guard)
	tokens, _ := guard.Issue("7", "user")

	withCookies := func(method, path, csrf string) *http.Request {
		request := httptest.NewRequest(method, path, nil)
		request.AddCookie(&http.Cookie{Name: auth.CookieAccess, Value: tokens.AccessToken})
		request.AddCookie(&http.Cookie{Name: auth.CookieRefresh, Value: tokens.RefreshToken})
		request.AddCookie(&http.Cookie{Name: auth.CookieCSRF, Value: "the-csrf-token"})
		if csrf != "" {
			request.Header.Set(auth.HeaderCSRF, csrf)
		}
		return request
	}

	if status, body, _ := call(t, app, withCookies(http.MethodGet, "/me", "")); status != http.StatusOK || body != "user:cookie" {
		t.Errorf("GET with the cookie: %d %q", status, body)
	}
	if status, body, _ := call(t, app, withCookies(http.MethodPost, "/act", "")); status != http.StatusForbidden || !strings.Contains(body, auth.CSRFMessage) {
		t.Errorf("POST without CSRF: %d %q", status, body)
	}
	if status, _, _ := call(t, app, withCookies(http.MethodPost, "/act", "the-csrf-tokeN")); status != http.StatusForbidden {
		t.Errorf("POST with a wrong CSRF: %d", status)
	}
	if status, _, _ := call(t, app, withCookies(http.MethodPost, "/act", "the-csrf-token")); status != http.StatusNoContent {
		t.Errorf("POST with CSRF: %d", status)
	}
	if _, body, _ := call(t, app, withCookies(http.MethodPost, "/maybe", "")); body != "anonymous" {
		t.Errorf("Optional must ignore a cookie without CSRF: %q", body)
	}
	if status, _, _ := call(t, app, withCookies(http.MethodPost, "/guarded", "")); status != http.StatusForbidden {
		t.Errorf("CSRF middleware without header: %d", status)
	}
	if status, body, _ := call(t, app, withCookies(http.MethodPost, "/guarded", "the-csrf-token")); status != http.StatusOK || body != tokens.RefreshToken {
		t.Errorf("CSRF middleware with header: %d", status)
	}

	// A bearer request is never asked for a CSRF token.
	request := withCookies(http.MethodPost, "/guarded", "")
	request.Header.Set("Authorization", "Bearer "+tokens.AccessToken)
	if status, _, _ := call(t, app, request); status != http.StatusOK {
		t.Errorf("bearer through the CSRF middleware: %d", status)
	}
}

func TestSessionCookiesCarryTheirAttributes(t *testing.T) {
	guard := cookieGuard()
	app := guardedApp(guard)

	_, _, result := call(t, app, httptest.NewRequest(http.MethodPost, "/session", nil))
	found := map[string]*http.Cookie{}
	for _, cookie := range result.Cookies() {
		found[cookie.Name] = cookie
	}

	access, refresh, csrf := found[auth.CookieAccess], found[auth.CookieRefresh], found[auth.CookieCSRF]
	if access == nil || !access.HttpOnly || !access.Secure || access.Domain != "example.test" || access.MaxAge != 60 || access.SameSite != http.SameSiteStrictMode {
		t.Errorf("access = %+v", access)
	}
	if refresh == nil || refresh.Path != auth.RefreshPath || refresh.MaxAge != 3600 || refresh.SameSite != http.SameSiteStrictMode {
		t.Errorf("refresh = %+v", refresh)
	}
	if csrf == nil || csrf.HttpOnly {
		t.Errorf("csrf = %+v", csrf)
	}

	bearer := httptest.NewRequest(http.MethodPost, "/session", nil)
	bearer.Header.Set(auth.HeaderMode, "Bearer")
	if _, body, _ := call(t, app, bearer); body != "bearer" {
		t.Errorf("X-Omega-Auth: Bearer ignored: %q", body)
	}

	_, _, cleared := call(t, app, httptest.NewRequest(http.MethodPost, "/clear", nil))
	if len(cleared.Cookies()) != 3 {
		t.Fatalf("cleared %d cookies, want 3", len(cleared.Cookies()))
	}
	for _, cookie := range cleared.Cookies() {
		if cookie.MaxAge >= 0 {
			t.Errorf("%s not expired", cookie.Name)
		}
	}
}

func TestSameSiteNoneAndLaxSpellings(t *testing.T) {
	for setting, want := range map[string]http.SameSite{"None": http.SameSiteNoneMode, "": http.SameSiteLaxMode} {
		guard := auth.New(auth.Config{Secret: secret, Cookies: auth.Cookies{Enabled: true, Secure: true, SameSite: setting}})
		_, _, result := call(t, guardedApp(guard), httptest.NewRequest(http.MethodGet, "/csrf", nil))
		if cookies := result.Cookies(); len(cookies) != 1 || cookies[0].SameSite != want {
			t.Errorf("%q: %+v", setting, cookies)
		}
	}
}

func TestCookiesAreIgnoredWhenTheTransportIsOff(t *testing.T) {
	guard := auth.New(auth.Config{Secret: secret})
	tokens, _ := guard.Issue("7", "user")

	request := httptest.NewRequest(http.MethodGet, "/me", nil)
	request.AddCookie(&http.Cookie{Name: auth.CookieAccess, Value: tokens.AccessToken})
	if status, _, _ := call(t, guardedApp(guard), request); status != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", status)
	}
	if guard.CookiesEnabled() {
		t.Error("enabled by default")
	}
}

func TestUnsafeMethods(t *testing.T) {
	for method, unsafe := range map[string]bool{"GET": false, "HEAD": false, "OPTIONS": false, "TRACE": false, "POST": true, "PUT": true, "PATCH": true, "DELETE": true} {
		if auth.Unsafe(method) != unsafe {
			t.Errorf("Unsafe(%s) = %v", method, !unsafe)
		}
	}
}

func TestPasswordsAreBoundedInBytes(t *testing.T) {
	type body struct {
		Password string `json:"password" validate:"maxbytes=4"`
	}
	if err := validation.Struct(&body{Password: "abcd"}); err != nil {
		t.Errorf("4 bytes: %v", err)
	}
	err := validation.Struct(&body{Password: "ééé"})
	var invalid *validation.Error
	if !errors.As(err, &invalid) || !strings.Contains(invalid.Fields["password"], "bytes") {
		t.Errorf("6 bytes: %v", err)
	}
}
