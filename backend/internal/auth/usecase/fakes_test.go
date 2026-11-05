package usecase

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/auth/domain"
)

// fakeUser is a stored user with its profile name.
type fakeUser struct {
	domain.User
	passwordHash     string
	suspensionReason *string
}

// fakeToken is a stored token with its hash and purpose.
type fakeToken struct {
	domain.Token
	hash    string
	purpose domain.TokenPurpose
}

// fakeSession is a stored session with the hash of its current refresh token.
type fakeSession struct {
	domain.Session
	TokenHash string
}

// fakeStore keeps users, tokens and sessions in memory. It compares email addresses in lower case,
// as the citext column does, and RunInTx undoes every write when fn fails.
type fakeStore struct {
	users    map[string]fakeUser  // by lower-case email
	tokens   map[string]fakeToken // by token ID
	sessions []fakeSession
	retired  map[string]string // session ID by retired refresh token hash
	projects map[string]fakeProject
	pubs     map[string][]string // user IDs linked, by publication ID
	nextID   int

	listed [][2]int // limit and offset of each ListUsers call

	failFind error // returned by the Find, List and Count methods when set
	failTx   error // returned by every Tx write when set
	txCount  int
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		users: map[string]fakeUser{}, tokens: map[string]fakeToken{}, retired: map[string]string{},
		projects: map[string]fakeProject{}, pubs: map[string][]string{},
	}
}

// fakeProject is a project with the roles of its members, by user ID.
type fakeProject struct {
	title   string
	members map[string]string
}

// addProject stores a project owned by ownerID with the other members given, as editors.
func (s *fakeStore) addProject(title, ownerID string, others ...string) string {
	id := s.id()
	members := map[string]string{ownerID: "owner"}
	for _, o := range others {
		members[o] = "editor"
	}
	s.projects[id] = fakeProject{title: title, members: members}
	return id
}

// ownedProjects returns the IDs of the projects userID owns, with those that have other
// members marked.
func (s *fakeStore) ownedProjects(userID string) map[string]bool {
	out := map[string]bool{}
	for id, p := range s.projects {
		if p.members[userID] == "owner" {
			out[id] = len(p.members) > 1
		}
	}
	return out
}

func (s *fakeStore) id() string {
	s.nextID++
	return "id-" + strconv.Itoa(s.nextID)
}

func (s *fakeStore) RunInTx(_ context.Context, fn func(domain.Tx) error) error {
	s.txCount++
	users, tokens, nextID := maps.Clone(s.users), maps.Clone(s.tokens), s.nextID
	sessions, retired := slices.Clone(s.sessions), maps.Clone(s.retired)
	projects, pubs := maps.Clone(s.projects), maps.Clone(s.pubs)
	if err := fn(fakeTx{s}); err != nil {
		s.users, s.tokens, s.nextID = users, tokens, nextID
		s.sessions, s.retired = sessions, retired
		s.projects, s.pubs = projects, pubs
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
	return domain.Credentials{User: u.User, PasswordHash: u.passwordHash, SuspensionReason: u.suspensionReason}, nil
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

// ListUsers returns the users newest first; a later ID is a newer user.
func (s *fakeStore) ListUsers(_ context.Context, limit, offset int) ([]domain.User, error) {
	if s.failFind != nil {
		return nil, s.failFind
	}
	s.listed = append(s.listed, [2]int{limit, offset})
	users := make([]domain.User, 0, len(s.users))
	for _, u := range s.users {
		users = append(users, u.User)
	}
	slices.SortFunc(users, func(a, b domain.User) int { return idNumber(b.ID) - idNumber(a.ID) })
	if offset >= len(users) {
		return nil, nil
	}
	return users[offset:min(offset+limit, len(users))], nil
}

func (s *fakeStore) CountUsers(context.Context) (int, error) {
	if s.failFind != nil {
		return 0, s.failFind
	}
	return len(s.users), nil
}

// idNumber returns n for an ID "id-n" made by fakeStore.id.
func idNumber(id string) int {
	n, _ := strconv.Atoi(strings.TrimPrefix(id, "id-"))
	return n
}

func (s *fakeStore) FindCredentialsByID(_ context.Context, id string) (domain.Credentials, error) {
	if s.failFind != nil {
		return domain.Credentials{}, s.failFind
	}
	_, u, ok := s.userByID(id)
	if !ok {
		return domain.Credentials{}, domain.ErrNotFound
	}
	return domain.Credentials{User: u.User, PasswordHash: u.passwordHash}, nil
}

func (s *fakeStore) CreateSession(_ context.Context, sess domain.NewSession) error {
	if s.failTx != nil {
		return s.failTx
	}
	s.sessions = append(s.sessions, fakeSession{
		Session:   domain.Session{ID: s.id(), UserID: sess.UserID, ExpiresAt: sess.ExpiresAt},
		TokenHash: sess.TokenHash,
	})
	return nil
}

func (s *fakeStore) RevokeSessionByToken(_ context.Context, hash string, at time.Time) (domain.Session, error) {
	if s.failTx != nil {
		return domain.Session{}, s.failTx
	}
	for i, sess := range s.sessions {
		if sess.TokenHash == hash && sess.RevokedAt == nil {
			s.sessions[i].RevokedAt = &at
			return s.sessions[i].Session, nil
		}
	}
	return domain.Session{}, domain.ErrNotFound
}

// sessionIndex returns the position of the session with id, or -1.
func (s *fakeStore) sessionIndex(id string) int {
	return slices.IndexFunc(s.sessions, func(sess fakeSession) bool { return sess.ID == id })
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

// ClearExpiredSuspension applies the query's condition.
func (s *fakeStore) ClearExpiredSuspension(_ context.Context, userID string, now time.Time) error {
	k, u, ok := s.userByID(userID)
	if ok && u.SuspendedUntil != nil && !u.SuspendedUntil.After(now) {
		u.SuspendedAt, u.SuspendedUntil, u.suspensionReason = nil, nil, nil
		s.users[k] = u
	}
	return nil
}

func (s *fakeStore) LockUser(_ context.Context, userID string) (domain.User, error) {
	_, u, ok := s.userByID(userID)
	if !ok {
		return domain.User{}, domain.ErrNotFound
	}
	return u.User, nil
}

func (s *fakeStore) Suspend(_ context.Context, userID string, at time.Time, until *time.Time, reason string) error {
	k, u, ok := s.userByID(userID)
	if !ok {
		return domain.ErrNotFound
	}
	u.SuspendedAt, u.SuspendedUntil, u.suspensionReason = &at, until, &reason
	s.users[k] = u
	return nil
}

func (s *fakeStore) Restore(_ context.Context, userID string) error {
	k, u, ok := s.userByID(userID)
	if !ok {
		return domain.ErrNotFound
	}
	u.SuspendedAt, u.SuspendedUntil, u.suspensionReason = nil, nil, nil
	s.users[k] = u
	return nil
}

type fakeTx struct{ s *fakeStore }

func (tx fakeTx) ClearExpiredSuspension(ctx context.Context, userID string, now time.Time) error {
	return tx.s.ClearExpiredSuspension(ctx, userID, now)
}

func (tx fakeTx) CreateUser(_ context.Context, u domain.NewUser) (string, error) {
	if tx.s.failTx != nil {
		return "", tx.s.failTx
	}
	key := strings.ToLower(u.Email)
	if _, ok := tx.s.users[key]; ok {
		return "", domain.ErrEmailTaken
	}
	id := tx.s.id()
	// "researcher" is the users.role column default.
	tx.s.users[key] = fakeUser{User: domain.User{ID: id, Email: u.Email, Role: "researcher"}, passwordHash: u.PasswordHash}
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
			tok := t.Token
			_, u, _ := tx.s.userByID(t.UserID)
			tok.UserSuspendedAt = u.SuspendedAt // as the query's join with users
			tok.UserSuspendedUntil = u.SuspendedUntil
			return tok, nil
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

func (tx fakeTx) FindSessionForUpdate(_ context.Context, hash string) (domain.Session, error) {
	for _, sess := range tx.s.sessions {
		if sess.TokenHash == hash {
			found := sess.Session
			_, u, _ := tx.s.userByID(sess.UserID)
			found.UserSuspendedAt = u.SuspendedAt // as the query's join with users
			found.UserSuspendedUntil = u.SuspendedUntil
			found.UserRole = u.Role
			return found, nil
		}
	}
	return domain.Session{}, domain.ErrNotFound
}

func (tx fakeTx) FindSessionByRetiredTokenForUpdate(_ context.Context, hash string) (domain.Session, error) {
	id, ok := tx.s.retired[hash]
	if !ok {
		return domain.Session{}, domain.ErrNotFound
	}
	return tx.s.sessions[tx.s.sessionIndex(id)].Session, nil
}

func (tx fakeTx) RotateSession(_ context.Context, sessionID, oldHash, newHash string, _ time.Time) error {
	if tx.s.failTx != nil {
		return tx.s.failTx
	}
	i := tx.s.sessionIndex(sessionID)
	if i < 0 || tx.s.sessions[i].TokenHash != oldHash {
		return errors.New("no such session")
	}
	tx.s.sessions[i].TokenHash = newHash
	tx.s.retired[oldHash] = sessionID
	return nil
}

func (tx fakeTx) RevokeSession(_ context.Context, sessionID string, at time.Time) error {
	if tx.s.failTx != nil {
		return tx.s.failTx
	}
	i := tx.s.sessionIndex(sessionID)
	if i < 0 {
		return errors.New("no such session")
	}
	if tx.s.sessions[i].RevokedAt == nil {
		tx.s.sessions[i].RevokedAt = &at
	}
	return nil
}

func (tx fakeTx) UpdatePasswordHash(_ context.Context, userID, hash string) error {
	if tx.s.failTx != nil {
		return tx.s.failTx
	}
	k, u, ok := tx.s.userByID(userID)
	if !ok {
		return errors.New("no such user")
	}
	u.passwordHash = hash
	tx.s.users[k] = u
	return nil
}

func (tx fakeTx) RevokeUserSessions(_ context.Context, userID string, at time.Time) error {
	if tx.s.failTx != nil {
		return tx.s.failTx
	}
	for i, sess := range tx.s.sessions {
		if sess.UserID == userID && sess.RevokedAt == nil {
			tx.s.sessions[i].RevokedAt = &at
		}
	}
	return nil
}

func (tx fakeTx) RevokeOtherUserSessions(_ context.Context, userID, keepSessionID string, at time.Time) error {
	if tx.s.failTx != nil {
		return tx.s.failTx
	}
	for i, sess := range tx.s.sessions {
		if sess.UserID == userID && sess.ID != keepSessionID && sess.RevokedAt == nil {
			tx.s.sessions[i].RevokedAt = &at
		}
	}
	return nil
}

func (tx fakeTx) LockOwnedProjects(context.Context, string) error { return tx.s.failTx }

func (tx fakeTx) FindSharedOwnedProjects(_ context.Context, userID string) ([]domain.ProjectRef, error) {
	if tx.s.failTx != nil {
		return nil, tx.s.failTx
	}
	var out []domain.ProjectRef
	for id, shared := range tx.s.ownedProjects(userID) {
		if shared {
			out = append(out, domain.ProjectRef{ID: id, Title: tx.s.projects[id].title})
		}
	}
	slices.SortFunc(out, func(a, b domain.ProjectRef) int { return strings.Compare(a.Title, b.Title) })
	return out, nil
}

func (tx fakeTx) DeleteSoloProjects(_ context.Context, userID string) (int64, error) {
	if tx.s.failTx != nil {
		return 0, tx.s.failTx
	}
	var n int64
	for id, shared := range tx.s.ownedProjects(userID) {
		if !shared {
			delete(tx.s.projects, id)
			n++
		}
	}
	return n, nil
}

func (tx fakeTx) DeleteUserPublications(_ context.Context, userID string) (int64, error) {
	if tx.s.failTx != nil {
		return 0, tx.s.failTx
	}
	var n int64
	for id, users := range tx.s.pubs {
		if !slices.Contains(users, userID) {
			continue
		}
		left := slices.DeleteFunc(slices.Clone(users), func(u string) bool { return u == userID })
		if len(left) == 0 {
			delete(tx.s.pubs, id)
			n++
			continue
		}
		tx.s.pubs[id] = left
	}
	return n, nil
}

// DeleteUser removes the user and, as the database cascades do, the user's sessions,
// tokens and project memberships.
func (tx fakeTx) DeleteUser(_ context.Context, userID string) error {
	if tx.s.failTx != nil {
		return tx.s.failTx
	}
	k, _, ok := tx.s.userByID(userID)
	if !ok {
		return domain.ErrNotFound
	}
	delete(tx.s.users, k)
	tx.s.sessions = slices.DeleteFunc(tx.s.sessions, func(sess fakeSession) bool { return sess.UserID == userID })
	maps.DeleteFunc(tx.s.tokens, func(_ string, t fakeToken) bool { return t.UserID == userID })
	for id, p := range tx.s.projects {
		if _, ok := p.members[userID]; ok {
			members := maps.Clone(p.members)
			delete(members, userID)
			tx.s.projects[id] = fakeProject{title: p.title, members: members}
		}
	}
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

// fakeSigner issues readable access tokens that name the user and the role.
type fakeSigner struct{ err error }

func (s fakeSigner) Sign(userID, role string, now time.Time) (string, time.Time, error) {
	if s.err != nil {
		return "", time.Time{}, s.err
	}
	return "access-for-" + userID + "-as-" + role, now.Add(15 * time.Minute), nil
}

// fakeMailer records the messages it was asked to send.
type fakeMailer struct {
	sent   []domain.VerificationEmail
	resets []domain.PasswordResetEmail
	err    error
}

func (m *fakeMailer) SendVerification(_ context.Context, msg domain.VerificationEmail) error {
	if m.err != nil {
		return m.err
	}
	m.sent = append(m.sent, msg)
	return nil
}

func (m *fakeMailer) SendPasswordReset(_ context.Context, msg domain.PasswordResetEmail) error {
	if m.err != nil {
		return m.err
	}
	m.resets = append(m.resets, msg)
	return nil
}

// fakeAttempts keeps failed-login windows and locks in memory, timed by the test clock,
// as the Redis adapter keeps them with key expiry.
type fakeAttempts struct {
	clock    *clock
	failures map[string]fakeWindow
	locks    map[string]time.Time
	err      error // returned by every method when set
	calls    int   // method calls made
}

// fakeWindow is the failures counted in a window that ends at end.
type fakeWindow struct {
	count int
	end   time.Time
}

func newFakeAttempts(c *clock) *fakeAttempts {
	return &fakeAttempts{clock: c, failures: map[string]fakeWindow{}, locks: map[string]time.Time{}}
}

func (a *fakeAttempts) RecordFailure(_ context.Context, key string, window time.Duration) (int, error) {
	a.calls++
	if a.err != nil {
		return 0, a.err
	}
	w, ok := a.failures[key]
	if !ok || !a.clock.t.Before(w.end) {
		w = fakeWindow{end: a.clock.t.Add(window)}
	}
	w.count++
	a.failures[key] = w
	return w.count, nil
}

func (a *fakeAttempts) Reset(_ context.Context, key string) error {
	a.calls++
	if a.err != nil {
		return a.err
	}
	delete(a.failures, key)
	return nil
}

func (a *fakeAttempts) Lock(_ context.Context, key string, until time.Time) error {
	a.calls++
	if a.err != nil {
		return a.err
	}
	a.locks[key] = until
	return nil
}

func (a *fakeAttempts) LockedUntil(_ context.Context, key string) (time.Time, error) {
	a.calls++
	if a.err != nil {
		return time.Time{}, a.err
	}
	until, ok := a.locks[key]
	if !ok || !a.clock.t.Before(until) {
		return time.Time{}, nil // the key has expired
	}
	return until, nil
}

// clock is a settable time source.
type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

var start = time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)

// env holds a Service and its fakes.
type env struct {
	svc      *Service
	store    *fakeStore
	attempts *fakeAttempts
	hasher   *fakeHasher
	mailer   *fakeMailer
	clock    *clock
	logs     *bytes.Buffer
}

func newEnv() *env {
	e := &env{store: newFakeStore(), hasher: &fakeHasher{}, mailer: &fakeMailer{}, clock: &clock{t: start}, logs: &bytes.Buffer{}}
	e.attempts = newFakeAttempts(e.clock)
	logger := slog.New(slog.NewTextHandler(e.logs, nil))
	e.svc = New(e.store, e.hasher, e.mailer, fakeSigner{}, e.attempts, Config{AppBaseURL: "https://rs.example/"}, e.clock.now, logger)
	return e
}

// tokenFromLink returns the token in a verification or reset link.
func tokenFromLink(link string) string {
	_, token, _ := strings.Cut(link, "#token=")
	return token
}
