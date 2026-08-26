package auth

import (
	"crypto/rand"
	"encoding/base64"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
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

type Config struct {
	Secret     string
	AccessTTL  time.Duration
	RefreshTTL time.Duration
	Loader     Loader
	Store      Store
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

func New(cfg Config) *Guard {
	if cfg.AccessTTL <= 0 {
		cfg.AccessTTL = 15 * time.Minute
	}
	if cfg.RefreshTTL <= 0 {
		cfg.RefreshTTL = 30 * 24 * time.Hour
	}
	return &Guard{cfg: cfg, secret: []byte(cfg.Secret)}
}

func (g *Guard) Issue(subject, role string) (Tokens, error) {
	now := time.Now()

	access, err := Sign(g.secret, Claims{
		Kind: KindAccess,
		Role: role,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   subject,
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
	expires := now.Add(g.cfg.RefreshTTL)

	refresh, err := Sign(g.secret, Claims{
		Kind: KindRefresh,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   subject,
			ID:        id,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expires),
		},
	})
	if err != nil {
		return Tokens{}, err
	}

	if g.cfg.Store != nil {
		if err := g.cfg.Store.Save(id, subject, expires); err != nil {
			return Tokens{}, err
		}
	}

	return Tokens{
		AccessToken:  access,
		RefreshToken: refresh,
		TokenType:    "Bearer",
		ExpiresIn:    int(g.cfg.AccessTTL.Seconds()),
	}, nil
}

func (g *Guard) Rotate(raw, role string) (Tokens, error) {
	claims, err := Verify(g.secret, raw, KindRefresh)
	if err != nil {
		return Tokens{}, err
	}

	if g.cfg.Store != nil {
		live, err := g.cfg.Store.Exists(claims.ID)
		if err != nil {
			return Tokens{}, err
		}
		if !live {
			return Tokens{}, ErrSignature
		}
		if err := g.cfg.Store.Revoke(claims.ID); err != nil {
			return Tokens{}, err
		}
	}

	return g.Issue(claims.Subject, role)
}

func (g *Guard) Revoke(raw string) error {
	claims, err := Verify(g.secret, raw, KindRefresh)
	if err != nil {
		return err
	}
	if g.cfg.Store == nil {
		return nil
	}
	return g.cfg.Store.Revoke(claims.ID)
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

func (g *Guard) Required() fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims, err := g.authenticate(c)
		if err != nil {
			return unauthorized(err)
		}
		c.Locals(LocalsClaims, claims)
		return c.Next()
	}
}

func (g *Guard) Optional() fiber.Handler {
	return func(c *fiber.Ctx) error {
		if claims, err := g.authenticate(c); err == nil {
			c.Locals(LocalsClaims, claims)
		}
		return c.Next()
	}
}

func (g *Guard) Roles(roles ...string) fiber.Handler {
	allowed := make(map[string]bool, len(roles))
	for _, role := range roles {
		allowed[role] = true
	}

	return func(c *fiber.Ctx) error {
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

func (g *Guard) authenticate(c *fiber.Ctx) (Claims, error) {
	raw := bearer(c)
	if raw == "" {
		return Claims{}, ErrMalformed
	}

	claims, err := Verify(g.secret, raw, KindAccess)
	if err != nil {
		return claims, err
	}

	return claims, nil
}

func (g *Guard) User(c *fiber.Ctx) any {
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

func Fingerprint(c *fiber.Ctx) string {
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

func ClaimsOf(c *fiber.Ctx) (Claims, bool) {
	claims, ok := c.Locals(LocalsClaims).(Claims)
	return claims, ok
}

func bearer(c *fiber.Ctx) string {
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

func unauthorized(err error) error {
	if err == ErrExpired {
		return fiber.NewError(fiber.StatusUnauthorized, "Token expired.")
	}
	return fiber.NewError(fiber.StatusUnauthorized, "Authentication required.")
}

func randomID() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}
