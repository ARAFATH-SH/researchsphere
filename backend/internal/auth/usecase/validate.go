package usecase

import (
	"net/mail"
	"strings"
	"unicode/utf8"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/auth/domain"
)

// Input limits. The SRS sets only the minimum password length (FR-1.2); the maximums
// keep stored text and hashing work bounded.
const (
	MaxFullNameLength = 200
	MaxEmailLength    = 254 // the longest address SMTP allows (RFC 5321)
	MinPasswordLength = 8
	MaxPasswordLength = 128
)

// normalizeFullName trims the name and checks its length in characters.
func normalizeFullName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > MaxFullNameLength || strings.ContainsAny(name, "\r\n") {
		return "", domain.ErrInvalidFullName
	}
	return name, nil
}

// normalizeEmail trims the address and accepts only a bare address such as
// ada@example.org, not "Ada <ada@example.org>". Letter case is kept; the database
// compares addresses ignoring case.
func normalizeEmail(email string) (string, error) {
	email = strings.TrimSpace(email)
	if email == "" || len(email) > MaxEmailLength {
		return "", domain.ErrInvalidEmail
	}
	addr, err := mail.ParseAddress(email)
	if err != nil || addr.Name != "" || addr.Address != email {
		return "", domain.ErrInvalidEmail
	}
	return email, nil
}

// checkPassword counts characters, not bytes, so "pässwörd" is 8 long.
func checkPassword(password string) error {
	n := utf8.RuneCountInString(password)
	switch {
	case n < MinPasswordLength:
		return domain.ErrPasswordTooShort
	case n > MaxPasswordLength:
		return domain.ErrPasswordTooLong
	}
	return nil
}
