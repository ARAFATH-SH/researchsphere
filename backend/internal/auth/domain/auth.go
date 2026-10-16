// Package domain holds the auth module's entities, errors and the interfaces its usecase
// depends on. Standard library only.
package domain

import (
	"errors"
	"time"
)

// TokenPurpose says what a one-time token is for. The values match the
// auth_token_purpose enum in the database.
type TokenPurpose string

// Token purposes.
const (
	PurposeEmailVerification TokenPurpose = "email_verification"
	PurposePasswordReset     TokenPurpose = "password_reset"
)

// User is the part of an account the auth module reads.
type User struct {
	ID              string
	Email           string
	FullName        string
	EmailVerifiedAt *time.Time // nil until the address is verified (FR-1.4)
	SuspendedAt     *time.Time // set while the account is suspended (FR-3.4)
}

// Verified reports whether the user's email address has been verified.
func (u User) Verified() bool { return u.EmailVerifiedAt != nil }

// Suspended reports whether the account is suspended (FR-3.4).
func (u User) Suspended() bool { return u.SuspendedAt != nil }

// NewUser is what registration stores. PasswordHash is never the password itself.
type NewUser struct {
	Email        string
	FullName     string
	PasswordHash string
}

// NewToken is a one-time token to store. Only the hash of the token is kept (NFR-5).
type NewToken struct {
	UserID    string
	Purpose   TokenPurpose
	Hash      string
	ExpiresAt time.Time
}

// Token is a stored one-time token.
type Token struct {
	ID        string
	UserID    string
	ExpiresAt time.Time
	UsedAt    *time.Time // set once the token has been used
	// UserSuspendedAt is the suspended time of the token's user (FR-3.4). Only
	// FindTokenForUpdate fills it.
	UserSuspendedAt *time.Time
}

// Credentials is a user with the stored password hash, read only to check a login.
type Credentials struct {
	User
	PasswordHash string
}

// NewSession is a login to store: one row per login (FR-2.1). Only the hash of the
// refresh token is kept (NFR-5).
type NewSession struct {
	UserID    string
	TokenHash string
	ExpiresAt time.Time
}

// Session is a stored login (FR-2.1). It ends when it expires or is revoked (FR-2.2).
type Session struct {
	ID        string
	UserID    string
	ExpiresAt time.Time
	RevokedAt *time.Time // set once the session has ended
	// UserSuspendedAt is the suspended time of the session's user (FR-3.4). Only
	// FindSessionForUpdate fills it.
	UserSuspendedAt *time.Time
}

// VerificationEmail is the message that carries a verification link.
type VerificationEmail struct {
	To       string
	FullName string
	Link     string // contains the token: never log it
}

// PasswordResetEmail is the message that carries a password reset link (FR-2.3).
type PasswordResetEmail struct {
	To       string
	FullName string
	Link     string // contains the token: never log it
}

// Errors the usecase returns. The HTTP adapter turns each into an error code.
var (
	ErrInvalidFullName  = errors.New("full name is empty or too long")
	ErrInvalidEmail     = errors.New("email address is not valid")
	ErrPasswordTooShort = errors.New("password is too short")
	ErrPasswordTooLong  = errors.New("password is too long")
	ErrEmailTaken       = errors.New("email address is already registered")
	ErrTokenInvalid     = errors.New("token is not valid")
	ErrTokenUsed        = errors.New("token has already been used")
	ErrTokenExpired     = errors.New("token has expired")
	// ErrInvalidCredentials covers both an unknown email address and a wrong password,
	// so a login attempt cannot tell which accounts exist (FR-2.1).
	ErrInvalidCredentials = errors.New("email address or password is wrong")
	ErrEmailNotVerified   = errors.New("email address is not verified")
	// ErrAccountSuspended refuses a login, only after the right password, and a password
	// reset of a suspended account (FR-3.4).
	ErrAccountSuspended = errors.New("account is suspended")
	// ErrWrongCurrentPassword refuses a password change whose current password is wrong
	// (FR-2.6).
	ErrWrongCurrentPassword = errors.New("current password is wrong")
	// ErrSamePassword refuses a password change to the password already in use.
	ErrSamePassword = errors.New("new password is the same as the current one")
	// ErrWrongPassword refuses an account deletion whose password is wrong (FR-2.8).
	ErrWrongPassword = errors.New("password is wrong")
	// ErrOwnsSharedProject is wrapped by SharedProjectsError, which lists the projects
	// (FR-2.9).
	ErrOwnsSharedProject = errors.New("user owns a project that has other members")
	// ErrAccountLocked is wrapped by LockedError, which says when the lock ends (FR-2.7).
	ErrAccountLocked = errors.New("account is locked after too many failed logins")
	// ErrSessionInvalid covers every refused refresh: a missing, unknown, expired, revoked
	// or reused refresh token. The client is not told which (FR-2.2).
	ErrSessionInvalid = errors.New("session is not valid")
	// ErrNotFound is returned by a Store or Tx when the row does not exist.
	ErrNotFound = errors.New("not found")
)

// LockedError refuses a login while the account is locked (FR-2.7). It matches
// ErrAccountLocked with errors.Is.
type LockedError struct {
	Until      time.Time     // when the lock ends
	RetryAfter time.Duration // how long until then, measured when the login was refused
}

func (e *LockedError) Error() string { return ErrAccountLocked.Error() }

func (e *LockedError) Unwrap() error { return ErrAccountLocked }

// ProjectRef names a project.
type ProjectRef struct {
	ID    string
	Title string
}

// SharedProjectsError refuses an account deletion while the user owns projects that have
// other members (FR-2.9). It matches ErrOwnsSharedProject with errors.Is.
type SharedProjectsError struct {
	Projects []ProjectRef
}

func (e *SharedProjectsError) Error() string { return ErrOwnsSharedProject.Error() }

func (e *SharedProjectsError) Unwrap() error { return ErrOwnsSharedProject }
