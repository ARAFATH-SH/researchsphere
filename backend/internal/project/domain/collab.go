package domain

import (
	"context"
	"errors"
	"time"
)

// Real-time editing (FR-12.4, FR-12.7 to FR-12.9, NFR-10.1; slice 22, docs/decisions.md
// 2026-11-19, docs/sync.md). The sync server is a separate service: browsers reach it with a
// one-time ticket the API issues, and it reads and writes files only through the API's
// internal operations, which run here.

// CollabTicket is what a ticket stands for until it is redeemed: who asked, for which file.
type CollabTicket struct {
	UserID    string
	ProjectID string
	FileID    string
}

// IssuedTicket is the answer to a member who asks to open a file on the sync server.
type IssuedTicket struct {
	Ticket      string
	ExpiresAt   time.Time
	Role        Role
	DisplayName string
	Epoch       *string // the file's Yjs history; nil when none is stored
}

// SyncSession is a redeemed ticket: the member and their role now.
type SyncSession struct {
	UserID      string
	ProjectID   string
	FileID      string
	Role        Role
	DisplayName string
}

// AccessCheck asks for userID's role on the project of the text file fileID.
type AccessCheck struct {
	UserID string
	FileID string
}

// CollabFile is a text file as the sync server loads it.
type CollabFile struct {
	ProjectID string
	Content   string
	Version   int
	State     []byte  // Y.encodeStateAsUpdate of the document; nil: build it from Content
	Epoch     *string // set exactly when State is
}

// CollabState is what the sync server stores: the Yjs state and the text it holds, in one
// write, when the file is still at BaseVersion.
type CollabState struct {
	State       []byte
	Epoch       string
	Content     string
	BaseVersion int
	UpdatedBy   *string // the last member who edited; nil when unknown
}

// MaxCollabStateBytes caps a stored Yjs state (migration 000037, project_files_ydoc_size).
const MaxCollabStateBytes = 8 << 20

// Errors of the real-time operations.
var (
	// ErrTicketNotFound covers an unknown, expired or spent ticket, one issued for another
	// file, and a holder who has no access any more.
	ErrTicketNotFound = errors.New("project: ticket not found")
	// ErrLiveSessionOpen refuses a REST save while the file is open on the sync server.
	ErrLiveSessionOpen = errors.New("project: the file is open for real-time editing")
	// ErrLiveCheckUnavailable refuses a REST save when it cannot be told whether the file
	// is open on the sync server (fail closed).
	ErrLiveCheckUnavailable = errors.New("project: cannot check for a real-time session")
	// ErrStaleVersion refuses a sync store whose base version is not the file's version.
	ErrStaleVersion = errors.New("project: the file changed since the base version")
)

// TicketStore keeps tickets for a short time, by a key derived from the ticket.
type TicketStore interface {
	// PutTicket stores t under key for ttl.
	PutTicket(ctx context.Context, key string, t CollabTicket, ttl time.Duration) error
	// TakeTicket reads and deletes the ticket under key at once; false when there is none.
	TakeTicket(ctx context.Context, key string) (CollabTicket, bool, error)
}

// LiveSessions tells whether a file is open on the sync server, which marks it while it
// holds the document.
type LiveSessions interface {
	LiveSession(ctx context.Context, fileID string) (bool, error)
	// AnyLive reports whether any of the files is open on the sync server.
	AnyLive(ctx context.Context, fileIDs []string) (bool, error)
}

// CollabEvents tells the sync server about changes that end access to open documents.
// Implementations publish after the commit and never fail the change; the sync server checks
// every open connection again every 60 s anyway. Membership changes reach it through
// Watcher.MembersChanged.
type CollabEvents interface {
	// FilesDeleted: the text files are gone.
	FilesDeleted(ctx context.Context, projectID string, fileIDs []string)
	// ProjectDeleted: the project and all its files are gone.
	ProjectDeleted(ctx context.Context, projectID string)
}

// CollabStore is what the real-time operations read and write in the database.
type CollabStore interface {
	// CollabFile reads a text file for the sync server; shared, it holds the row shared until
	// the transaction ends, so a REST save in progress finishes first. ErrFileNotFound when
	// there is no such text file.
	CollabFile(ctx context.Context, fileID string, shared bool) (CollabFile, error)
	// StoreCollab writes st when the file is still at st.BaseVersion and returns the new
	// version; ErrStaleVersion when it is not, ErrFileNotFound when there is no such text
	// file. An UpdatedBy whose account is gone is stored as nobody.
	StoreCollab(ctx context.Context, fileID string, st CollabState) (int, error)
	// CollabRoles answers each check in order: the user's role in the file's project, or
	// RoleNone when the user is not a member, unverified or suspended, or the file is not a
	// text file of a project.
	CollabRoles(ctx context.Context, checks []AccessCheck) ([]Role, error)
	// TextFileIDsUnder returns the IDs of the text files at path and under it.
	TextFileIDsUnder(ctx context.Context, projectID, path string) ([]string, error)
}
