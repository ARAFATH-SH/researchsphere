package usecase

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"maps"
	"strconv"
	"strings"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/auth/domain"
)

// fakeUser is a stored user with its profile name.
type fakeUser struct {
	domain.User
	passwordHash string
}

// fakeToken is a stored token with its hash and purpose.
type fakeToken struct {
	domain.Token
	hash    string
	purpose domain.TokenPurpose
}

// fakeStore keeps users and tokens in maps. It compares email addresses in lower case,
// as the citext column does, and RunInTx undoes every write when fn fails.
type fakeStore struct {
	users  map[string]fakeUser  // by lower-case email
	tokens   map[string]fakeToken // by token ID
	sessions []domain.NewSession
	nextID   int

	failFind error // returned by the Find methods when set
	failTx   error // returned by every Tx write when set
	txCount  int
}

func newFakeStore() *fakeStore {
	return &fakeStore{users: map[string]fakeUser{}, tokens: map[string]fakeToken{}}
}

func (s *fakeStore) id() string {
	s.nextID++
	return "id-" + strconv.Itoa(s.nextID)
}

func (s *fakeStore) RunInTx(_ context.Context, fn func(domain.Tx) error) error {
	s.txCount++
	users, tokens, nextID := maps.Clone(s.users), maps.Clone(s.tokens), s.nextID
	if err := fn(fakeTx{s}); err != nil {
		s.users, s.tokens, s.nextID = users, tokens, nextID
		return err
	}
	return nil
}

func (s *fakeStore) FindUserByEmail(_ context.Context, email string) (domain.User, error) {
	if s.failFind != nil {
		return domain.User{}, s.failFind
	}
	u, ok := s.users[strings.ToLower(email)]
	if !ok {
		return domain.User{}, domain.ErrNotFound
	}
	return u.User, nil
}

func (s *fakeStore) FindCredentialsByEmail(_ context.Context, email string) (domain.Credentials, error) {
	if s.failFind != nil {
		return domain.Credentials{}, s.failFind
	}
	u, ok := s.users[strings.ToLower(email)]
	if !ok {
		return domain.Credentials{}, domain.ErrNotFound
	}
	return domain.Credentials{User: u.User, PasswordHash: u.passwordHash}, nil
}

func (s *fakeStore) FindUserByID(_ context.Context, id string) (domain.User, error) {
	if s.failFind != nil {
		return domain.User{}, s.failFind
	}
	_, u, ok := s.userByID(id)
	if !ok {
		return domain.User{}, domain.ErrNotFound
	}
	return u.User, nil
}

func (s *fakeStore) CreateSession(_ context.Context, sess domain.NewSession) error {
	if s.failTx != nil {
		return s.failTx
	}
	s.sessions = append(s.sessions, sess)
	return nil
}

func (s *fakeStore) userByID(id string) (string, fakeUser, bool) {
	for k, u := range s.users {
		if u.ID == id {
			return k, u, true
		}
	}
	return "", fakeUser{}, false
}

// tokensFor returns the user's stored tokens.
func (s *fakeStore) tokensFor(userID string) []fakeToken {
	var out []fakeToken
	for _, t := range s.tokens {
		if t.UserID == userID {
			out = append(out, t)
		}
	}
	return out
}

type fakeTx struct{ s *fakeStore }

func (tx fakeTx) CreateUser(_ context.Context, u domain.NewUser) (string, error) {
	if tx.s.failTx != nil {
		return "", tx.s.failTx
	}
	key := strings.ToLower(u.Email)
	if _, ok := tx.s.users[key]; ok {
		return "", domain.ErrEmailTaken
	}
	id := tx.s.id()
	tx.s.users[key] = fakeUser{User: domain.User{ID: id, Email: u.Email}, passwordHash: u.PasswordHash}
	return id, nil
}

func (tx fakeTx) CreateProfile(_ context.Context, userID, fullName string) error {
	k, u, ok := tx.s.userByID(userID)
	if !ok {
		return errors.New("no such user")
	}
	u.FullName = fullName
	tx.s.users[k] = u
	return nil
}

func (tx fakeTx) CreateToken(_ context.Context, t domain.NewToken) error {
	if tx.s.failTx != nil {
		return tx.s.failTx
	}
	id := tx.s.id()
	tx.s.tokens[id] = fakeToken{
		Token:   domain.Token{ID: id, UserID: t.UserID, ExpiresAt: t.ExpiresAt},
		hash:    t.Hash,
		purpose: t.Purpose,
	}
	return nil
}

func (tx fakeTx) DeleteUnusedTokens(_ context.Context, userID string, purpose domain.TokenPurpose) error {
	if tx.s.failTx != nil {
		return tx.s.failTx
	}
	for id, t := range tx.s.tokens {
		if t.UserID == userID && t.purpose == purpose && t.UsedAt == nil {
			delete(tx.s.tokens, id)
		}
	}
	return nil
}

func (tx fakeTx) FindTokenForUpdate(_ context.Context, hash string, purpose domain.TokenPurpose) (domain.Token, error) {
	for _, t := range tx.s.tokens {
		if t.hash == hash && t.purpose == purpose {
			return t.Token, nil
		}
	}
	return domain.Token{}, domain.ErrNotFound
}

func (tx fakeTx) MarkTokenUsed(_ context.Context, tokenID string, at time.Time) error {
	t := tx.s.tokens[tokenID]
	t.UsedAt = &at
	tx.s.tokens[tokenID] = t
	return nil
}

func (tx fakeTx) MarkEmailVerified(_ context.Context, userID string, at time.Time) error {
	k, u, ok := tx.s.userByID(userID)
	if !ok {
		return errors.New("no such user")
	}
	if u.EmailVerifiedAt == nil {
		u.EmailVerifiedAt = &at
	}
	tx.s.users[k] = u
	return nil
}

// fakeHasher marks the password so tests can see it was hashed, and counts the
// passwords it checked.
type fakeHasher struct {
	err      error
	verified int
}

func (h *fakeHasher) Hash(password string) (string, error) {
	if h.err != nil {
		return "", h.err
	}
	return "hashed:" + password, nil
}

func (h *fakeHasher) Verify(password, encoded string) (bool, error) {
	h.verified++
	if h.err != nil {
		return false, h.err
	}
	return encoded == "hashed:"+password, nil
}

// fakeSigner issues readable access tokens.
type fakeSigner struct{ err error }

func (s fakeSigner) Sign(userID string, now time.Time) (string, time.Time, error) {
	if s.err != nil {
		return "", time.Time{}, s.err
	}
	return "access-for-" + userID, now.Add(15 * time.Minute), nil
}

// fakeMailer records the messages it was asked to send.
type fakeMailer struct {
	sent []domain.VerificationEmail
	err  error
}

func (m *fakeMailer) SendVerification(_ context.Context, msg domain.VerificationEmail) error {
	if m.err != nil {
		return m.err
	}
	m.sent = append(m.sent, msg)
	return nil
}

// clock is a settable time source.
type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

var start = time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)

// env holds a Service and its fakes.
type env struct {
	svc    *Service
	store  *fakeStore
	hasher *fakeHasher
	mailer *fakeMailer
	clock  *clock
	logs   *bytes.Buffer
}

func newEnv() *env {
	e := &env{store: newFakeStore(), hasher: &fakeHasher{}, mailer: &fakeMailer{}, clock: &clock{t: start}, logs: &bytes.Buffer{}}
	logger := slog.New(slog.NewTextHandler(e.logs, nil))
	e.svc = New(e.store, e.hasher, e.mailer, fakeSigner{}, Config{AppBaseURL: "https://rs.example/"}, e.clock.now, logger)
	return e
}

// tokenFromLink returns the token in a verification link.
func tokenFromLink(link string) string {
	_, token, _ := strings.Cut(link, "#token=")
	return token
}
