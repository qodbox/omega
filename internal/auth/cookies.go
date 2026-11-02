package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
)

// The cookie transport. In cookie mode no token is ever readable by
// JavaScript: the access and refresh tokens travel in HttpOnly cookies, and a
// double-submit token (a readable cookie echoed in a header) stops a third
// party site from riding them.
const (
	CookieAccess  = "omega_access"
	CookieRefresh = "omega_refresh"
	CookieCSRF    = "omega_csrf"

	// HeaderCSRF carries the copy of the omega_csrf cookie on unsafe methods.
	HeaderCSRF = "X-CSRF-Token"

	// HeaderMode lets a client that holds its tokens itself (mobile, server to
	// server) ask for them in the body: "X-Omega-Auth: bearer".
	HeaderMode = "X-Omega-Auth"

	// RefreshPath scopes the refresh cookie: it is sent to the auth endpoints
	// and nowhere else.
	RefreshPath = "/api/auth"

	// LocalsTransport records how the request authenticated: "bearer" or
	// "cookie".
	LocalsTransport = "auth_transport"

	TransportBearer = "bearer"
	TransportCookie = "cookie"

	csrfBytes = 32
)

// ErrCSRF is a cookie-authenticated unsafe request without a matching
// X-CSRF-Token header.
var ErrCSRF = errors.New("auth: CSRF token missing or invalid")

// CSRFMessage is the error text a client recognises to retry with a fresh
// token. Part of the transport contract: do not reword it.
const CSRFMessage = "CSRF token missing or invalid."

// Cookies configures the cookie transport.
type Cookies struct {
	Enabled bool
	Domain  string
	Secure  bool

	// SameSite applies to the access and CSRF cookies. The refresh cookie is
	// always Strict: it is only ever needed by the application's own calls.
	SameSite string
}

func (k Cookies) sameSite() string {
	switch strings.ToLower(strings.TrimSpace(k.SameSite)) {
	case "strict":
		return fiber.CookieSameSiteStrictMode
	case "none":
		return fiber.CookieSameSiteNoneMode
	default:
		return fiber.CookieSameSiteLaxMode
	}
}

// CookiesEnabled reports whether the cookie transport is on.
func (g *Guard) CookiesEnabled() bool { return g.cfg.Cookies.Enabled }

// CookieMode reports whether the answer to a sign-in should travel in
// cookies: the transport is on and the client did not ask for bearer tokens.
func (g *Guard) CookieMode(c fiber.Ctx) bool {
	return g.cfg.Cookies.Enabled && !strings.EqualFold(strings.TrimSpace(c.Get(HeaderMode)), TransportBearer)
}

// SetSession hands a token pair to the browser: the two tokens in HttpOnly
// cookies, and a CSRF token it can read.
func (g *Guard) SetSession(c fiber.Ctx, tokens Tokens) error {
	k := g.cfg.Cookies

	c.Cookie(&fiber.Cookie{
		Name:     CookieAccess,
		Value:    tokens.AccessToken,
		Path:     "/",
		Domain:   k.Domain,
		MaxAge:   int(g.cfg.AccessTTL.Seconds()),
		Secure:   k.Secure,
		HTTPOnly: true,
		SameSite: k.sameSite(),
	})
	c.Cookie(&fiber.Cookie{
		Name:     CookieRefresh,
		Value:    tokens.RefreshToken,
		Path:     RefreshPath,
		Domain:   k.Domain,
		MaxAge:   int(g.cfg.RefreshTTL.Seconds()),
		Secure:   k.Secure,
		HTTPOnly: true,
		SameSite: fiber.CookieSameSiteStrictMode,
	})

	_, err := g.EnsureCSRF(c)
	return err
}

// ClearSession expires the three cookies.
func (g *Guard) ClearSession(c fiber.Ctx) {
	k := g.cfg.Cookies
	for _, cookie := range []struct {
		name, path string
		http       bool
		same       string
	}{
		{CookieAccess, "/", true, k.sameSite()},
		{CookieRefresh, RefreshPath, true, fiber.CookieSameSiteStrictMode},
		{CookieCSRF, "/", false, k.sameSite()},
	} {
		c.Cookie(&fiber.Cookie{
			Name:     cookie.name,
			Value:    "",
			Path:     cookie.path,
			Domain:   k.Domain,
			MaxAge:   -1,
			Expires:  time.Unix(0, 0),
			Secure:   k.Secure,
			HTTPOnly: cookie.http,
			SameSite: cookie.same,
		})
	}
}

// EnsureCSRF keeps a well-formed omega_csrf cookie the browser already has,
// or sets a fresh one, and returns its value. Keeping it means a tab that
// read the cookie a moment ago does not race a rotation.
func (g *Guard) EnsureCSRF(c fiber.Ctx) (string, error) {
	token := c.Cookies(CookieCSRF)
	if !wellFormedCSRF(token) {
		raw := make([]byte, csrfBytes)
		if _, err := rand.Read(raw); err != nil {
			return "", err
		}
		token = base64.RawURLEncoding.EncodeToString(raw)
	}

	k := g.cfg.Cookies
	c.Cookie(&fiber.Cookie{
		Name:     CookieCSRF,
		Value:    token,
		Path:     "/",
		Domain:   k.Domain,
		MaxAge:   int(g.cfg.RefreshTTL.Seconds()),
		Secure:   k.Secure,
		HTTPOnly: false,
		SameSite: k.sameSite(),
	})
	return token, nil
}

func wellFormedCSRF(token string) bool {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	return err == nil && len(raw) == csrfBytes
}

// CheckCSRF compares the X-CSRF-Token header with the omega_csrf cookie, in
// constant time.
func CheckCSRF(c fiber.Ctx) error {
	cookie := c.Cookies(CookieCSRF)
	header := c.Get(HeaderCSRF)
	if cookie == "" || header == "" {
		return ErrCSRF
	}
	if subtle.ConstantTimeCompare([]byte(cookie), []byte(header)) != 1 {
		return ErrCSRF
	}
	return nil
}

// Unsafe reports whether a method changes state, and so needs the CSRF check
// when the request rides on cookies.
func Unsafe(method string) bool {
	switch method {
	case fiber.MethodGet, fiber.MethodHead, fiber.MethodOptions, fiber.MethodTrace:
		return false
	default:
		return true
	}
}

// RidesOnCookies reports whether a request would authenticate with cookies:
// no Authorization header, and one of the session cookies present.
func (g *Guard) RidesOnCookies(c fiber.Ctx) bool {
	if !g.cfg.Cookies.Enabled || bearer(c) != "" {
		return false
	}
	return c.Cookies(CookieAccess) != "" || c.Cookies(CookieRefresh) != ""
}

// CSRF guards a route that may be reached with cookies but is not behind
// Required: the refresh and logout endpoints. Bearer requests pass untouched.
func (g *Guard) CSRF() fiber.Handler {
	return func(c fiber.Ctx) error {
		if Unsafe(c.Method()) && g.RidesOnCookies(c) {
			if err := CheckCSRF(c); err != nil {
				return fiber.NewError(fiber.StatusForbidden, CSRFMessage)
			}
		}
		return c.Next()
	}
}

// RefreshToken reads the refresh token from the cookie, when the transport is
// on and the request carries one.
func (g *Guard) RefreshToken(c fiber.Ctx) string {
	if !g.cfg.Cookies.Enabled {
		return ""
	}
	return c.Cookies(CookieRefresh)
}

// Transport reports how the request authenticated, once a guard ran.
func Transport(c fiber.Ctx) string {
	transport, _ := c.Locals(LocalsTransport).(string)
	return transport
}
