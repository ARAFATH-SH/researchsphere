package accesstoken

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

var (
	secret = []byte("test-secret-that-is-at-least-32-bytes-long")
	start  = time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
)

func TestSignThenVerify(t *testing.T) {
	s := New(secret, 15*time.Minute)
	token, exp, err := s.Sign("user-1", "researcher", start)
	if err != nil {
		t.Fatal(err)
	}
	if want := start.Add(15 * time.Minute); !exp.Equal(want) {
		t.Errorf("expiresAt = %v, want %v", exp, want)
	}
	got, err := s.Verify(token, start.Add(14*time.Minute))
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if want := (Claims{UserID: "user-1", Role: "researcher"}); got != want {
		t.Errorf("claims = %+v, want %+v", got, want)
	}
}

func TestVerifyRefuses(t *testing.T) {
	s := New(secret, 15*time.Minute)
	good, _, err := s.Sign("user-1", "researcher", start)
	if err != nil {
		t.Fatal(err)
	}
	sign := func(method jwt.SigningMethod, key any, claims jwt.MapClaims) string {
		tok, err := jwt.NewWithClaims(method, claims).SignedString(key)
		if err != nil {
			t.Fatal(err)
		}
		return tok
	}
	exp := start.Add(time.Hour).Unix()
	parts := strings.Split(good, ".")

	tests := []struct {
		name  string
		token string
		at    time.Time
	}{
		{"expired", good, start.Add(15 * time.Minute)},
		{"long expired", good, start.Add(48 * time.Hour)},
		{"tampered payload", parts[0] + "." + parts[1] + "x." + parts[2], start},
		{"tampered signature", parts[0] + "." + parts[1] + "." + strings.Repeat("A", len(parts[2])), start},
		{"wrong secret", sign(jwt.SigningMethodHS256, []byte("another-secret-of-at-least-32-bytes!!"), jwt.MapClaims{"sub": "user-1", "role": "researcher", "exp": exp}), start},
		{"alg none", sign(jwt.SigningMethodNone, jwt.UnsafeAllowNoneSignatureType, jwt.MapClaims{"sub": "user-1", "role": "researcher", "exp": exp}), start},
		{"other HMAC algorithm", sign(jwt.SigningMethodHS512, secret, jwt.MapClaims{"sub": "user-1", "role": "researcher", "exp": exp}), start},
		{"no exp", sign(jwt.SigningMethodHS256, secret, jwt.MapClaims{"sub": "user-1", "role": "researcher"}), start},
		{"no sub", sign(jwt.SigningMethodHS256, secret, jwt.MapClaims{"role": "researcher", "exp": exp}), start},
		{"empty sub", sign(jwt.SigningMethodHS256, secret, jwt.MapClaims{"sub": "", "role": "researcher", "exp": exp}), start},
		{"no role (signed before roles)", sign(jwt.SigningMethodHS256, secret, jwt.MapClaims{"sub": "user-1", "exp": exp}), start},
		{"empty role", sign(jwt.SigningMethodHS256, secret, jwt.MapClaims{"sub": "user-1", "role": "", "exp": exp}), start},
		{"role not a string", sign(jwt.SigningMethodHS256, secret, jwt.MapClaims{"sub": "user-1", "role": 1, "exp": exp}), start},
		{"empty string", "", start},
		{"garbage", "not-a-token", start},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := s.Verify(tt.token, tt.at)
			if !errors.Is(err, ErrInvalid) {
				t.Errorf("Verify = (%+v, %v), want ErrInvalid", c, err)
			}
		})
	}
}
