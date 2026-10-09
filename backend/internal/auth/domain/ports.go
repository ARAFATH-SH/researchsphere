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
}

// PasswordHasher turns a password into a salted hash (NFR-5).
type PasswordHasher interface {
	Hash(password string) (string, error)
}

// Mailer sends email.
type Mailer interface {
	SendVerification(ctx context.Context, msg VerificationEmail) error
}
