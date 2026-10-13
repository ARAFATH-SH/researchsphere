// Package accesstoken signs and checks the access tokens issued at login (FR-2.1):
// JWTs signed with HS256 that carry the user ID in "sub" and expire after a fixed time.
// The auth usecase signs them through its domain interface; the auth middleware in
// httpx checks them.
package accesstoken

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// ErrInvalid is returned by Verify for every token that must not be accepted: malformed,
// badly signed, signed with another algorithm, expired or without a user ID.
var ErrInvalid = errors.New("access token is not valid")

// Signer signs and checks access tokens with one secret.
type Signer struct {
	secret []byte
	ttl    time.Duration
}

// New returns a Signer whose tokens are valid for ttl. secret comes from JWT_SECRET:
// never log it.
func New(secret []byte, ttl time.Duration) *Signer {
	return &Signer{secret: secret, ttl: ttl}
}

// Sign returns a token for userID issued at now, and the time it expires.
func (s *Signer) Sign(userID string, now time.Time) (string, time.Time, error) {
	exp := now.Add(s.ttl)
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{
		Subject:   userID,
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(exp),
	}).SignedString(s.secret)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("sign access token: %w", err)
	}
	return token, exp, nil
}

// Verify returns the user ID in token, or ErrInvalid. now decides whether it expired.
func (s *Signer) Verify(token string, now time.Time) (string, error) {
	var claims jwt.RegisteredClaims
	_, err := jwt.ParseWithClaims(token, &claims,
		func(*jwt.Token) (any, error) { return s.secret, nil },
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithExpirationRequired(),
		jwt.WithTimeFunc(func() time.Time { return now }),
	)
	if err != nil || claims.Subject == "" {
		return "", ErrInvalid
	}
	return claims.Subject, nil
}
