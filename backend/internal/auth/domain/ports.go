package domain

import (
	"context"
	"time"
)

// Store reads and writes accounts. Writes that must happen together go through RunInTx.
type Store interface {
	// RunInTx runs fn in one database transaction. It commits when fn returns nil and
	// rolls back otherwise, returning fn's error.
	RunInTx(ctx context.Context, fn func(tx Tx) error) error
	// FindUserByEmail returns ErrNotFound when no user has the address, ignoring letter case.
	FindUserByEmail(ctx context.Context, email string) (User, error)
	// FindCredentialsByEmail is FindUserByEmail with the password hash.
	FindCredentialsByEmail(ctx context.Context, email string) (Credentials, error)
	// FindUserByID returns ErrNotFound when no user has the ID.
	FindUserByID(ctx context.Context, id string) (User, error)
	CreateSession(ctx context.Context, s NewSession) error
	// RevokeSessionByToken ends the session whose current refresh token has this hash and
	// returns it. It returns ErrNotFound, and changes nothing, when there is no such
	// session or it has already ended.
	RevokeSessionByToken(ctx context.Context, hash string, at time.Time) (Session, error)
}

// Tx is the set of writes available inside a transaction.
type Tx interface {
	// CreateUser returns the new user's ID, or ErrEmailTaken when the address is
	// already registered, ignoring letter case.
	CreateUser(ctx context.Context, u NewUser) (string, error)
	CreateProfile(ctx context.Context, userID, fullName string) error
	CreateToken(ctx context.Context, t NewToken) error
	// DeleteUnusedTokens removes the user's tokens for purpose that have not been used.
	DeleteUnusedTokens(ctx context.Context, userID string, purpose TokenPurpose) error
	// FindTokenForUpdate locks the token with this hash until the transaction ends.
	// It returns ErrNotFound when there is none.
	FindTokenForUpdate(ctx context.Context, hash string, purpose TokenPurpose) (Token, error)
	MarkTokenUsed(ctx context.Context, tokenID string, at time.Time) error
	// MarkEmailVerified sets the verified time unless it is already set.
	MarkEmailVerified(ctx context.Context, userID string, at time.Time) error
	// FindSessionForUpdate locks the session whose current refresh token has this hash
	// until the transaction ends. It returns ErrNotFound when there is none.
	FindSessionForUpdate(ctx context.Context, hash string) (Session, error)
	// FindSessionByRetiredTokenForUpdate locks the session that once had a refresh token
	// with this hash and has since replaced it. It returns ErrNotFound when there is none.
	FindSessionByRetiredTokenForUpdate(ctx context.Context, hash string) (Session, error)
	// RotateSession gives the session a new refresh token hash and records oldHash as
	// retired at at.
	RotateSession(ctx context.Context, sessionID, oldHash, newHash string, at time.Time) error
	// RevokeSession ends the session at at, unless it has already ended.
	RevokeSession(ctx context.Context, sessionID string, at time.Time) error
}

// PasswordHasher turns a password into a salted hash (NFR-5) and checks a password
// against one.
type PasswordHasher interface {
	Hash(password string) (string, error)
	// Verify reports whether password matches encoded, a hash made by Hash.
	Verify(password, encoded string) (bool, error)
}

// AccessTokenSigner issues the short-lived access token given at login (FR-2.1).
type AccessTokenSigner interface {
	// Sign returns a token for userID issued at now, and the time it expires.
	Sign(userID string, now time.Time) (string, time.Time, error)
}

// LoginAttempts counts failed logins per account and holds account locks (FR-2.7). key
// names the account; the usecase passes the normalized email address, which may belong
// to no account.
type LoginAttempts interface {
	// RecordFailure adds a failed login and returns how many the current window holds.
	// A window starts at the first failure and lasts window; the first failure after it
	// starts a new one.
	RecordFailure(ctx context.Context, key string, window time.Duration) (int, error)
	// Reset forgets the account's failed logins.
	Reset(ctx context.Context, key string) error
	// Lock refuses logins for the account until until.
	Lock(ctx context.Context, key string, until time.Time) error
	// LockedUntil returns when the account's lock ends, or the zero time when it has none.
	LockedUntil(ctx context.Context, key string) (time.Time, error)
}

// Mailer sends email.
type Mailer interface {
	SendVerification(ctx context.Context, msg VerificationEmail) error
}
