package usecase

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/messaging/domain"
)

const (
	digestUser  = "0190a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5b"
	digestOther = "0190a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5c"
)

var digestKey = []byte("test-unsubscribe-secret-0123456789ab")

// fakeDigestStore keeps settings and the digest state in memory. A user is a candidate while
// they have conversations; claimed users are locked until the fake transaction ends.
type fakeDigestStore struct {
	settings  map[string]bool
	convs     map[string][]domain.DigestConversation
	requests  int
	claimable map[string]bool
	records   []domain.DigestRecord
	turnedOff []string
	failClaim error
	prepared  []string
}

func newFakeDigestStore() *fakeDigestStore {
	return &fakeDigestStore{settings: map[string]bool{}, convs: map[string][]domain.DigestConversation{}, claimable: map[string]bool{}}
}

func (s *fakeDigestStore) Settings(_ context.Context, id string) (domain.Settings, error) {
	on, ok := s.settings[id]
	if !ok {
		return domain.DefaultSettings, nil
	}
	return domain.Settings{UnreadMessageDigest: on}, nil
}

func (s *fakeDigestStore) UpdateSettings(_ context.Context, id string, u domain.SettingsUpdate) (domain.Settings, error) {
	s.settings[id] = *u.UnreadMessageDigest
	return domain.Settings{UnreadMessageDigest: *u.UnreadMessageDigest}, nil
}

func (s *fakeDigestStore) TurnOffDigest(_ context.Context, id string) error {
	s.turnedOff = append(s.turnedOff, id)
	s.settings[id] = false
	return nil
}

func (s *fakeDigestStore) DigestCandidates(_ context.Context, _ time.Time, limit int) ([]string, error) {
	var ids []string
	for id := range s.convs {
		ids = append(ids, id)
	}
	return ids[:min(len(ids), limit)], nil
}

func (s *fakeDigestStore) PrepareDigests(_ context.Context, ids []string) error {
	s.prepared = append(s.prepared, ids...)
	return nil
}

func (s *fakeDigestStore) ClaimDigest(_ context.Context, id string, _ time.Time) (domain.DigestRecipient, bool, error) {
	if s.failClaim != nil {
		return domain.DigestRecipient{}, false, s.failClaim
	}
	if !s.claimable[id] {
		return domain.DigestRecipient{}, false, nil
	}
	return domain.DigestRecipient{Email: id + "@example.org", FullName: "Ada"}, true, nil
}

func (s *fakeDigestStore) DigestConversations(_ context.Context, id string, _, _ time.Time) ([]domain.DigestConversation, error) {
	return s.convs[id], nil
}

func (s *fakeDigestStore) PendingRequests(context.Context, string) (int, error) {
	return s.requests, nil
}

func (s *fakeDigestStore) RecordDigest(_ context.Context, r domain.DigestRecord) error {
	s.records = append(s.records, r)
	return nil
}

// digestTx runs fn and drops the records it wrote when fn fails, like a rollback.
type digestTx struct{ store *fakeDigestStore }

func (t digestTx) InTx(ctx context.Context, fn func(context.Context) error) error {
	n := len(t.store.records)
	if err := fn(ctx); err != nil {
		t.store.records = t.store.records[:n]
		return err
	}
	return nil
}

type fakeDigestMailer struct {
	sent []domain.Digest
	err  error
}

func (m *fakeDigestMailer) SendDigest(_ context.Context, d domain.Digest) error {
	if m.err != nil {
		return m.err
	}
	m.sent = append(m.sent, d)
	return nil
}

var digestNow = time.Date(2026, 11, 14, 9, 0, 0, 0, time.UTC)

func newTestDigests(store *fakeDigestStore, mailer *fakeDigestMailer) *Digests {
	return NewDigests(store, digestTx{store}, mailer, digestKey, func() time.Time { return digestNow }, slog.New(slog.DiscardHandler))
}

func TestUpdateSettingsNeedsASetting(t *testing.T) {
	d := newTestDigests(newFakeDigestStore(), &fakeDigestMailer{})
	if _, err := d.UpdateSettings(t.Context(), digestUser, domain.SettingsUpdate{}); !errors.Is(err, domain.ErrNoSetting) {
		t.Fatalf("UpdateSettings(empty) error = %v, want ErrNoSetting", err)
	}
	off := false
	s, err := d.UpdateSettings(t.Context(), digestUser, domain.SettingsUpdate{UnreadMessageDigest: &off})
	if err != nil || s.UnreadMessageDigest {
		t.Fatalf("UpdateSettings(off) = %+v, %v", s, err)
	}
}

func TestUnsubscribeToken(t *testing.T) {
	store := newFakeDigestStore()
	d := newTestDigests(store, &fakeDigestMailer{})
	token, err := d.UnsubscribeToken(digestUser)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(token, digestUser) || len(token) > maxTestTokenLen {
		t.Errorf("token %q shows the raw ID or is too long", token)
	}
	other, _ := d.UnsubscribeToken(digestOther)
	idPart, sigPart, _ := strings.Cut(token, ".")
	otherID, _, _ := strings.Cut(other, ".")
	wrongKey, _ := NewDigests(store, digestTx{store}, &fakeDigestMailer{}, []byte("another-key-of-at-least-32-bytes!!"), time.Now, slog.New(slog.DiscardHandler)).UnsubscribeToken(digestUser)

	for name, bad := range map[string]string{
		"empty":               "",
		"no signature":        idPart,
		"tampered signature":  idPart + "." + flipFirst(sigPart),
		"another user's ID":   otherID + "." + sigPart,
		"signed with another": wrongKey,
		"not base64":          "!!!." + sigPart,
		"short ID":            idPart[:10] + "." + sigPart,
		"extra part":          token + ".x",
	} {
		t.Run(name, func(t *testing.T) {
			if err := d.Unsubscribe(t.Context(), bad); !errors.Is(err, domain.ErrInvalidUnsubscribeToken) {
				t.Errorf("Unsubscribe(%q) error = %v, want ErrInvalidUnsubscribeToken", bad, err)
			}
		})
	}
	if len(store.turnedOff) != 0 {
		t.Fatalf("a bad token turned off %v", store.turnedOff)
	}

	if err := d.Unsubscribe(t.Context(), token); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(store.turnedOff) != "["+digestUser+"]" {
		t.Errorf("turned off %v, want [%s]", store.turnedOff, digestUser)
	}
}

// A token only ever turns the digest off: unsubscribing twice keeps it off.
func TestUnsubscribeOnlyTurnsOff(t *testing.T) {
	store := newFakeDigestStore()
	d := newTestDigests(store, &fakeDigestMailer{})
	token, _ := d.UnsubscribeToken(digestUser)
	for range 2 {
		if err := d.Unsubscribe(t.Context(), token); err != nil {
			t.Fatal(err)
		}
		if s, _ := d.Settings(t.Context(), digestUser); s.UnreadMessageDigest {
			t.Fatal("the digest is on after unsubscribing")
		}
	}
}

// The signature is single-purpose: the purpose label is part of it.
func TestUnsubscribeTokenPurpose(t *testing.T) {
	d := newTestDigests(newFakeDigestStore(), &fakeDigestMailer{})
	raw := []byte{0x01, 0x90, 0xa1, 0xb2, 0xc3, 0xd4, 0x7e, 0x5f, 0x8a, 0x9b, 0x0c, 0x1d, 0x2e, 0x3f, 0x4a, 0x5b}
	got := d.sign(raw)
	mac := newMAC(digestKey, raw) // the same key without the purpose label
	if string(got) == string(mac) {
		t.Fatal("the signature does not depend on the purpose label")
	}
}

func TestSendDueBuildsTheDigest(t *testing.T) {
	store := newFakeDigestStore()
	var convs []domain.DigestConversation
	for i := range 7 {
		convs = append(convs, domain.DigestConversation{Name: fmt.Sprintf("Person %d", i), Unread: 10 - i})
	}
	store.convs[digestUser] = convs
	store.claimable[digestUser] = true
	store.requests = 2
	mailer := &fakeDigestMailer{}

	run, err := newTestDigests(store, mailer).SendDue(t.Context())
	if err != nil || run != (DigestRun{Sent: 1}) {
		t.Fatalf("SendDue() = %+v, %v", run, err)
	}
	if len(mailer.sent) != 1 {
		t.Fatalf("sent %d digests", len(mailer.sent))
	}
	if !slices.Equal(store.prepared, []string{digestUser}) {
		t.Errorf("prepared %v, want the candidates", store.prepared)
	}
	got := mailer.sent[0]
	if got.Unread != 10+9+8+7+6+5+4 || len(got.Top) != DigestTop || got.Top[0].Name != "Person 0" || got.More != 2 || got.Requests != 2 {
		t.Errorf("digest = %+v", got)
	}
	if got.To != digestUser+"@example.org" || got.UnsubscribeToken == "" {
		t.Errorf("digest recipient or token missing: %+v", got)
	}
	want := domain.DigestRecord{UserID: digestUser, SentAt: digestNow, CoveredUntil: digestNow.Add(-DigestDelay), Unread: got.Unread, Conversations: 7, Requests: 2}
	if len(store.records) != 1 || store.records[0] != want {
		t.Errorf("records = %+v, want [%+v]", store.records, want)
	}
}

func TestSendDueSkipsAndFails(t *testing.T) {
	t.Run("another worker holds the user", func(t *testing.T) {
		store := newFakeDigestStore()
		store.convs[digestUser] = []domain.DigestConversation{{Name: "A", Unread: 1}}
		mailer := &fakeDigestMailer{}
		run, _ := newTestDigests(store, mailer).SendDue(t.Context())
		if run != (DigestRun{Skipped: 1}) || len(mailer.sent) != 0 || len(store.records) != 0 {
			t.Errorf("run = %+v, sent %d, records %d", run, len(mailer.sent), len(store.records))
		}
	})
	t.Run("nothing left to count", func(t *testing.T) {
		store := newFakeDigestStore()
		store.convs[digestUser] = nil
		store.claimable[digestUser] = true
		mailer := &fakeDigestMailer{}
		run, _ := newTestDigests(store, mailer).SendDue(t.Context())
		if run != (DigestRun{Skipped: 1}) || len(mailer.sent) != 0 || len(store.records) != 0 {
			t.Errorf("run = %+v, sent %d, records %d", run, len(mailer.sent), len(store.records))
		}
	})
	t.Run("a failed send records nothing", func(t *testing.T) {
		store := newFakeDigestStore()
		store.convs[digestUser] = []domain.DigestConversation{{Name: "A", Unread: 1}}
		store.claimable[digestUser] = true
		mailer := &fakeDigestMailer{err: errors.New("smtp down")}
		run, err := newTestDigests(store, mailer).SendDue(t.Context())
		if err != nil || run != (DigestRun{Failed: 1}) || len(store.records) != 0 {
			t.Errorf("run = %+v, %v, records %d", run, err, len(store.records))
		}
	})
	t.Run("a store error fails one user, not the run", func(t *testing.T) {
		store := newFakeDigestStore()
		store.convs[digestUser] = []domain.DigestConversation{{Name: "A", Unread: 1}}
		store.failClaim = errors.New("db down")
		run, err := newTestDigests(store, &fakeDigestMailer{}).SendDue(t.Context())
		if err != nil || run != (DigestRun{Failed: 1}) {
			t.Errorf("run = %+v, %v", run, err)
		}
	})
}
