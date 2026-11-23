package usecase

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"time"
	"unicode/utf8"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/project/domain"
)

// Real-time editing (FR-12.4, FR-12.7 to FR-12.9, NFR-10.1; slice 22, docs/decisions.md
// 2026-11-19). The sync server reaches the database only through these operations.

// Limits of the real-time operations.
const (
	// TicketTTL is how long a collaboration ticket can be redeemed (docs/sync.md).
	TicketTTL = 30 * time.Second
	// TicketsPerWindow tickets a user may ask for in TicketWindow: a reconnect costs one, and
	// a workspace may open several files in several tabs.
	TicketsPerWindow = 60
	TicketWindow     = time.Minute
	// MaxAccessChecks is the most checks one access request may hold.
	MaxAccessChecks = 200
	// ticketBytes is the randomness of a ticket.
	ticketBytes = 32
)

// ticketKey is how a ticket is stored: its SHA-256, so Redis never holds a usable ticket.
func ticketKey(ticket string) string {
	sum := sha256.Sum256([]byte(ticket))
	return hex.EncodeToString(sum[:])
}

// CollabTicket gives a member a one-time ticket to open the text file on the sync server
// (FR-12.7). Errors, in order: *domain.RateLimitedError after TicketsPerWindow tickets in
// TicketWindow; the access errors of reading files (members only, else domain.ErrNotFound);
// *domain.SuspendedError; domain.ErrFileNotFound; domain.ErrNotTextFile.
func (s *Service) CollabTicket(ctx context.Context, userID, projectID, fileID string) (domain.IssuedTicket, error) {
	if s.d.Tickets == nil || s.d.CollabFiles == nil {
		return domain.IssuedTicket{}, errors.New("project: real-time editing is not configured")
	}
	if err := s.underLimit(ctx, s.d.TicketLimiter, userID, TicketsPerWindow, "collaboration ticket"); err != nil {
		return domain.IssuedTicket{}, err
	}
	role, err := s.d.Store.Role(ctx, projectID, userID)
	if err != nil {
		return domain.IssuedTicket{}, err
	}
	if role == domain.RoleNone {
		return domain.IssuedTicket{}, domain.ErrNotFound
	}
	if !s.d.Permissions.Can(role, domain.ActReadFiles) {
		return domain.IssuedTicket{}, domain.ErrForbidden
	}
	if err := s.refuseSuspended(ctx, userID); err != nil {
		return domain.IssuedTicket{}, err
	}
	f, err := s.d.Files.File(ctx, projectID, fileID)
	if err != nil {
		return domain.IssuedTicket{}, err
	}
	if f.Kind != domain.KindText {
		return domain.IssuedTicket{}, domain.ErrNotTextFile
	}
	cf, err := s.d.CollabFiles.CollabFile(ctx, f.ID, false)
	if err != nil {
		return domain.IssuedTicket{}, err
	}
	name, err := s.d.Files.FullName(ctx, userID)
	if err != nil {
		return domain.IssuedTicket{}, err
	}
	raw := make([]byte, ticketBytes)
	if _, err := rand.Read(raw); err != nil {
		return domain.IssuedTicket{}, fmt.Errorf("make ticket: %w", err)
	}
	ticket := base64.RawURLEncoding.EncodeToString(raw)
	if err := s.d.Tickets.PutTicket(ctx, ticketKey(ticket), domain.CollabTicket{UserID: userID, ProjectID: projectID, FileID: f.ID}, TicketTTL); err != nil {
		return domain.IssuedTicket{}, err
	}
	if err := s.d.TicketLimiter.Record(ctx, userID, TicketWindow); err != nil {
		s.d.Logger.WarnContext(ctx, "collaboration ticket limit not recorded", slog.Any("error", err))
	}
	return domain.IssuedTicket{
		Ticket: ticket, ExpiresAt: s.d.Now().Add(TicketTTL), Role: role, DisplayName: name, Epoch: cf.Epoch,
	}, nil
}

// RedeemTicket spends a ticket for fileID and says who it was for and their role now. Any
// failure is domain.ErrTicketNotFound: unknown, expired or spent, issued for another file,
// or the holder has no access any more. The ticket is spent in every case.
func (s *Service) RedeemTicket(ctx context.Context, ticket, fileID string) (domain.SyncSession, error) {
	if s.d.Tickets == nil || s.d.CollabFiles == nil {
		return domain.SyncSession{}, errors.New("project: real-time editing is not configured")
	}
	if ticket == "" {
		return domain.SyncSession{}, domain.ErrTicketNotFound
	}
	t, ok, err := s.d.Tickets.TakeTicket(ctx, ticketKey(ticket))
	if err != nil {
		return domain.SyncSession{}, err
	}
	if !ok || t.FileID != fileID {
		return domain.SyncSession{}, domain.ErrTicketNotFound
	}
	roles, err := s.d.CollabFiles.CollabRoles(ctx, []domain.AccessCheck{{UserID: t.UserID, FileID: t.FileID}})
	if err != nil {
		return domain.SyncSession{}, err
	}
	if roles[0] == domain.RoleNone {
		return domain.SyncSession{}, domain.ErrTicketNotFound
	}
	name, err := s.d.Files.FullName(ctx, t.UserID)
	if err != nil {
		return domain.SyncSession{}, err
	}
	return domain.SyncSession{UserID: t.UserID, ProjectID: t.ProjectID, FileID: t.FileID, Role: roles[0], DisplayName: name}, nil
}

// SyncAccess answers each check with the user's role now, RoleNone without access.
// *domain.FieldError ("checks") for none or more than MaxAccessChecks.
func (s *Service) SyncAccess(ctx context.Context, checks []domain.AccessCheck) ([]domain.Role, error) {
	if s.d.CollabFiles == nil {
		return nil, errors.New("project: real-time editing is not configured")
	}
	if len(checks) == 0 || len(checks) > MaxAccessChecks {
		return nil, &domain.FieldError{Field: "checks"}
	}
	return s.d.CollabFiles.CollabRoles(ctx, checks)
}

// LoadCollabFile reads a text file for the sync server. It holds the row shared while it
// reads, so a REST save that locked the row first, and so checked for a live session before
// the sync server marked the file, is committed first and read here (docs/decisions.md
// 2026-11-19). domain.ErrFileNotFound when there is no such text file.
func (s *Service) LoadCollabFile(ctx context.Context, fileID string) (domain.CollabFile, error) {
	if s.d.CollabFiles == nil {
		return domain.CollabFile{}, errors.New("project: real-time editing is not configured")
	}
	var f domain.CollabFile
	err := s.d.Tx.InTx(ctx, func(ctx context.Context) error {
		var err error
		f, err = s.d.CollabFiles.CollabFile(ctx, fileID, true)
		return err
	})
	return f, err
}

// StoreCollabFile writes the sync server's state and text in one statement (NFR-10.1) and
// returns the new version. Errors: *domain.FieldError ("content" for text that is not UTF-8,
// "ydocState" for an empty state, "baseVersion"); domain.ErrFileTooLarge (text over
// MaxTextBytes or state over domain.MaxCollabStateBytes); domain.ErrFileNotFound;
// domain.ErrStaleVersion.
//
// The text is what members typed, so unlike a REST save it is not refused for control
// characters: refusing would leave the document unsaved while everyone goes on editing.
func (s *Service) StoreCollabFile(ctx context.Context, fileID string, st domain.CollabState) (int, error) {
	if s.d.CollabFiles == nil {
		return 0, errors.New("project: real-time editing is not configured")
	}
	switch {
	case !utf8.ValidString(st.Content):
		return 0, &domain.FieldError{Field: "content"}
	case len(st.State) == 0:
		return 0, &domain.FieldError{Field: "ydocState"}
	case st.BaseVersion < 1:
		return 0, &domain.FieldError{Field: "baseVersion"}
	case len(st.Content) > MaxTextBytes, len(st.State) > domain.MaxCollabStateBytes:
		return 0, domain.ErrFileTooLarge
	}
	return s.d.CollabFiles.StoreCollab(ctx, fileID, st)
}

// refuseLive refuses a REST save while the file is open on the sync server, whose next store
// would overwrite it. It runs with the file row locked, so a load waits for the save and a
// save that comes after the mark sees it. Fail closed: if the mark cannot be read, nothing is
// saved.
func (s *Service) refuseLive(ctx context.Context, fileID string) error {
	if s.d.Live == nil {
		return nil
	}
	live, err := s.d.Live.LiveSession(ctx, fileID)
	if err != nil {
		s.d.Logger.WarnContext(ctx, "live session check failed, refusing the save", slog.Any("error", err))
		return domain.ErrLiveCheckUnavailable
	}
	if live {
		return domain.ErrLiveSessionOpen
	}
	return nil
}
