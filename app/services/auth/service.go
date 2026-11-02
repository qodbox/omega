package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/markbates/goth"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"omega/app/models"
	request "omega/app/requests/auth"
	jwt "omega/internal/auth"
	"omega/internal/events"
	"omega/internal/validation"
)

var (
	ErrInvalidCredentials = errors.New("services: invalid credentials")
	ErrInvalidRefresh     = errors.New("services: invalid refresh token")
	ErrNoProviderEmail    = errors.New("services: the provider returned no email address")
	ErrUnverifiedProvider = errors.New("services: the provider did not verify this email address")
)

type Service struct {
	db       *gorm.DB
	guard    *jwt.Guard
	events   *events.Bus
	attempts *jwt.Attempts
}

func NewService(db *gorm.DB, guard *jwt.Guard, bus *events.Bus) *Service {
	// Hashed ahead of the first unknown-email sign-in, which would otherwise
	// take twice as long as the ones after it.
	go burnHash()
	return &Service{db: db, guard: guard, events: bus}
}

func (a *Service) Throttle(attempts *jwt.Attempts) *Service {
	a.attempts = attempts
	return a
}

type Session struct {
	User   *models.User
	Tokens jwt.Tokens
}

func (a *Service) Register(ctx context.Context, body request.Register) (*Session, error) {
	email := normaliseEmail(body.Email)

	var taken int64
	if err := a.db.WithContext(ctx).Model(&models.User{}).
		Where("email = ?", email).Count(&taken).Error; err != nil {
		return nil, err
	}
	if taken > 0 {
		return nil, validation.Failed("email", "This email address is already registered.")
	}

	user := models.User{
		Name:  strings.TrimSpace(body.Name),
		Email: email,
		Role:  models.RoleUser,
	}
	if err := user.SetPassword(body.Password); err != nil {
		return nil, err
	}
	if err := a.db.WithContext(ctx).Create(&user).Error; err != nil {
		return nil, err
	}

	if a.events != nil {
		_ = a.events.Emit(ctx, "user.registered", map[string]any{
			"id": user.ID, "name": user.Name, "email": user.Email,
		})
	}

	return a.session(&user)
}

func (a *Service) Login(ctx context.Context, body request.Login, address string) (*Session, error) {
	email := normaliseEmail(body.Email)

	if err := a.attempts.Allow(ctx, email); err != nil {
		return nil, err
	}

	var user models.User
	err := a.db.WithContext(ctx).Where("email = ?", email).Take(&user).Error

	if errors.Is(err, gorm.ErrRecordNotFound) {
		burnPasswordCheck(body.Password)
		_ = a.attempts.Record(ctx, email, address)
		return nil, ErrInvalidCredentials
	}
	if err != nil {
		return nil, err
	}
	if !user.CheckPassword(body.Password) {
		_ = a.attempts.Record(ctx, email, address)
		return nil, ErrInvalidCredentials
	}

	_ = a.attempts.Clear(ctx, email)
	a.upgradeHash(ctx, &user, body.Password)
	return a.session(&user)
}

// upgradeHash re-hashes a password stored at a lower bcrypt cost, now that
// the plain text is at hand. A failure is not the caller's problem: the old
// hash still verifies.
func (a *Service) upgradeHash(ctx context.Context, user *models.User, plain string) {
	if !user.NeedsRehash() || user.SetPassword(plain) != nil {
		return
	}
	_ = a.db.WithContext(ctx).Model(user).Update("password", user.Password).Error
}

// Refresh spends a refresh token for a new pair. A token replayed after its
// rotation revokes its whole family (the guard logs it) and is refused.
func (a *Service) Refresh(ctx context.Context, refreshToken string) (*Session, error) {
	subject, err := a.guard.Subject(refreshToken)
	if err != nil {
		return nil, ErrInvalidRefresh
	}

	var user models.User
	if err := a.db.WithContext(ctx).Where("id = ?", subject).Take(&user).Error; err != nil {
		return nil, ErrInvalidRefresh
	}

	tokens, err := a.guard.RotateGrant(refreshToken, user.Role, user.TokenVersion)
	if err != nil {
		return nil, ErrInvalidRefresh
	}
	return &Session{User: &user, Tokens: tokens}, nil
}

func (a *Service) Logout(refreshToken string) {
	_ = a.guard.Revoke(refreshToken)
}

// LogoutAll signs the account out everywhere: every refresh token is revoked
// and the token version bumped, so the access tokens in flight stop working
// on their next request.
func (a *Service) LogoutAll(ctx context.Context, user *models.User) error {
	if err := a.bumpVersion(ctx, user); err != nil {
		return err
	}
	return a.guard.RevokeAll(user.Key())
}

func (a *Service) bumpVersion(ctx context.Context, user *models.User) error {
	if err := a.db.WithContext(ctx).Model(&models.User{}).Where("id = ?", user.ID).
		Update("token_version", gorm.Expr("token_version + 1")).Error; err != nil {
		return err
	}
	return a.db.WithContext(ctx).Model(&models.User{}).Where("id = ?", user.ID).
		Select("token_version").Scan(&user.TokenVersion).Error
}

func (a *Service) ChangePassword(ctx context.Context, user *models.User, body request.ChangePassword) (jwt.Tokens, error) {
	if !user.CheckPassword(body.CurrentPassword) {
		return jwt.Tokens{}, validation.Failed("current_password", "This is not the current password.")
	}

	if err := user.SetPassword(body.NewPassword); err != nil {
		return jwt.Tokens{}, err
	}
	if err := a.db.WithContext(ctx).Model(user).Update("password", user.Password).Error; err != nil {
		return jwt.Tokens{}, err
	}
	if err := a.LogoutAll(ctx, user); err != nil {
		return jwt.Tokens{}, err
	}

	return a.guard.IssueGrant(jwt.Grant{Subject: user.Key(), Role: user.Role, Version: user.TokenVersion})
}

func (a *Service) SignInWithProvider(ctx context.Context, external goth.User) (*Session, error) {
	email := normaliseEmail(external.Email)
	if email == "" {
		return nil, ErrNoProviderEmail
	}

	trusted := providerVerifiedEmail(external)

	var user models.User
	err := a.db.WithContext(ctx).Where("email = ?", email).Take(&user).Error
	if err == nil {
		if !trusted {
			return nil, ErrUnverifiedProvider
		}
		return a.session(&user)
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	user = models.User{
		Name:  displayName(external, email),
		Email: email,
		Role:  models.RoleUser,
	}
	if trusted {
		verified := time.Now()
		user.EmailVerifiedAt = &verified
	}

	password, err := unguessable()
	if err != nil {
		return nil, err
	}
	if err := user.SetPassword(password); err != nil {
		return nil, err
	}
	if err := a.db.WithContext(ctx).Create(&user).Error; err != nil {
		return nil, err
	}
	return a.session(&user)
}

func (a *Service) session(user *models.User) (*Session, error) {
	tokens, err := a.guard.IssueGrant(jwt.Grant{Subject: user.Key(), Role: user.Role, Version: user.TokenVersion})
	if err != nil {
		return nil, err
	}
	return &Session{User: user, Tokens: tokens}, nil
}

func providerVerifiedEmail(external goth.User) bool {
	for _, key := range []string{"email_verified", "verified_email", "verified"} {
		switch flag := external.RawData[key].(type) {
		case bool:
			if flag {
				return true
			}
		case string:
			if flag == "true" {
				return true
			}
		}
	}
	return false
}

func displayName(external goth.User, email string) string {
	for _, candidate := range []string{external.Name, external.NickName, external.FirstName} {
		if trimmed := strings.TrimSpace(candidate); trimmed != "" {
			return trimmed
		}
	}
	local, _, _ := strings.Cut(email, "@")
	return local
}

// burnHashes holds, per bcrypt cost, a hash of a random secret. Comparing an
// unknown account's password against the one at models.PasswordCost spends
// the time a real check spends, so the response time does not tell whether
// the email is registered.
var burnHashes sync.Map

func burnHash() []byte {
	cost := models.PasswordCost
	if hash, ok := burnHashes.Load(cost); ok {
		return hash.([]byte)
	}

	secret, err := unguessable()
	if err != nil {
		secret = "omega-burn-hash"
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(secret), cost)
	if err != nil {
		return nil
	}
	actual, _ := burnHashes.LoadOrStore(cost, hash)
	return actual.([]byte)
}

func burnPasswordCheck(plain string) {
	_ = bcrypt.CompareHashAndPassword(burnHash(), []byte(plain))
}

func unguessable() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func normaliseEmail(raw string) string { return strings.ToLower(strings.TrimSpace(raw)) }
