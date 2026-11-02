package auth

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/golang-jwt/jwt/v5"
)

const (
	LocalsUser   = "auth_user"
	LocalsClaims = "auth_claims"
)

type Store interface {
	Save(id, subject string, expires time.Time) error
	Exists(id string) (bool, error)
	Revoke(id string) error
	RevokeSubject(subject string) error
	Purge(now time.Time) error
}

type Loader func(subject string) (any, error)

// Versioned is a loaded user that carries a token version. An access token
// signed with another version is refused, so bumping it signs the account out
// everywhere at once.
type Versioned interface {
	AuthVersion() uint64
}

// Roled is a loaded user that carries its current role. The guard reads the
// role from the user rather than trusting the one frozen in the token, so a
// demotion takes effect on the next request.
type Roled interface {
	AuthRole() string
}

type Config struct {
	Secret     string
	AccessTTL  time.Duration
	RefreshTTL time.Duration
	Loader     Loader
	Store      Store

	// Cookies configures the cookie transport. Off by default here; the
	// application turns it on from config/auth.yaml.
	Cookies Cookies

	// ReuseGrace is how long after a rotation the same refresh token may be
	// replayed without being treated as stolen. Zero is strict. A replay inside
	// the grace is still refused; it just does not revoke the family.
	ReuseGrace time.Duration

	// OnReuse is told when a rotated refresh token comes back, after its family
	// has been revoked. Log it: it is a security event.
	OnReuse func(subject, family string)
}

type Guard struct {
	cfg    Config
	secret []byte
}

type Tokens struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
}

// Grant is what a token pair is signed for.
type Grant struct {
	Subject string
	Role    string

	// Version is the account's current token version.
	Version uint64

	// Family is the login chain the refresh token joins. Empty starts one.
	Family string
}

func New(cfg Config) *Guard {
	if cfg.AccessTTL <= 0 {
		cfg.AccessTTL = 15 * time.Minute
	}
	if cfg.RefreshTTL <= 0 {
		cfg.RefreshTTL = 30 * 24 * time.Hour
	}
	return &Guard{cfg: cfg, secret: []byte(cfg.Secret)}
}

// Issue signs a fresh pair, reading the token version through the Loader.
func (g *Guard) Issue(subject, role string) (Tokens, error) {
	return g.IssueGrant(Grant{Subject: subject, Role: role, Version: g.versionOf(subject)})
}

// IssueGrant signs a fresh pair for an explicit grant.
func (g *Guard) IssueGrant(grant Grant) (Tokens, error) {
	now := time.Now()

	access, err := Sign(g.secret, Claims{
		Kind:    KindAccess,
		Role:    grant.Role,
		Version: grant.Version,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   grant.Subject,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(g.cfg.AccessTTL)),
		},
	})
	if err != nil {
		return Tokens{}, err
	}

	id, err := randomID()
	if err != nil {
		return Tokens{}, err
	}
	family := grant.Family
	if family == "" {
		family = id
	}
	expires := now.Add(g.cfg.RefreshTTL)

	refresh, err := Sign(g.secret, Claims{
		Kind:   KindRefresh,
		Family: family,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   grant.Subject,
			ID:        id,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expires),
		},
	})
	if err != nil {
		return Tokens{}, err
	}

	if err := g.save(id, grant.Subject, family, expires); err != nil {
		return Tokens{}, err
	}

	return Tokens{
		AccessToken:  access,
		RefreshToken: refresh,
		TokenType:    "Bearer",
		ExpiresIn:    int(g.cfg.AccessTTL.Seconds()),
	}, nil
}

func (g *Guard) save(id, subject, family string, expires time.Time) error {
	switch store := g.cfg.Store.(type) {
	case nil:
		return nil
	case FamilyStore:
		return store.SaveFamily(id, subject, family, expires)
	default:
		return store.Save(id, subject, expires)
	}
}

// AccessTTL is how long an access token lives.
func (g *Guard) AccessTTL() time.Duration { return g.cfg.AccessTTL }

// RefreshTTL is how long a refresh token lives.
func (g *Guard) RefreshTTL() time.Duration { return g.cfg.RefreshTTL }

// Rotate spends a refresh token and signs a new pair in the same family.
func (g *Guard) Rotate(raw, role string) (Tokens, error) {
	return g.rotate(raw, func(subject string) Grant {
		return Grant{Subject: subject, Role: role, Version: g.versionOf(subject)}
	})
}

// RotateGrant is Rotate for a caller that already loaded the account.
func (g *Guard) RotateGrant(raw, role string, version uint64) (Tokens, error) {
	return g.rotate(raw, func(subject string) Grant {
		return Grant{Subject: subject, Role: role, Version: version}
	})
}

func (g *Guard) rotate(raw string, grant func(subject string) Grant) (Tokens, error) {
	claims, err := Verify(g.secret, raw, KindRefresh)
	if err != nil {
		return Tokens{}, err
	}

	family := claims.Family
	switch store := g.cfg.Store.(type) {
	case nil:
	case FamilyStore:
		live, usedAt, err := store.Consume(claims.ID, time.Now())
		switch {
		case errors.Is(err, ErrReused):
			if g.cfg.ReuseGrace > 0 && time.Since(usedAt) <= g.cfg.ReuseGrace {
				return Tokens{}, ErrSignature
			}
			if err := store.RevokeFamily(live); err != nil {
				return Tokens{}, err
			}
			if g.cfg.OnReuse != nil {
				g.cfg.OnReuse(claims.Subject, live)
			}
			return Tokens{}, ErrReused
		case errors.Is(err, ErrUnknownToken):
			return Tokens{}, ErrSignature
		case err != nil:
			return Tokens{}, err
		}
		family = live
	default:
		live, err := store.Exists(claims.ID)
		if err != nil {
			return Tokens{}, err
		}
		if !live {
			return Tokens{}, ErrSignature
		}
		if err := store.Revoke(claims.ID); err != nil {
			return Tokens{}, err
		}
		family = ""
	}

	next := grant(claims.Subject)
	next.Family = family
	return g.IssueGrant(next)
}

// Revoke signs out the login a refresh token belongs to: with a FamilyStore
// the whole chain goes, so an older token of the same login cannot revive it.
func (g *Guard) Revoke(raw string) error {
	claims, err := Verify(g.secret, raw, KindRefresh)
	if err != nil {
		return err
	}
	switch store := g.cfg.Store.(type) {
	case nil:
		return nil
	case FamilyStore:
		family := claims.Family
		if family == "" {
			family = claims.ID
		}
		return store.RevokeFamily(family)
	default:
		return store.Revoke(claims.ID)
	}
}

func (g *Guard) RevokeAll(subject string) error {
	if g.cfg.Store == nil {
		return nil
	}
	return g.cfg.Store.RevokeSubject(subject)
}

func (g *Guard) Subject(raw string) (string, error) {
	claims, err := Verify(g.secret, raw, KindRefresh)
	return claims.Subject, err
}

// Required refuses a request without a valid access token. With a Loader, the
// account is loaded once here (and cached for User): an access token whose
// version is stale, or whose account is gone, is refused immediately.
func (g *Guard) Required() fiber.Handler {
	return func(c fiber.Ctx) error {
		if err := g.Authenticate(c); err != nil {
			return err
		}
		return c.Next()
	}
}

// Authenticate is what Required checks, for a handler that decides itself
// what happens next. It answers the error Required would send: 401, or 403
// for a cookie-authenticated unsafe request without its CSRF token.
func (g *Guard) Authenticate(c fiber.Ctx) error {
	claims, err := g.authenticate(c)
	if err != nil {
		return refuse(err)
	}
	if err := g.admit(c, claims); err != nil {
		return refuse(err)
	}
	return nil
}

func (g *Guard) Optional() fiber.Handler {
	return func(c fiber.Ctx) error {
		if claims, err := g.authenticate(c); err == nil {
			if g.admit(c, claims) != nil {
				c.Locals(LocalsClaims, nil)
				c.Locals(LocalsUser, nil)
				c.Locals(LocalsTransport, nil)
			}
		}
		return c.Next()
	}
}

// admit records the claims, then checks them against the stored account.
func (g *Guard) admit(c fiber.Ctx, claims Claims) error {
	c.Locals(LocalsClaims, claims)
	if g.cfg.Loader == nil {
		return nil
	}

	user := g.User(c)
	if user == nil {
		return ErrRevoked
	}
	if versioned, ok := user.(Versioned); ok && versioned.AuthVersion() != claims.Version {
		return ErrRevoked
	}
	if roled, ok := user.(Roled); ok && roled.AuthRole() != claims.Role {
		claims.Role = roled.AuthRole()
		c.Locals(LocalsClaims, claims)
	}
	return nil
}

func (g *Guard) versionOf(subject string) uint64 {
	if g.cfg.Loader == nil {
		return 0
	}
	user, err := g.cfg.Loader(subject)
	if err != nil {
		return 0
	}
	if versioned, ok := user.(Versioned); ok {
		return versioned.AuthVersion()
	}
	return 0
}

func (g *Guard) Roles(roles ...string) fiber.Handler {
	allowed := make(map[string]bool, len(roles))
	for _, role := range roles {
		allowed[role] = true
	}

	return func(c fiber.Ctx) error {
		claims, ok := c.Locals(LocalsClaims).(Claims)
		if !ok {
			return fiber.NewError(fiber.StatusUnauthorized, "Authentication required.")
		}
		if !allowed[claims.Role] {
			return fiber.NewError(fiber.StatusForbidden, "This role may not access this resource.")
		}
		return c.Next()
	}
}

// authenticate reads the access token: from the Authorization header first
// (bearer mode, no CSRF check), then from the omega_access cookie (cookie
// mode, CSRF checked on unsafe methods).
func (g *Guard) authenticate(c fiber.Ctx) (Claims, error) {
	transport := TransportBearer
	raw := bearer(c)

	if raw == "" && g.cfg.Cookies.Enabled {
		raw = c.Cookies(CookieAccess)
		transport = TransportCookie
	}
	if raw == "" {
		return Claims{}, ErrMalformed
	}

	claims, err := Verify(g.secret, raw, KindAccess)
	if err != nil {
		return claims, err
	}

	if transport == TransportCookie && Unsafe(c.Method()) {
		if err := CheckCSRF(c); err != nil {
			return Claims{}, err
		}
	}

	c.Locals(LocalsTransport, transport)
	return claims, nil
}

func (g *Guard) User(c fiber.Ctx) any {
	if loaded := c.Locals(LocalsUser); loaded != nil {
		return loaded
	}

	claims, ok := ClaimsOf(c)
	if !ok || g.cfg.Loader == nil {
		return nil
	}

	user, err := g.cfg.Loader(claims.Subject)
	if err != nil || user == nil {
		return nil
	}

	c.Locals(LocalsUser, user)
	return user
}

func Fingerprint(c fiber.Ctx) string {
	raw := bearer(c)
	if raw == "" {
		return ""
	}

	signature := raw[strings.LastIndexByte(raw, '.')+1:]
	if len(signature) < 16 {
		return ""
	}
	return signature[:16]
}

func ClaimsOf(c fiber.Ctx) (Claims, bool) {
	claims, ok := c.Locals(LocalsClaims).(Claims)
	return claims, ok
}

func bearer(c fiber.Ctx) string {
	raw := strings.TrimSpace(c.Get(fiber.HeaderAuthorization))

	for range 2 {
		if len(raw) > 7 && strings.EqualFold(raw[:7], "bearer ") {
			raw = strings.TrimSpace(raw[7:])
		}
	}

	if strings.ContainsAny(raw, " \t") {
		return ""
	}
	return raw
}

// ErrRevoked is a valid access token whose account is gone or whose token
// version was bumped since it was signed.
var ErrRevoked = errors.New("auth: token revoked")

func refuse(err error) error {
	switch {
	case errors.Is(err, ErrCSRF):
		return fiber.NewError(fiber.StatusForbidden, CSRFMessage)
	case errors.Is(err, ErrExpired), errors.Is(err, ErrRevoked):
		return fiber.NewError(fiber.StatusUnauthorized, "Token expired.")
	default:
		return fiber.NewError(fiber.StatusUnauthorized, "Authentication required.")
	}
}

func randomID() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}
