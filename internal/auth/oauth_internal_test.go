package auth

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/markbates/goth"
	"github.com/markbates/goth/gothic"
	"github.com/markbates/goth/providers/faux"
)

func oauthApp(t *testing.T) (*fiber.App, *error) {
	t.Helper()
	goth.ClearProviders()
	goth.UseProviders(&faux.Provider{})
	t.Cleanup(goth.ClearProviders)

	var completeErr error
	app := fiber.New()
	app.Get("/auth/:provider", func(c fiber.Ctx) error {
		return BeginOAuth(c, c.Params("provider"))
	})
	app.Get("/auth/:provider/callback", func(c fiber.Ctx) error {
		user, err := CompleteOAuth(c, c.Params("provider"))
		completeErr = err
		if err != nil {
			return c.SendStatus(fiber.StatusUnauthorized)
		}
		return c.SendString(user.Provider + ":" + user.AccessToken)
	})
	return app, &completeErr
}

func send(t *testing.T, app *fiber.App, target string, cookies ...*http.Cookie) *http.Response {
	t.Helper()
	req := httptest.NewRequest(fiber.MethodGet, target, nil)
	for _, cookie := range cookies {
		req.AddCookie(cookie)
	}
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func begin(t *testing.T, app *fiber.App) (string, *http.Cookie) {
	t.Helper()
	resp := send(t, app, "/auth/faux")
	if resp.StatusCode != fiber.StatusTemporaryRedirect {
		t.Fatalf("begin status = %d, want 307", resp.StatusCode)
	}

	location, err := url.Parse(resp.Header.Get(fiber.HeaderLocation))
	if err != nil {
		t.Fatal(err)
	}
	state := location.Query().Get("state")
	if len(state) < 64 {
		t.Fatalf("state %q is too short to be unguessable", state)
	}

	for _, cookie := range resp.Cookies() {
		if cookie.Name == gothic.SessionName {
			if !cookie.HttpOnly {
				t.Error("the oauth session cookie must be HttpOnly")
			}
			return state, cookie
		}
	}
	t.Fatal("begin set no oauth session cookie")
	return "", nil
}

func TestOAuthRoundTripCompletesWithTheSameState(t *testing.T) {
	app, _ := oauthApp(t)
	state, cookie := begin(t, app)

	resp := send(t, app, "/auth/faux/callback?state="+url.QueryEscape(state), cookie)
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != fiber.StatusOK || string(body) != "faux:access" {
		t.Fatalf("callback = %d %q, want 200 faux:access", resp.StatusCode, body)
	}

	// The session ends with the round trip: replaying the callback fails.
	resp = send(t, app, "/auth/faux/callback?state="+url.QueryEscape(state), cookie)
	if resp.StatusCode != fiber.StatusUnauthorized {
		t.Fatalf("replayed callback = %d, want 401", resp.StatusCode)
	}
}

func TestOAuthCallbackRefusesAnotherState(t *testing.T) {
	app, completeErr := oauthApp(t)
	_, cookie := begin(t, app)

	resp := send(t, app, "/auth/faux/callback?state=forged", cookie)
	if resp.StatusCode != fiber.StatusUnauthorized {
		t.Fatalf("callback with a forged state = %d, want 401", resp.StatusCode)
	}
	if !errors.Is(*completeErr, ErrStateMismatch) {
		t.Fatalf("error = %v, want ErrStateMismatch", *completeErr)
	}
}

func TestOAuthCallbackWithoutTheSessionFails(t *testing.T) {
	app, completeErr := oauthApp(t)
	state, _ := begin(t, app)

	resp := send(t, app, "/auth/faux/callback?state="+url.QueryEscape(state))
	if resp.StatusCode != fiber.StatusUnauthorized {
		t.Fatalf("callback from another browser = %d, want 401", resp.StatusCode)
	}
	if !errors.Is(*completeErr, ErrNoOAuthSession) {
		t.Fatalf("error = %v, want ErrNoOAuthSession", *completeErr)
	}
}

func TestOAuthCallbackWithACorruptSessionFails(t *testing.T) {
	app, completeErr := oauthApp(t)
	_, cookie := begin(t, app)

	app.Get("/corrupt", func(c fiber.Ctx) error {
		sess, err := oauthSessions.Get(c)
		if err != nil {
			return err
		}
		defer sess.Release()
		sess.Set("faux", "not gzip")
		return sess.Save()
	})
	send(t, app, "/corrupt", cookie)

	resp := send(t, app, "/auth/faux/callback?state=x", cookie)
	if resp.StatusCode != fiber.StatusUnauthorized || !errors.Is(*completeErr, ErrNoOAuthSession) {
		t.Fatalf("corrupt session = %d %v, want 401 ErrNoOAuthSession", resp.StatusCode, *completeErr)
	}
}

func TestOAuthKeepsAStateTheCallerPassed(t *testing.T) {
	app, _ := oauthApp(t)

	resp := send(t, app, "/auth/faux?state=chosen")
	if !strings.Contains(resp.Header.Get(fiber.HeaderLocation), "state=chosen") {
		t.Fatalf("location = %q, want the caller's state", resp.Header.Get(fiber.HeaderLocation))
	}
}

func TestOAuthUnknownProvider(t *testing.T) {
	app, completeErr := oauthApp(t)

	if resp := send(t, app, "/auth/nobody"); resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("begin on an unknown provider = %d, want 400", resp.StatusCode)
	}
	if resp := send(t, app, "/auth/nobody/callback"); resp.StatusCode != fiber.StatusUnauthorized || *completeErr == nil {
		t.Fatalf("callback on an unknown provider = %d %v, want 401 and an error", resp.StatusCode, *completeErr)
	}
}
