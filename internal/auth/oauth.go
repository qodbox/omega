package auth

import (
	"bytes"
	"compress/gzip"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/extractors"
	"github.com/gofiber/fiber/v3/middleware/session"
	"github.com/markbates/goth"
	"github.com/markbates/goth/gothic"
)

// The OAuth round trip used to go through shareed2k/goth_fiber, which only
// speaks Fiber v2. This is the same glue around markbates/goth, kept to what
// the social controller needs: begin, then complete with the state checked.

var (
	ErrStateMismatch  = errors.New("auth: oauth state token mismatch")
	ErrNoOAuthSession = errors.New("auth: no oauth session matches this request")
)

// oauthSessionTTL matches the lifetime the goth_fiber session store had.
const oauthSessionTTL = 24 * time.Hour

var oauthSessions = session.NewStore(session.Config{
	Extractor:      extractors.FromCookie(gothic.SessionName),
	CookieHTTPOnly: true,
	IdleTimeout:    oauthSessionTTL,
})

// BeginOAuth sends the browser to the provider's consent page, after storing
// the provider session (and the state it was started with) server side.
func BeginOAuth(c fiber.Ctx, providerName string) error {
	target, err := OAuthURL(c, providerName)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).SendString(err.Error())
	}
	return c.Redirect().Status(fiber.StatusTemporaryRedirect).To(target)
}

// OAuthURL starts the provider session and returns the URL to send the user to.
func OAuthURL(c fiber.Ctx, providerName string) (string, error) {
	provider, err := goth.GetProvider(providerName)
	if err != nil {
		return "", err
	}

	state, err := oauthState(c)
	if err != nil {
		return "", err
	}

	sess, err := provider.BeginAuth(state)
	if err != nil {
		return "", err
	}

	target, err := sess.GetAuthURL()
	if err != nil {
		return "", err
	}

	if err := storeOAuth(c, providerName, sess.Marshal()); err != nil {
		return "", err
	}
	return target, nil
}

// CompleteOAuth finishes the round trip on the callback: it reloads the
// provider session, refuses a state that differs from the one the round trip
// started with, then fetches the user. The session is always ended.
func CompleteOAuth(c fiber.Ctx, providerName string) (goth.User, error) {
	provider, err := goth.GetProvider(providerName)
	if err != nil {
		return goth.User{}, err
	}

	value, err := loadOAuth(c, providerName)
	if err != nil {
		return goth.User{}, err
	}
	defer endOAuth(c)

	sess, err := provider.UnmarshalSession(value)
	if err != nil {
		return goth.User{}, err
	}

	if err := validateState(c, sess); err != nil {
		return goth.User{}, err
	}

	if user, err := provider.FetchUser(sess); err == nil {
		return user, nil
	}

	if _, err := sess.Authorize(provider, oauthParams{c: c}); err != nil {
		return goth.User{}, err
	}

	if err := storeOAuth(c, providerName, sess.Marshal()); err != nil {
		return goth.User{}, err
	}
	return provider.FetchUser(sess)
}

type oauthParams struct{ c fiber.Ctx }

func (p oauthParams) Get(key string) string { return p.c.Query(key) }

// oauthState keeps a state the caller passed and otherwise draws an
// unguessable one, so the callback can be tied to this browser.
func oauthState(c fiber.Ctx) (string, error) {
	if state := c.Query("state"); state != "" {
		return state, nil
	}

	nonce := make([]byte, 64)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("auth: no randomness for the oauth state: %w", err)
	}
	return base64.URLEncoding.EncodeToString(nonce), nil
}

func validateState(c fiber.Ctx, sess goth.Session) error {
	raw, err := sess.GetAuthURL()
	if err != nil {
		return err
	}

	authURL, err := url.Parse(raw)
	if err != nil {
		return err
	}

	original := authURL.Query().Get("state")
	if original != "" && original != c.Query("state") {
		return ErrStateMismatch
	}
	return nil
}

func storeOAuth(c fiber.Ctx, key, value string) error {
	sess, err := oauthSessions.Get(c)
	if err != nil {
		return err
	}
	defer sess.Release()

	var packed bytes.Buffer
	gz := gzip.NewWriter(&packed)
	if _, err := gz.Write([]byte(value)); err != nil {
		return err
	}
	if err := gz.Close(); err != nil {
		return err
	}

	sess.Set(key, packed.String())
	return sess.Save()
}

func loadOAuth(c fiber.Ctx, key string) (string, error) {
	sess, err := oauthSessions.Get(c)
	if err != nil {
		return "", err
	}
	defer sess.Release()

	raw, ok := sess.Get(key).(string)
	if !ok {
		return "", ErrNoOAuthSession
	}

	reader, err := gzip.NewReader(strings.NewReader(raw))
	if err != nil {
		return "", ErrNoOAuthSession
	}
	defer reader.Close()

	value, err := io.ReadAll(reader)
	if err != nil {
		return "", ErrNoOAuthSession
	}
	return string(value), nil
}

func endOAuth(c fiber.Ctx) {
	sess, err := oauthSessions.Get(c)
	if err != nil {
		return
	}
	defer sess.Release()
	_ = sess.Destroy()
}
