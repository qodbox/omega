package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"strings"
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
	return a.session(&user)
}

func (a *Service) Refresh(ctx context.Context, refreshToken string) (jwt.Tokens, error) {
	subject, err := a.guard.Subject(refreshToken)
	if err != nil {
		return jwt.Tokens{}, ErrInvalidRefresh
	}

	var user models.User
	if err := a.db.WithContext(ctx).Where("id = ?", subject).Take(&user).Error; err != nil {
		return jwt.Tokens{}, ErrInvalidRefresh
	}

	tokens, err := a.guard.Rotate(refreshToken, user.Role)
	if err != nil {
		return jwt.Tokens{}, ErrInvalidRefresh
	}
	return tokens, nil
}

func (a *Service) Logout(refreshToken string) {
	_ = a.guard.Revoke(refreshToken)
}

func (a *Service) ChangePassword(ctx context.Context, user *models.User, body request.ChangePassword) (jwt.Tokens, error) {
	if !user.CheckPassword(body.CurrentPassword) {
		return jwt.Tokens{}, validation.Failed("current_password", "This is not the current password.")
	}

	if err := user.SetPassword(body.NewPassword); err != nil {
		return jwt.Tokens{}, err
	}
	if err := a.db.WithContext(ctx).Save(user).Error; err != nil {
		return jwt.Tokens{}, err
	}
	if err := a.guard.RevokeAll(user.Key()); err != nil {
		return jwt.Tokens{}, err
	}

	return a.guard.Issue(user.Key(), user.Role)
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
	tokens, err := a.guard.Issue(user.Key(), user.Role)
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

var burnHash = []byte("$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy")

func burnPasswordCheck(plain string) {
	_ = bcrypt.CompareHashAndPassword(burnHash, []byte(plain))
}

func unguessable() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func normaliseEmail(raw string) string { return strings.ToLower(strings.TrimSpace(raw)) }
