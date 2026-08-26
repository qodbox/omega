package auth

import (
	"errors"

	"github.com/golang-jwt/jwt/v5"
)

var (
	ErrMalformed = errors.New("auth: malformed token")
	ErrSignature = errors.New("auth: bad signature")
	ErrExpired   = errors.New("auth: token expired")
	ErrKind      = errors.New("auth: wrong token kind")
)

const (
	KindAccess  = "access"
	KindRefresh = "refresh"
)

var method = jwt.SigningMethodHS256

type Claims struct {
	Kind string `json:"typ"`
	Role string `json:"role,omitempty"`
	jwt.RegisteredClaims
}

func Sign(secret []byte, claims Claims) (string, error) {
	return jwt.NewWithClaims(method, claims).SignedString(secret)
}

func Verify(secret []byte, raw, kind string) (Claims, error) {
	var claims Claims

	_, err := jwt.ParseWithClaims(raw, &claims,
		func(*jwt.Token) (any, error) { return secret, nil },

		jwt.WithValidMethods([]string{method.Alg()}),
		jwt.WithExpirationRequired(),
	)
	if err != nil {
		return claims, translate(err)
	}

	if claims.Kind != kind {
		return claims, ErrKind
	}
	return claims, nil
}

func translate(err error) error {
	switch {
	case errors.Is(err, jwt.ErrTokenExpired), errors.Is(err, jwt.ErrTokenNotValidYet):
		return ErrExpired
	case errors.Is(err, jwt.ErrTokenSignatureInvalid), errors.Is(err, jwt.ErrTokenUnverifiable):
		return ErrSignature
	default:
		return ErrMalformed
	}
}
