package factories

import (
	"time"

	"github.com/brianvoe/gofakeit/v7"
	"golang.org/x/crypto/bcrypt"

	"omega/app/models"
	"omega/internal/database"
)

const defaultPassword = "password"

var User = database.NewFactory(func(f *gofakeit.Faker) models.User {
	verified := f.PastDate()

	return models.User{
		Name:            f.Name(),
		Email:           f.Email(),
		Password:        hashed(defaultPassword),
		Role:            "user",
		EmailVerifiedAt: &verified,
	}
})

var Admin = User.State(func(u *models.User) { u.Role = "admin" })

var Unverified = User.State(func(u *models.User) { u.EmailVerifiedAt = nil })

var cachedHash string

func hashed(plain string) string {
	if cachedHash == "" {
		raw, err := bcrypt.GenerateFromPassword([]byte(plain), bcrypt.MinCost)
		if err != nil {
			return ""
		}
		cachedHash = string(raw)
	}
	return cachedHash
}

func VerifiedAt(t time.Time) func(*models.User) {
	return func(u *models.User) { u.EmailVerifiedAt = &t }
}
