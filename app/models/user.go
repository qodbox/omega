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

	EmailVerifiedAt *time.Time     `json:"email_verified_at,omitempty"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
	DeletedAt       gorm.DeletedAt `gorm:"index" json:"-"`
}

func (User) TableName() string { return "users" }

func (u *User) SetPassword(plain string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(plain), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	u.Password = string(hash)
	return nil
}

func (u *User) CheckPassword(plain string) bool {
	return bcrypt.CompareHashAndPassword([]byte(u.Password), []byte(plain)) == nil
}

func (u *User) IsVerified() bool { return u.EmailVerifiedAt != nil }

func (u *User) Is(role string) bool { return u.Role == role }

func (u *User) Key() string { return strconv.FormatUint(uint64(u.ID), 10) }
