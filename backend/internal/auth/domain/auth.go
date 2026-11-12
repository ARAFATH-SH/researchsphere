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
	// SuspendedUntil is when a temporary suspension ends; nil when permanent (FR-15.7).
	SuspendedUntil *time.Time
	// SuspensionReason is what the administrator told the user (FR-15.7); nil for an account
	// suspended before reasons were recorded. FindCredentialsByEmail, FindUserByID and
	// ListUsers fill it.
	SuspensionReason *string
	// Role is the platform role (users.role, SRS 2.3), signed into the access token.
	// FindCredentialsByEmail, FindUserByID and ListUsers fill it.
	Role string
	// CreatedAt is when the account was registered. FindUserByID and ListUsers fill it.
	CreatedAt time.Time
}

// Platform roles (SRS 2.2). The values match the user_role enum in the database.
const (
	RoleResearcher = "researcher"
	RoleAdmin      = "admin"
)

// UserPage is one page of the users list an administrator sees (FR-15.3).
type UserPage struct {
	Users    []User
	Page     int       // from 1
	PageSize int       // the size used, at most 50
	Total    int       // users on all pages
	At       time.Time // the time the page was read, for User.Suspended
}

// Verified reports whether the user's email address has been verified.
func (u User) Verified() bool { return u.EmailVerifiedAt != nil }

// Suspended reports whether the account is suspended at now (FR-3.4, FR-15.7). A temporary
// suspension no longer counts from its end time, though its columns stay until the next login
// or refresh clears them.
func (u User) Suspended(now time.Time) bool { return Suspended(u.SuspendedAt, u.SuspendedUntil, now) }

// SuspensionExpired reports whether a temporary suspension has ended but is still stored.
func (u User) SuspensionExpired(now time.Time) bool {
	return SuspensionExpired(u.SuspendedAt, u.SuspendedUntil, now)
}

// Suspended reports whether a suspension with these times holds at now.
func Suspended(at, until *time.Time, now time.Time) bool {
	return at != nil && (until == nil || now.Before(*until))
}

// SuspensionExpired reports whether a suspension with these times has ended by time.
func SuspensionExpired(at, until *time.Time, now time.Time) bool {
	return at != nil && until != nil && !now.Before(*until)
}

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
	UserSuspendedAt    *time.Time
	UserSuspendedUntil *time.Time
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
	UserSuspendedAt    *time.Time
	UserSuspendedUntil *time.Time
	// UserRole is the platform role of the session's user, signed into the new access
	// token at refresh, so a role change takes effect there (SRS 2.3). Only
	// FindSessionForUpdate fills it.
	UserRole string
	// UserVerified says whether the session's user has verified their email address,
	// signed into the new access token at refresh (FR-17.3). Only FindSessionForUpdate
	// fills it.
	UserVerified bool
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
	// ErrEmailDomainNotAllowed refuses a sign-up with a disposable domain or one that
	// cannot receive email (FR-17.2).
	ErrEmailDomainNotAllowed = errors.New("email domain is not allowed")
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
	// ErrInvalidPage refuses a list page or page size below 1, or a page so far out that
	// its offset does not fit a query parameter.
	ErrInvalidPage = errors.New("page or page size is not valid")
	// ErrNotFound is returned by a Store or Tx when the row does not exist.
	ErrNotFound = errors.New("not found")
)

// SuspendedError refuses a login, after the right password, while the account is suspended
// (FR-3.4, FR-15.7). It matches ErrAccountSuspended with errors.Is.
type SuspendedError struct {
	Reason *string    // nil for an account suspended before reasons were recorded
	Until  *time.Time // nil when permanent
}

func (e *SuspendedError) Error() string { return ErrAccountSuspended.Error() }

func (e *SuspendedError) Unwrap() error { return ErrAccountSuspended }

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
