package integration

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
)

const password = "correct-horse-battery"

// browser switches the server's client to the cookie transport and signs up.
func browser(t *testing.T, s *server, email string) response {
	t.Helper()
	s.cookies = true
	s.csrf = true
	s.token = ""

	answer := s.post("/api/auth/register", map[string]any{
		"name": "Browser", "email": email, "password": password,
	})
	if answer.status != http.StatusCreated {
		t.Fatalf("register: status = %d — %s", answer.status, answer.raw)
	}
	return answer
}

func setCookie(r response, name string) *http.Cookie {
	for _, raw := range r.header.Values("Set-Cookie") {
		cookie, err := http.ParseSetCookie(raw)
		if err == nil && cookie.Name == name {
			return cookie
		}
	}
	return nil
}

func TestCookieLoginSetsTheThreeCookiesAndNoTokenInTheBody(t *testing.T) {
	s := boot(t)
	browser(t, s, "cookies@integration.test")
	s.jar = nil

	answer := s.post("/api/auth/login", map[string]any{"email": "cookies@integration.test", "password": password})
	if answer.status != http.StatusOK {
		t.Fatalf("login: status = %d — %s", answer.status, answer.raw)
	}

	if answer.body["token_type"] != "Cookie" {
		t.Errorf("token_type = %v, want Cookie", answer.body["token_type"])
	}
	if _, ok := answer.body["expires_in"].(float64); !ok {
		t.Error("expires_in is missing")
	}
	user, _ := answer.body["user"].(map[string]any)
	if user["email"] != "cookies@integration.test" {
		t.Errorf("user = %v", answer.body["user"])
	}
	if strings.Contains(answer.raw, "access_token") || strings.Contains(answer.raw, "refresh_token") {
		t.Errorf("a token leaked into the body: %s", answer.raw)
	}

	access := setCookie(answer, "omega_access")
	if access == nil || !access.HttpOnly || access.Path != "/" || access.SameSite != http.SameSiteLaxMode || access.MaxAge <= 0 {
		t.Errorf("omega_access = %+v", access)
	}
	refresh := setCookie(answer, "omega_refresh")
	if refresh == nil || !refresh.HttpOnly || refresh.Path != "/api/auth" || refresh.SameSite != http.SameSiteStrictMode {
		t.Errorf("omega_refresh = %+v", refresh)
	}
	csrf := setCookie(answer, "omega_csrf")
	if csrf == nil || csrf.HttpOnly || csrf.Path != "/" || len(csrf.Value) < 40 {
		t.Errorf("omega_csrf = %+v", csrf)
	}
	if access != nil && access.Secure {
		t.Error("in testing the cookies must not require HTTPS")
	}

	if got := answer.header.Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control on login = %q, want no-store", got)
	}
}

func TestCookieSessionAuthenticatesSafeRequestsWithoutCSRF(t *testing.T) {
	s := boot(t)
	browser(t, s, "safe@integration.test")
	s.csrf = false

	answer := s.get("/api/auth/me")
	if answer.status != http.StatusOK {
		t.Fatalf("/me with the cookie: status = %d — %s", answer.status, answer.raw)
	}
	if got := answer.header.Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
}

func TestCookieUnsafeRequestWithoutCSRFIsRefused(t *testing.T) {
	s := boot(t)
	browser(t, s, "csrf@integration.test")

	s.csrf = false
	for _, path := range []string{"/api/auth/logout-all", "/api/auth/refresh", "/api/auth/logout"} {
		answer := s.post(path, nil)
		if answer.status != http.StatusForbidden || answer.body["error"] != "CSRF token missing or invalid." {
			t.Errorf("%s without the header: %d %s", path, answer.status, answer.raw)
		}
	}

	// A header that does not match the cookie is no better.
	request := httptest.NewRequest(http.MethodPut, "/api/settings/currency", strings.NewReader(`{"currency":"USD"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CSRF-Token", "forged")
	for name, value := range s.jar {
		request.AddCookie(&http.Cookie{Name: name, Value: value})
	}
	result, err := s.app.Fiber.Test(request, fiber.TestConfig{Timeout: 0, FailOnTimeout: false})
	if err != nil {
		t.Fatal(err)
	}
	if result.StatusCode != http.StatusForbidden {
		t.Errorf("forged header: status = %d, want 403", result.StatusCode)
	}

	s.csrf = true
	if answer := s.put("/api/settings/currency", map[string]any{"currency": "EUR"}); answer.status == http.StatusForbidden {
		t.Errorf("with the header: status = %d — %s", answer.status, answer.raw)
	}
}

func TestCookieRefreshRotatesFromTheCookie(t *testing.T) {
	s := boot(t)
	browser(t, s, "refresh@integration.test")
	before := s.jar["omega_refresh"]

	answer := s.post("/api/auth/refresh", nil)
	if answer.status != http.StatusOK {
		t.Fatalf("refresh: status = %d — %s", answer.status, answer.raw)
	}
	if answer.body["token_type"] != "Cookie" || strings.Contains(answer.raw, "refresh_token") {
		t.Errorf("cookie-mode refresh body = %s", answer.raw)
	}
	if s.jar["omega_refresh"] == before || s.jar["omega_refresh"] == "" {
		t.Error("the refresh cookie was not rotated")
	}
	if s.get("/api/auth/me").status != http.StatusOK {
		t.Error("the new access cookie does not authenticate")
	}
}

func TestCookieLogoutClearsTheCookiesAndRevokes(t *testing.T) {
	s := boot(t)
	browser(t, s, "logout@integration.test")
	spent := s.jar["omega_refresh"]

	answer := s.post("/api/auth/logout", nil)
	if answer.status != http.StatusNoContent {
		t.Fatalf("logout: status = %d — %s", answer.status, answer.raw)
	}
	for _, name := range []string{"omega_access", "omega_refresh", "omega_csrf"} {
		cookie := setCookie(answer, name)
		if cookie == nil || cookie.MaxAge >= 0 {
			t.Errorf("%s was not expired: %+v", name, cookie)
		}
	}

	// The revoked token cannot be replayed, from a cookie or from a body.
	s.cookies = false
	if replay := s.post("/api/auth/refresh", map[string]any{"refresh_token": spent}); replay.status != http.StatusUnauthorized {
		t.Errorf("replay after logout: status = %d", replay.status)
	}
}

func TestLogoutWithAnExpiredRefreshCookieStillClears(t *testing.T) {
	s := boot(t)
	browser(t, s, "stale@integration.test")
	delete(s.jar, "omega_refresh")
	delete(s.jar, "omega_access")

	if answer := s.post("/api/auth/logout", nil); answer.status != http.StatusNoContent {
		t.Fatalf("status = %d — %s", answer.status, answer.raw)
	}
}

func TestCSRFEndpointSetsAndKeepsTheToken(t *testing.T) {
	s := boot(t)
	s.cookies = true

	first := s.get("/api/auth/csrf")
	if first.status != http.StatusNoContent {
		t.Fatalf("status = %d", first.status)
	}
	token := s.jar["omega_csrf"]
	if token == "" {
		t.Fatal("no omega_csrf cookie")
	}

	if s.get("/api/auth/csrf"); s.jar["omega_csrf"] != token {
		t.Error("a valid token was rotated")
	}

	s.jar["omega_csrf"] = "not-a-token"
	if s.get("/api/auth/csrf"); s.jar["omega_csrf"] == "not-a-token" {
		t.Error("a malformed token was kept")
	}
}

func TestBearerModeIsUnchangedWithCookiesOn(t *testing.T) {
	s := boot(t)
	answer := s.signUp("bearer@integration.test")
	if answer.tokens()["access_token"] == nil || answer.data()["email"] == nil {
		t.Fatalf("bearer body = %s", answer.raw)
	}
	if setCookie(answer, "omega_access") != nil {
		t.Error("bearer mode must not set cookies")
	}

	// A bearer request is never asked for a CSRF token, stale cookies or not.
	request := httptest.NewRequest(http.MethodPost, "/api/auth/logout-all", nil)
	request.Header.Set("Authorization", "Bearer "+s.token)
	request.AddCookie(&http.Cookie{Name: "omega_access", Value: "stale"})
	result, err := s.app.Fiber.Test(request, fiber.TestConfig{Timeout: 0, FailOnTimeout: false})
	if err != nil {
		t.Fatal(err)
	}
	if result.StatusCode != http.StatusNoContent {
		t.Errorf("bearer logout-all: status = %d, want 204", result.StatusCode)
	}
}

func TestBearerModeWhenCookiesAreDisabled(t *testing.T) {
	s := bootWith(t, map[string]string{"AUTH_COOKIES": "false"})
	s.cookies = true // no X-Omega-Auth header: the server decides alone

	answer := s.post("/api/auth/register", map[string]any{
		"name": "NoCookie", "email": "nocookie@integration.test", "password": password,
	})
	if answer.tokens()["access_token"] == nil {
		t.Fatalf("with cookies off the tokens belong in the body: %s", answer.raw)
	}
	if len(s.jar) != 0 {
		t.Errorf("cookies set while disabled: %v", s.jar)
	}
}

func TestWebSocketUpgradeChecksTheOrigin(t *testing.T) {
	s := bootWith(t, map[string]string{"CORS_ORIGINS": "https://app.example.test"})
	browser(t, s, "socket@integration.test")

	upgrade := func(origin string) int {
		request := httptest.NewRequest(http.MethodGet, "/api/ws", nil)
		request.Header.Set("Origin", origin)
		for name, value := range s.jar {
			request.AddCookie(&http.Cookie{Name: name, Value: value})
		}
		result, err := s.app.Fiber.Test(request, fiber.TestConfig{Timeout: 0, FailOnTimeout: false})
		if err != nil {
			t.Fatal(err)
		}
		return result.StatusCode
	}

	if status := upgrade("https://evil.example.test"); status != http.StatusForbidden {
		t.Errorf("foreign origin: status = %d, want 403", status)
	}
	// An allowed origin passes the check and the cookie authenticates; what
	// stops it then is only that this test is not a real WebSocket handshake.
	if status := upgrade("https://app.example.test"); status != http.StatusUpgradeRequired {
		t.Errorf("allowed origin: status = %d, want 426", status)
	}
}
