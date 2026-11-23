package usecase

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/project/domain"
)

// Real-time editing (slice 22): tickets, access checks, the REST rule while a file is live,
// and what the sync server may store.

// memTickets is the ticket store in memory, with expiry by the test's clock.
type memTickets struct {
	mu   sync.Mutex
	now  func() time.Time
	byID map[string]memTicket
	err  error
}

type memTicket struct {
	t       domain.CollabTicket
	expires time.Time
}

func (m *memTickets) PutTicket(_ context.Context, key string, t domain.CollabTicket, ttl time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return m.err
	}
	m.byID[key] = memTicket{t: t, expires: m.now().Add(ttl)}
	return nil
}

func (m *memTickets) TakeTicket(_ context.Context, key string) (domain.CollabTicket, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.byID[key]
	delete(m.byID, key)
	if !ok || !m.now().Before(t.expires) {
		return domain.CollabTicket{}, false, nil
	}
	return t.t, true, nil
}

// memLive marks files live; err makes every check fail.
type memLive struct {
	live map[string]bool
	err  error
}

func (m *memLive) LiveSession(_ context.Context, fileID string) (bool, error) {
	if m.err != nil {
		return false, m.err
	}
	return m.live[fileID], nil
}

// memCollab answers the collab store from memFiles and the project store's members.
type memCollab struct {
	files     *memFiles
	suspended map[string]bool
	stored    []domain.CollabState
}

func (m *memCollab) CollabFile(_ context.Context, fileID string, _ bool) (domain.CollabFile, error) {
	m.files.mu.Lock()
	defer m.files.mu.Unlock()
	f, ok := m.files.files[fileID]
	if !ok || f.Kind != domain.KindText {
		return domain.CollabFile{}, domain.ErrFileNotFound
	}
	return domain.CollabFile{ProjectID: f.ProjectID, Content: *f.Content, Version: f.Version}, nil
}

func (m *memCollab) StoreCollab(_ context.Context, fileID string, st domain.CollabState) (int, error) {
	m.files.mu.Lock()
	defer m.files.mu.Unlock()
	f, ok := m.files.files[fileID]
	if !ok || f.Kind != domain.KindText {
		return 0, domain.ErrFileNotFound
	}
	if f.Version != st.BaseVersion {
		return 0, domain.ErrStaleVersion
	}
	f.Version++
	c := st.Content
	f.Content = &c
	m.stored = append(m.stored, st)
	return f.Version, nil
}

func (m *memCollab) CollabRoles(_ context.Context, checks []domain.AccessCheck) ([]domain.Role, error) {
	out := make([]domain.Role, len(checks))
	for i, c := range checks {
		m.files.mu.Lock()
		f, ok := m.files.files[c.FileID]
		m.files.mu.Unlock()
		if !ok || f.Kind != domain.KindText || m.suspended[c.UserID] {
			continue
		}
		m.files.s.mu.Lock()
		out[i] = m.files.s.members[f.ProjectID][c.UserID]
		m.files.s.mu.Unlock()
	}
	return out, nil
}

func (m *memCollab) TextFileIDsUnder(_ context.Context, projectID, path string) ([]string, error) {
	m.files.mu.Lock()
	defer m.files.mu.Unlock()
	var out []string
	for id, f := range m.files.files {
		p := strings.ToLower(f.Path)
		if f.ProjectID == projectID && f.Kind == domain.KindText && (p == strings.ToLower(path) || strings.HasPrefix(p, strings.ToLower(path)+"/")) {
			out = append(out, id)
		}
	}
	return out, nil
}

// memCollabEvents records what the sync server would be told.
type memCollabEvents struct {
	deleted  [][]string
	projects []string
}

func (m *memCollabEvents) FilesDeleted(_ context.Context, _ string, ids []string) {
	m.deleted = append(m.deleted, ids)
}

func (m *memCollabEvents) ProjectDeleted(_ context.Context, projectID string) {
	m.projects = append(m.projects, projectID)
}

type collabEnv struct {
	*fileEnv
	tickets *memTickets
	live    *memLive
	collab  *memCollab
	events  *memCollabEvents
	limit   *memLimiter
}

func newCollabEnv() *collabEnv {
	fe := newFileEnv()
	ce := &collabEnv{
		fileEnv: fe,
		tickets: &memTickets{byID: map[string]memTicket{}, now: func() time.Time { return fe.now }},
		live:    &memLive{live: map[string]bool{}},
		collab:  &memCollab{files: fe.files, suspended: map[string]bool{}},
		events:  &memCollabEvents{},
		limit:   &memLimiter{},
	}
	d := fe.service.d
	d.CollabFiles, d.Tickets, d.Live, d.CollabEvents, d.TicketLimiter = ce.collab, ce.tickets, ce.live, ce.events, ce.limit
	fe.service = New(d)
	return ce
}

func TestCollabTicket(t *testing.T) {
	e := newCollabEnv()
	pid := e.project(t)
	ctx := t.Context()
	main := e.fileAt(t, pid, MainFileName)
	e.files.names[fEditor] = "Ada Editor"

	tk, err := e.service.CollabTicket(ctx, fEditor, pid, main.ID)
	if err != nil {
		t.Fatal(err)
	}
	if tk.Role != domain.RoleEditor || tk.DisplayName != "Ada Editor" || len(tk.Ticket) < 40 || !tk.ExpiresAt.Equal(e.now.Add(TicketTTL)) {
		t.Errorf("ticket = %+v", tk)
	}
	for key := range e.tickets.byID {
		if strings.Contains(key, tk.Ticket) {
			t.Error("the store holds the ticket itself, not its hash")
		}
	}

	// Redeemed once, for this file only.
	s, err := e.service.RedeemTicket(ctx, tk.Ticket, main.ID)
	if err != nil || s.UserID != fEditor || s.ProjectID != pid || s.Role != domain.RoleEditor {
		t.Fatalf("redeem = %+v, %v", s, err)
	}
	if _, err := e.service.RedeemTicket(ctx, tk.Ticket, main.ID); !errors.Is(err, domain.ErrTicketNotFound) {
		t.Errorf("second redeem: %v, want not found", err)
	}

	// A ticket for another file is spent by the attempt.
	other, err := e.service.CreateFile(ctx, fOwner, pid, CreateFileInput{Path: "b.tex", Kind: domain.KindText, Content: ptr("b")})
	if err != nil {
		t.Fatal(err)
	}
	tk2, _ := e.service.CollabTicket(ctx, fEditor, pid, main.ID)
	if _, err := e.service.RedeemTicket(ctx, tk2.Ticket, other.ID); !errors.Is(err, domain.ErrTicketNotFound) {
		t.Errorf("wrong file: %v", err)
	}
	if _, err := e.service.RedeemTicket(ctx, tk2.Ticket, main.ID); !errors.Is(err, domain.ErrTicketNotFound) {
		t.Errorf("after a wrong file: %v, want spent", err)
	}

	// Expired after TicketTTL.
	tk3, _ := e.service.CollabTicket(ctx, fViewer, pid, main.ID)
	if tk3.Role != domain.RoleViewer {
		t.Errorf("viewer's ticket role = %q", tk3.Role)
	}
	e.now = e.now.Add(TicketTTL)
	if _, err := e.service.RedeemTicket(ctx, tk3.Ticket, main.ID); !errors.Is(err, domain.ErrTicketNotFound) {
		t.Errorf("expired: %v", err)
	}

	// A member removed between issue and redeem gets nothing.
	tk4, _ := e.service.CollabTicket(ctx, fEditor, pid, main.ID)
	e.withMember(pid, fEditor, domain.RoleNone)
	if _, err := e.service.RedeemTicket(ctx, tk4.Ticket, main.ID); !errors.Is(err, domain.ErrTicketNotFound) {
		t.Errorf("removed member: %v", err)
	}
	if _, err := e.service.RedeemTicket(ctx, "", main.ID); !errors.Is(err, domain.ErrTicketNotFound) {
		t.Errorf("empty ticket: %v", err)
	}
}

func TestCollabTicketRefusals(t *testing.T) {
	e := newCollabEnv()
	pid := e.project(t)
	ctx := t.Context()
	main := e.fileAt(t, pid, MainFileName)
	folder, _ := e.service.CreateFile(ctx, fOwner, pid, CreateFileInput{Path: "dir", Kind: domain.KindFolder})

	if _, err := e.service.CollabTicket(ctx, fStrang, pid, main.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("non-member: %v, want not found", err)
	}
	if _, err := e.service.CollabTicket(ctx, fEditor, pid, folder.ID); !errors.Is(err, domain.ErrNotTextFile) {
		t.Errorf("folder: %v", err)
	}
	if _, err := e.service.CollabTicket(ctx, fEditor, pid, "f999"); !errors.Is(err, domain.ErrFileNotFound) {
		t.Errorf("unknown file: %v", err)
	}
	e.actors.suspended[fEditor] = true
	var susp *domain.SuspendedError
	if _, err := e.service.CollabTicket(ctx, fEditor, pid, main.ID); !errors.As(err, &susp) {
		t.Errorf("suspended: %v", err)
	}
	e.limit.used = map[string]int{fOwner: TicketsPerWindow}
	var limited *domain.RateLimitedError
	if _, err := e.service.CollabTicket(ctx, fOwner, pid, main.ID); !errors.As(err, &limited) {
		t.Errorf("limit: %v", err)
	}
	if len(e.tickets.byID) != 0 {
		t.Errorf("refusals stored %d tickets", len(e.tickets.byID))
	}
}

func TestSyncAccess(t *testing.T) {
	e := newCollabEnv()
	pid := e.project(t)
	main := e.fileAt(t, pid, MainFileName)
	e.collab.suspended[fViewer] = true
	roles, err := e.service.SyncAccess(t.Context(), []domain.AccessCheck{
		{UserID: fOwner, FileID: main.ID}, {UserID: fEditor, FileID: main.ID},
		{UserID: fViewer, FileID: main.ID}, {UserID: fStrang, FileID: main.ID}, {UserID: fOwner, FileID: "gone"},
	})
	want := []domain.Role{domain.RoleOwner, domain.RoleEditor, domain.RoleNone, domain.RoleNone, domain.RoleNone}
	if err != nil || len(roles) != len(want) {
		t.Fatalf("roles = %v, %v", roles, err)
	}
	for i := range want {
		if roles[i] != want[i] {
			t.Errorf("check %d: %q, want %q", i, roles[i], want[i])
		}
	}
	var fe *domain.FieldError
	if _, err := e.service.SyncAccess(t.Context(), nil); !errors.As(err, &fe) {
		t.Errorf("no checks: %v", err)
	}
	if _, err := e.service.SyncAccess(t.Context(), make([]domain.AccessCheck, MaxAccessChecks+1)); !errors.As(err, &fe) {
		t.Errorf("too many checks: %v", err)
	}
}

// A REST save is refused while the file is live, and when that cannot be told (fail closed).
func TestSaveContentWhileLive(t *testing.T) {
	e := newCollabEnv()
	pid := e.project(t)
	ctx := t.Context()
	main := e.fileAt(t, pid, MainFileName)

	e.live.live[main.ID] = true
	if _, err := e.service.SaveContent(ctx, fEditor, pid, main.ID, "rest", 1); !errors.Is(err, domain.ErrLiveSessionOpen) {
		t.Errorf("live: %v, want live session open", err)
	}
	e.live.live[main.ID] = false
	e.live.err = errBoom
	if _, err := e.service.SaveContent(ctx, fEditor, pid, main.ID, "rest", 1); !errors.Is(err, domain.ErrLiveCheckUnavailable) {
		t.Errorf("check down: %v, want unavailable", err)
	}
	if got, _ := e.service.File(ctx, fOwner, pid, main.ID); *got.Content != "hello" {
		t.Errorf("content = %q; a refused save was stored", *got.Content)
	}
	e.live.err = nil
	if f, err := e.service.SaveContent(ctx, fEditor, pid, main.ID, "rest", 1); err != nil || f.Version != 2 {
		t.Errorf("not live: %+v, %v", f, err)
	}
}

func TestStoreCollabFile(t *testing.T) {
	e := newCollabEnv()
	pid := e.project(t)
	ctx := t.Context()
	main := e.fileAt(t, pid, MainFileName)
	by := fEditor
	ok := domain.CollabState{State: []byte{1, 0}, Epoch: "e", Content: "live text", BaseVersion: 1, UpdatedBy: &by}

	if v, err := e.service.StoreCollabFile(ctx, main.ID, ok); err != nil || v != 2 {
		t.Fatalf("store = %d, %v", v, err)
	}
	if _, err := e.service.StoreCollabFile(ctx, main.ID, ok); !errors.Is(err, domain.ErrStaleVersion) {
		t.Errorf("same base again: %v, want stale", err)
	}
	// Text the members typed is stored even with control characters (unlike REST).
	ctrl := ok
	ctrl.Content, ctrl.BaseVersion = "a\x1bb", 2
	if _, err := e.service.StoreCollabFile(ctx, main.ID, ctrl); err != nil {
		t.Errorf("control character: %v", err)
	}
	var fe *domain.FieldError
	for name, tt := range map[string]struct {
		mod  func(*domain.CollabState)
		want any
	}{
		"invalid UTF-8": {func(s *domain.CollabState) { s.Content = "\xff" }, &fe},
		"empty state":   {func(s *domain.CollabState) { s.State = nil }, &fe},
		"base 0":        {func(s *domain.CollabState) { s.BaseVersion = 0 }, &fe},
		"text too long": {func(s *domain.CollabState) { s.Content = strings.Repeat("x", MaxTextBytes+1) }, domain.ErrFileTooLarge},
		"state too big": {func(s *domain.CollabState) { s.State = make([]byte, domain.MaxCollabStateBytes+1) }, domain.ErrFileTooLarge},
	} {
		st := ok
		st.BaseVersion = 3
		tt.mod(&st)
		_, err := e.service.StoreCollabFile(ctx, main.ID, st)
		switch w := tt.want.(type) {
		case error:
			if !errors.Is(err, w) {
				t.Errorf("%s: %v, want %v", name, err, w)
			}
		default:
			if !errors.As(err, &fe) {
				t.Errorf("%s: %v, want a field error", name, err)
			}
		}
	}
	if _, err := e.service.StoreCollabFile(ctx, "f999", domain.CollabState{State: []byte{1}, Epoch: "e", BaseVersion: 1}); !errors.Is(err, domain.ErrFileNotFound) {
		t.Errorf("unknown file: %v", err)
	}
}

// Deleting text files or a project tells the sync server which documents are gone.
func TestCollabEventsOnDelete(t *testing.T) {
	e := newCollabEnv()
	pid := e.project(t)
	ctx := t.Context()
	if _, err := e.service.CreateFile(ctx, fOwner, pid, CreateFileInput{Path: "sec/a.tex", Kind: domain.KindText, Content: ptr("a")}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.service.CreateFile(ctx, fOwner, pid, CreateFileInput{Path: "sec/b.tex", Kind: domain.KindText, Content: ptr("b")}); err != nil {
		t.Fatal(err)
	}
	sec := e.fileAt(t, pid, "sec")
	if err := e.service.DeleteFile(ctx, fEditor, pid, sec.ID); err != nil {
		t.Fatal(err)
	}
	if len(e.events.deleted) != 1 || len(e.events.deleted[0]) != 2 {
		t.Errorf("deleted events = %v, want the folder's two text files", e.events.deleted)
	}
	title := e.store.projects[pid].Title
	if err := e.service.Delete(ctx, fOwner, pid, &title); err != nil {
		t.Fatal(err)
	}
	if len(e.events.projects) != 1 || e.events.projects[0] != pid {
		t.Errorf("project events = %v", e.events.projects)
	}
}
