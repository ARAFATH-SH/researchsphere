package domain

import (
	"context"
	"errors"
	"time"
)

// Settings are a user's account settings (FR-16.11). A user who never changed one has the
// defaults (DefaultSettings).
type Settings struct {
	// UnreadMessageDigest emails a digest of messages left unread for 24 hours.
	UnreadMessageDigest bool
}

// DefaultSettings are the settings of a user who never changed one.
var DefaultSettings = Settings{UnreadMessageDigest: true}

// SettingsUpdate changes the settings that are not nil.
type SettingsUpdate struct {
	UnreadMessageDigest *bool
}

// DigestRecipient is a user the digest job holds: their row is locked until the
// transaction ends, so no other worker sends them a digest meanwhile.
type DigestRecipient struct {
	Email    string
	FullName string
	// CoveredUntil is the previous digest's watermark: only messages created after it count.
	// Zero when the user never got a digest.
	CoveredUntil time.Time
}

// DigestConversation is one conversation with messages left unread, as the digest names it:
// the other person's name, or the group's name. It never holds message text (NFR-13).
type DigestConversation struct {
	Name   string
	Group  bool
	Unread int
}

// Digest is the content of one unread message digest email (FR-16.11). It holds counts and
// names only: never message text, attachment names or message IDs (NFR-13).
type Digest struct {
	To       string
	FullName string
	// Unread is the number of messages the digest covers.
	Unread int
	// Top are the conversations with the most of them, at most usecase.DigestTop.
	Top []DigestConversation
	// More is the number of other conversations with messages the digest covers.
	More int
	// Requests is the number of pending message requests to the user.
	Requests int
	// UnsubscribeToken turns the digest off without a login.
	UnsubscribeToken string
}

// DigestRecord is what is kept of a sent digest, for support.
type DigestRecord struct {
	UserID        string
	SentAt        time.Time
	CoveredUntil  time.Time
	Unread        int
	Conversations int
	Requests      int
}

// DigestStore reads and writes settings and the digest job's state. Every method runs in the
// transaction the context carries, if any. IDs that are not UUIDs are missing rows.
type DigestStore interface {
	// Settings returns the user's settings, the defaults when they never changed one.
	Settings(ctx context.Context, userID string) (Settings, error)
	// UpdateSettings applies u and returns the settings after it.
	UpdateSettings(ctx context.Context, userID string, u SettingsUpdate) (Settings, error)
	// TurnOffDigest turns the digest off. It does nothing when the user does not exist.
	TurnOffDigest(ctx context.Context, userID string) error
	// DigestCandidates returns at most limit users who may be due a digest at cutoff (24
	// hours ago): verified, not suspended, with the digest on, no digest sent after cutoff, and
	// a message that counts. Users never sent a digest come first, then the longest waiting.
	DigestCandidates(ctx context.Context, cutoff time.Time, limit int) ([]string, error)
	// PrepareDigests creates the digest rows the users lack. It must run outside the workers'
	// transactions (it commits on its own), so ClaimDigest never waits for another worker.
	PrepareDigests(ctx context.Context, userIDs []string) error
	// ClaimDigest locks the user's digest row in the context's transaction and returns them
	// when they are still due a digest at cutoff. ok is false, and nothing waits, when another
	// worker holds the row, the row is missing (PrepareDigests was not run), or the user is no
	// longer due one.
	ClaimDigest(ctx context.Context, userID string, cutoff time.Time) (r DigestRecipient, ok bool, err error)
	// DigestConversations returns the conversations with messages that count for the user,
	// created after coveredUntil (zero: no lower bound) and at most at cutoff, most unread
	// first. A message counts when it is someone else's text message in an active direct or
	// group conversation, from after the user joined, not deleted, after the user's read
	// marker, and no block stands between the user and its sender.
	DigestConversations(ctx context.Context, userID string, coveredUntil, cutoff time.Time) ([]DigestConversation, error)
	// PendingRequests counts the message requests waiting for the user's answer.
	PendingRequests(ctx context.Context, userID string) (int, error)
	// RecordDigest moves the user's watermark and keeps the record of the digest.
	RecordDigest(ctx context.Context, r DigestRecord) error
}

// DigestMailer sends a digest email.
type DigestMailer interface {
	SendDigest(ctx context.Context, d Digest) error
}

// Errors of the settings and the digest.
var (
	// ErrNoSetting is returned for a settings update that changes nothing.
	ErrNoSetting = errors.New("messaging: the update names no setting")
	// ErrInvalidUnsubscribeToken is returned for a token this server did not sign.
	ErrInvalidUnsubscribeToken = errors.New("messaging: invalid unsubscribe token")
)
