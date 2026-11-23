package main

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	goredis "github.com/redis/go-redis/v9"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/httpserver"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/httpx"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/postgres"
	projectredis "github.com/ARAFATH-SH/researchsphere/backend/internal/project/adapter/redis"
	projectdomain "github.com/ARAFATH-SH/researchsphere/backend/internal/project/domain"
)

// Real-time editing (slice 22, docs/sync.md): the sync server is a separate service. It
// calls the internal operations on a listener of their own, and hears about changes that end
// access through Redis pub/sub.

// internalPrefix is the path of the operations only the sync server calls.
const internalPrefix = httpserver.BaseURL + "/internal/"

// publicOnly answers 404 for the internal operations, so they never reach the internet
// through the public port, whatever the reverse proxy forwards.
func publicOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, internalPrefix) {
			httpx.WriteError(w, http.StatusNotFound, httpx.CodeNotFound, "Not found.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// internalOnly serves the internal operations and answers 404 for everything else.
func internalOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, internalPrefix) {
			httpx.WriteError(w, http.StatusNotFound, httpx.CodeNotFound, "Not found.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// syncEvents publishes the changes that end access to open documents, after the commit of
// the transaction the context carries (dropped on a rollback). A failure is logged and never
// fails the change: the sync server checks every connection again every 60 s.
type syncEvents struct {
	limits *projectredis.Limits
	logger *slog.Logger
}

var _ projectdomain.CollabEvents = (*syncEvents)(nil)

// newSyncEvents returns the publisher on a Redis client of its own; close it at shutdown.
func newSyncEvents(rdb *goredis.Client, logger *slog.Logger) *syncEvents {
	return &syncEvents{limits: projectredis.New(rdb), logger: logger}
}

func (e *syncEvents) Close() error { return e.limits.Close() }

func (e *syncEvents) publish(ctx context.Context, ev projectredis.SyncEvent) {
	if e == nil {
		return
	}
	postgres.AfterCommit(ctx, func(ctx context.Context) {
		if err := e.limits.Collab().PublishSync(ctx, ev); err != nil {
			e.logger.WarnContext(ctx, "sync event not published", slog.String("kind", ev.Kind), slog.Any("error", err))
		}
	})
}

// MembersChanged: the users' role in the project changed, or they left it (FR-11).
func (e *syncEvents) MembersChanged(ctx context.Context, projectID string, userIDs ...string) {
	e.publish(ctx, projectredis.SyncEvent{Kind: projectredis.SyncMembersChanged, ProjectID: projectID, UserIDs: userIDs})
}

// FilesDeleted: the text files are gone.
func (e *syncEvents) FilesDeleted(ctx context.Context, projectID string, fileIDs []string) {
	e.publish(ctx, projectredis.SyncEvent{Kind: projectredis.SyncFilesDeleted, ProjectID: projectID, FileIDs: fileIDs})
}

// ProjectDeleted: the project and its files are gone.
func (e *syncEvents) ProjectDeleted(ctx context.Context, projectID string) {
	e.publish(ctx, projectredis.SyncEvent{Kind: projectredis.SyncProjectDeleted, ProjectID: projectID})
}

// UserEnded: the user's sessions ended (logout, password change, account deletion, a reused
// refresh token) or the account was suspended. The sync server closes their connections; a
// client that may still edit asks for a new ticket.
func (e *syncEvents) UserEnded(ctx context.Context, userID string) {
	e.publish(ctx, projectredis.SyncEvent{Kind: projectredis.SyncUserEnded, UserIDs: []string{userID}})
}
