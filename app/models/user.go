package models

import (
	"strconv"
	"time"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

const (
	RoleUser  = "user"
	RoleAdmin = "admin"
)

type User struct {
	ID       uint   `gorm:"primarykey" json:"id"`
	Name     string `gorm:"size:120;not null" json:"name"`
	Email    string `gorm:"size:255;not null;uniqueIndex" json:"email"`
	Password string `gorm:"size:255;not null" json:"-"`
	Role     string `gorm:"size:32;not null;default:user;index" json:"role"`

	// The currency this account wants to read amounts in. It changes what is
	// displayed, never what is stored: rows stay in the base currency.
	Currency string `gorm:"size:3;not null;default:EUR" json:"currency"`

	// TokenVersion is signed into every access token. Bumping it (sign out
	// everywhere, password change) refuses every access token signed before,
	// on the next request rather than at expiry.
	TokenVersion uint64 `gorm:"not null;default:0" json:"-"`

	EmailVerifiedAt *time.Time     `json:"email_verified_at,omitempty"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
	DeletedAt       gorm.DeletedAt `gorm:"index" json:"-"`
}

func (User) TableName() string { return "users" }

// PasswordCost is the bcrypt cost of every new hash. Hashes made at a lower
// cost still verify, and are upgraded on the next successful sign-in. A
// variable only so a test suite can lower it: never lower it in an
// application.
var PasswordCost = 12

func (u *User) SetPassword(plain string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(plain), PasswordCost)
	if err != nil {
		return err
	}
	u.Password = string(hash)
	return nil
}

func (u *User) CheckPassword(plain string) bool {
	return bcrypt.CompareHashAndPassword([]byte(u.Password), []byte(plain)) == nil
}

// NeedsRehash reports a hash made at a lower cost than PasswordCost.
func (u *User) NeedsRehash() bool {
	cost, err := bcrypt.Cost([]byte(u.Password))
	return err != nil || cost < PasswordCost
}

// AuthVersion is read by the guard to refuse access tokens signed before the
// last TokenVersion bump.
func (u *User) AuthVersion() uint64 { return u.TokenVersion }

// AuthRole is read by the guard, so a role change applies on the next request.
func (u *User) AuthRole() string { return u.Role }

func (u *User) IsVerified() bool { return u.EmailVerifiedAt != nil }

func (u *User) Is(role string) bool { return u.Role == role }

func (u *User) Key() string { return strconv.FormatUint(uint64(u.ID), 10) }
