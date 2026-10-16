// Package accesstoken signs and checks the access tokens issued at login (FR-2.1):
// JWTs signed with HS256 that carry the user ID in "sub", the platform role in "role"
// (SRS 2.3) and expire after a fixed time.
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

// Claims is what a valid token says about its holder.
type Claims struct {
	UserID string
	Role   string // the platform role when the token was signed (users.role)
}

// claims is the token payload. A role change takes effect at the next refresh
// (docs/decisions.md).
type claims struct {
	Role string `json:"role"`
	jwt.RegisteredClaims
}

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

// Sign returns a token for userID with role, issued at now, and the time it expires.
func (s *Signer) Sign(userID, role string, now time.Time) (string, time.Time, error) {
	exp := now.Add(s.ttl)
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims{
		Role: role,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(exp),
		},
	}).SignedString(s.secret)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("sign access token: %w", err)
	}
	return token, exp, nil
}

// Verify returns the claims in token, or ErrInvalid. now decides whether it expired. A
// token without a role (signed before roles were added) is invalid, so the client
// refreshes and gets one with a role.
func (s *Signer) Verify(token string, now time.Time) (Claims, error) {
	var c claims
	_, err := jwt.ParseWithClaims(token, &c,
		func(*jwt.Token) (any, error) { return s.secret, nil },
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithExpirationRequired(),
		jwt.WithTimeFunc(func() time.Time { return now }),
	)
	if err != nil || c.Subject == "" || c.Role == "" {
		return Claims{}, ErrInvalid
	}
	return Claims{UserID: c.Subject, Role: c.Role}, nil
}
