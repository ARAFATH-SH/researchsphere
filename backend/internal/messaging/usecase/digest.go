package usecase

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/messaging/domain"
)

// The digest's rules (FR-16.11, docs/decisions.md).
const (
	// DigestDelay is how long a message stays unread before a digest counts it, and the
	// shortest time between two digests to one user.
	DigestDelay = 24 * time.Hour
	// DigestBatch is the most users one run sends a digest to; the rest wait for the next run.
	DigestBatch = 200
	// DigestTop is the most conversations a digest names.
	DigestTop = 5
)

// unsubscribePurpose is signed with every unsubscribe token, so no other signature made with
// the same key is a valid token, and a later version can change its meaning.
const unsubscribePurpose = "researchsphere:digest-off:v1:"

// Digests holds the user's settings and the unread message digest (FR-16.11).
type Digests struct {
	store  domain.DigestStore
	tx     domain.Transactor
	mailer domain.DigestMailer
	key    []byte
	now    func() time.Time
	logger *slog.Logger
}

// NewDigests returns the digest usecase. key signs the unsubscribe tokens (UNSUBSCRIBE_SECRET).
func NewDigests(store domain.DigestStore, tx domain.Transactor, mailer domain.DigestMailer, key []byte,
	now func() time.Time, logger *slog.Logger,
) *Digests {
	return &Digests{store: store, tx: tx, mailer: mailer, key: key, now: now, logger: logger}
}

// Settings returns the user's settings.
func (d *Digests) Settings(ctx context.Context, userID string) (domain.Settings, error) {
	return d.store.Settings(ctx, userID)
}

// UpdateSettings changes the settings u names. An update that names none is ErrNoSetting.
func (d *Digests) UpdateSettings(ctx context.Context, userID string, u domain.SettingsUpdate) (domain.Settings, error) {
	if u.UnreadMessageDigest == nil {
		return domain.Settings{}, domain.ErrNoSetting
	}
	return d.store.UpdateSettings(ctx, userID, u)
}

// UnsubscribeToken returns the token that turns the user's digest off. It never expires.
func (d *Digests) UnsubscribeToken(userID string) (string, error) {
	raw, err := hex.DecodeString(strings.ReplaceAll(userID, "-", ""))
	if err != nil || len(raw) != 16 {
		return "", fmt.Errorf("unsubscribe token: user ID is not a UUID")
	}
	enc := base64.RawURLEncoding
	return enc.EncodeToString(raw) + "." + enc.EncodeToString(d.sign(raw)), nil
}

// Unsubscribe turns the digest off for the user the token names. It never turns anything
// on. A token this server did not sign is ErrInvalidUnsubscribeToken; a valid one of a user
// who no longer exists does nothing.
func (d *Digests) Unsubscribe(ctx context.Context, token string) error {
	userID, ok := d.checkToken(token)
	if !ok {
		return domain.ErrInvalidUnsubscribeToken
	}
	return d.store.TurnOffDigest(ctx, userID)
}

func (d *Digests) sign(rawID []byte) []byte {
	mac := hmac.New(sha256.New, d.key)
	mac.Write([]byte(unsubscribePurpose))
	mac.Write(rawID)
	return mac.Sum(nil)
}

// checkToken returns the user ID a valid token names.
func (d *Digests) checkToken(token string) (string, bool) {
	idPart, sigPart, ok := strings.Cut(token, ".")
	if !ok || len(d.key) == 0 {
		return "", false
	}
	enc := base64.RawURLEncoding
	raw, err1 := enc.DecodeString(idPart)
	sig, err2 := enc.DecodeString(sigPart)
	if err1 != nil || err2 != nil || len(raw) != 16 || !hmac.Equal(sig, d.sign(raw)) {
		return "", false
	}
	h := hex.EncodeToString(raw)
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:], true
}

// DigestRun is what one run of the digest job did.
type DigestRun struct {
	Sent    int // digests sent
	Skipped int // candidates another worker held, or no longer due one
	Failed  int // candidates whose digest could not be built or sent; tried again next run
}

// SendDue sends a digest to at most DigestBatch users due one: verified, not suspended, with
// the digest on, no digest in the last DigestDelay, and messages left unread for DigestDelay
// that no digest counted yet. Each user is handled in their own transaction, which locks their
// digest row (other workers skip it), sends, then moves their watermark, so no message is in
// two digests. A failed send rolls back and is tried at the next run. The error is only for a
// failure to list the candidates.
func (d *Digests) SendDue(ctx context.Context) (DigestRun, error) {
	var run DigestRun
	cutoff := d.now().Add(-DigestDelay)
	ids, err := d.store.DigestCandidates(ctx, cutoff, DigestBatch)
	if err != nil || len(ids) == 0 {
		return run, err
	}
	if err := d.store.PrepareDigests(ctx, ids); err != nil {
		return run, err
	}
	for _, id := range ids {
		if ctx.Err() != nil {
			break
		}
		sent, err := d.sendOne(ctx, id, cutoff)
		switch {
		case err != nil:
			run.Failed++
			// No IDs: the error never holds message text (the store and mailer promise it).
			d.logger.ErrorContext(ctx, "unread message digest failed", slog.Any("error", err))
		case sent:
			run.Sent++
		default:
			run.Skipped++
		}
	}
	return run, nil
}

func (d *Digests) sendOne(ctx context.Context, userID string, cutoff time.Time) (bool, error) {
	var sent bool
	err := d.tx.InTx(ctx, func(ctx context.Context) error {
		rec, ok, err := d.store.ClaimDigest(ctx, userID, cutoff)
		if err != nil || !ok {
			return err
		}
		convs, err := d.store.DigestConversations(ctx, userID, rec.CoveredUntil, cutoff)
		if err != nil || len(convs) == 0 {
			return err
		}
		requests, err := d.store.PendingRequests(ctx, userID)
		if err != nil {
			return err
		}
		token, err := d.UnsubscribeToken(userID)
		if err != nil {
			return err
		}
		digest := buildDigest(rec, convs, requests, token)
		if err := d.mailer.SendDigest(ctx, digest); err != nil {
			return fmt.Errorf("send digest: %w", err)
		}
		if err := d.store.RecordDigest(ctx, domain.DigestRecord{
			UserID: userID, SentAt: d.now(), CoveredUntil: cutoff,
			Unread: digest.Unread, Conversations: len(convs), Requests: requests,
		}); err != nil {
			return err
		}
		sent = true
		return nil
	})
	return sent && err == nil, err
}

// buildDigest names the DigestTop conversations with the most unread messages; convs come
// most unread first.
func buildDigest(rec domain.DigestRecipient, convs []domain.DigestConversation, requests int, token string) domain.Digest {
	total := 0
	for _, c := range convs {
		total += c.Unread
	}
	top := convs[:min(len(convs), DigestTop)]
	return domain.Digest{
		To: rec.Email, FullName: rec.FullName, Unread: total,
		Top: append([]domain.DigestConversation(nil), top...), More: len(convs) - len(top),
		Requests: requests, UnsubscribeToken: token,
	}
}

// RunDigests runs SendDue now and then every interval until ctx ends, logging totals only.
func (d *Digests) RunDigests(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		d.RunOnce(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// RunOnce runs SendDue once and logs what it did.
func (d *Digests) RunOnce(ctx context.Context) DigestRun {
	run, err := d.SendDue(ctx)
	switch {
	case err != nil && !errors.Is(err, context.Canceled):
		d.logger.ErrorContext(ctx, "unread message digests failed", slog.Any("error", err))
	case run != DigestRun{}:
		d.logger.InfoContext(ctx, "unread message digests", slog.Int("sent", run.Sent),
			slog.Int("skipped", run.Skipped), slog.Int("failed", run.Failed))
	}
	return run
}
