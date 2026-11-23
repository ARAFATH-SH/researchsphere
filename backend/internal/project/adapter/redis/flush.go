package redis

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/project/domain"
)

// Flusher asks the sync server to store a project's open documents before a compile reads
// them (slice 23, docs/sync.md "flush"). It uses the shared client, whose read timeout
// go-redis extends for the blocking wait; it never closes it.
type Flusher struct {
	client *goredis.Client
}

var _ domain.LiveFlusher = (*Flusher)(nil)

// NewFlusher returns a Flusher on the shared client.
func NewFlusher(shared *goredis.Client) *Flusher { return &Flusher{client: shared} }

// FlushLive publishes a flush for the project and waits up to timeout for the sync server's
// answer. false, nil when no sync server listens or none answered in time, or it could not
// store every document.
func (f *Flusher) FlushLive(ctx context.Context, projectID string, timeout time.Duration) (bool, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return false, fmt.Errorf("make flush id: %w", err)
	}
	id := hex.EncodeToString(raw)
	b, err := json.Marshal(SyncEvent{Kind: SyncFlush, ProjectID: projectID, RequestID: id})
	if err != nil {
		return false, err
	}
	n, err := f.client.Publish(ctx, SyncChannel(f.client.Options().DB), b).Result()
	if err != nil {
		return false, fmt.Errorf("publish flush: %w", err)
	}
	if n == 0 {
		return false, nil // no sync server is listening
	}
	res, err := f.client.BLPop(ctx, timeout, FlushAckPrefix+id).Result()
	if errors.Is(err, goredis.Nil) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("wait for flush: %w", err)
	}
	return len(res) == 2 && res[1] == "ok", nil
}
