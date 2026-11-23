package redis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/project/domain"
)

// Real-time editing (slice 22, docs/sync.md). Keys the API and the sync server share; both
// use the same Redis database.
const (
	// ticketPrefix is followed by the SHA-256 of a ticket (hex), never the ticket itself.
	ticketPrefix = "sync:ticket:"
	// LivePrefix is followed by a file ID. The sync server sets the key, with a TTL it keeps
	// renewing, while it holds the file's document.
	LivePrefix = "sync:live:"
)

// Collab keeps collaboration tickets and reads the sync server's live marks.
type Collab struct {
	client *goredis.Client
}

var (
	_ domain.TicketStore  = Collab{}
	_ domain.LiveSessions = Collab{}
)

// Collab returns the ticket store and live-session check on the limits' client, which gives
// up after callTimeout.
func (l *Limits) Collab() Collab { return Collab{client: l.client} }

type storedTicket struct {
	UserID    string `json:"u"`
	ProjectID string `json:"p"`
	FileID    string `json:"f"`
}

// PutTicket stores t under key for ttl.
func (c Collab) PutTicket(ctx context.Context, key string, t domain.CollabTicket, ttl time.Duration) error {
	b, err := json.Marshal(storedTicket{UserID: t.UserID, ProjectID: t.ProjectID, FileID: t.FileID})
	if err != nil {
		return err
	}
	if err := c.client.Set(ctx, ticketPrefix+key, b, ttl).Err(); err != nil {
		return fmt.Errorf("store ticket: %w", err)
	}
	return nil
}

// TakeTicket reads and deletes the ticket in one command, so it works once.
func (c Collab) TakeTicket(ctx context.Context, key string) (domain.CollabTicket, bool, error) {
	b, err := c.client.GetDel(ctx, ticketPrefix+key).Bytes()
	if errors.Is(err, goredis.Nil) {
		return domain.CollabTicket{}, false, nil
	}
	if err != nil {
		return domain.CollabTicket{}, false, fmt.Errorf("take ticket: %w", err)
	}
	var t storedTicket
	if err := json.Unmarshal(b, &t); err != nil {
		return domain.CollabTicket{}, false, fmt.Errorf("read ticket: %w", err)
	}
	return domain.CollabTicket{UserID: t.UserID, ProjectID: t.ProjectID, FileID: t.FileID}, true, nil
}

// LiveSession reports whether the sync server holds the file's document.
func (c Collab) LiveSession(ctx context.Context, fileID string) (bool, error) {
	n, err := c.client.Exists(ctx, LivePrefix+fileID).Result()
	if err != nil {
		return false, fmt.Errorf("read live mark: %w", err)
	}
	return n > 0, nil
}

// SyncEvent tells the sync server about a change that may end access to open documents
// (docs/sync.md, "Events"). Kind is one of the SyncEvent* constants.
type SyncEvent struct {
	Kind      string   `json:"kind"`
	ProjectID string   `json:"projectId,omitempty"`
	UserIDs   []string `json:"userIds,omitempty"`
	FileIDs   []string `json:"fileIds,omitempty"`
}

// Kinds of SyncEvent.
const (
	SyncMembersChanged = "members_changed"
	SyncFilesDeleted   = "files_deleted"
	SyncProjectDeleted = "project_deleted"
	SyncUserEnded      = "user_ended"
)

// SyncChannel is the pub/sub channel of sync events. Pub/sub ignores database numbers, so
// the name carries the database's, as the realtime channels do: tests on DB 15 never reach a
// development sync server.
func SyncChannel(db int) string { return "sync:" + strconv.Itoa(db) + ":events" }

// PublishSync sends ev to the sync server. Delivery is at most once; the sync server checks
// every connection again every 60 s.
func (c Collab) PublishSync(ctx context.Context, ev SyncEvent) error {
	b, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	if err := c.client.Publish(ctx, SyncChannel(c.client.Options().DB), b).Err(); err != nil {
		return fmt.Errorf("publish sync event: %w", err)
	}
	return nil
}
