// Package redis implements the realtime domain's Bus with Redis pub/sub, one channel per user,
// and its Presence with one sorted set per user (docs/realtime.md).
package redis

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	goredis "github.com/redis/go-redis/v9"

	platformredis "github.com/ARAFATH-SH/researchsphere/backend/internal/platform/redis"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/realtime/domain"
)

const (
	// callTimeout bounds each publish and presence call. A REST request waits at most this
	// long for one event (fail open, as the limiters).
	callTimeout = 300 * time.Millisecond
	// subscribeTimeout bounds waiting for Redis to confirm a subscription.
	subscribeTimeout = 2 * time.Second
	// PresenceTTL is how long a socket counts as open without a heartbeat. The heartbeat
	// comes every 25 s, so one can be missed.
	PresenceTTL = 60 * time.Second
	// deliveryBuffer is how many received envelopes may wait for the hub.
	deliveryBuffer = 1024
)

// newClient makes a client of the adapter's own from the shared one's address and
// credentials, that gives up after callTimeout (see auth/adapter/redis.New).
func newClient(shared *goredis.Client) *goredis.Client {
	opts := *shared.Options()
	opts.ContextTimeoutEnabled = true
	opts.PoolSize = 10
	opts.MaxRetries = -1
	opts.DialTimeout = callTimeout
	opts.ReadTimeout = callTimeout
	opts.WriteTimeout = callTimeout
	platformredis.ApplyDefaults(&opts)
	return goredis.NewClient(&opts)
}

// Bus is one instance's subscription and publisher.
type Bus struct {
	client *goredis.Client // publishes
	pubsub *goredis.PubSub
	prefix string
	out    chan domain.Delivery
	done   chan struct{}

	mu      sync.Mutex
	waiters map[string][]chan struct{}
}

var _ domain.Bus = (*Bus)(nil)

// NewBus returns a Bus and starts reading its subscription. Redis pub/sub ignores the database
// number, so the channel names carry it: tests on DB 15 never hear a development server on the
// same Redis. Close it at shutdown.
func NewBus(shared *goredis.Client) *Bus {
	b := &Bus{
		client: newClient(shared),
		// The subscription's own connection waits for messages, so it keeps the shared
		// client's timeouts rather than callTimeout.
		pubsub:  shared.Subscribe(context.Background()),
		prefix:  "realtime:" + strconv.Itoa(shared.Options().DB) + ":user:",
		out:     make(chan domain.Delivery, deliveryBuffer),
		done:    make(chan struct{}),
		waiters: map[string][]chan struct{}{},
	}
	go b.read()
	return b
}

// Close stops the subscription and closes the Bus's clients.
func (b *Bus) Close() error {
	close(b.done)
	return errors.Join(b.pubsub.Close(), b.client.Close())
}

func (b *Bus) channel(userID string) string { return b.prefix + userID }

func (b *Bus) read() {
	for m := range b.pubsub.ChannelWithSubscriptions(goredis.WithChannelSize(deliveryBuffer)) {
		switch v := m.(type) {
		case *goredis.Subscription:
			if v.Kind == "subscribe" {
				b.confirmed(v.Channel)
			}
		case *goredis.Message:
			userID, ok := strings.CutPrefix(v.Channel, b.prefix)
			if !ok {
				continue
			}
			select {
			case b.out <- domain.Delivery{UserID: userID, Payload: []byte(v.Payload)}:
			case <-b.done:
				return
			}
		}
	}
}

func (b *Bus) confirmed(channel string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, w := range b.waiters[channel] {
		close(w)
	}
	delete(b.waiters, channel)
}

// Deliveries is where received envelopes arrive.
func (b *Bus) Deliveries() <-chan domain.Delivery { return b.out }

// Publish sends payload on the user's channel.
func (b *Bus) Publish(ctx context.Context, userID string, payload []byte) error {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	if err := b.client.Publish(ctx, b.channel(userID), payload).Err(); err != nil {
		return fmt.Errorf("publish: %w", err)
	}
	return nil
}

// Subscribe returns once Redis has confirmed the subscription.
func (b *Bus) Subscribe(ctx context.Context, userID string) error {
	ctx, cancel := context.WithTimeout(ctx, subscribeTimeout)
	defer cancel()
	channel := b.channel(userID)
	w := make(chan struct{})
	b.mu.Lock()
	b.waiters[channel] = append(b.waiters[channel], w)
	b.mu.Unlock()
	if err := b.pubsub.Subscribe(ctx, channel); err != nil {
		b.forget(channel, w)
		return fmt.Errorf("subscribe: %w", err)
	}
	select {
	case <-w:
		return nil
	case <-ctx.Done():
		b.forget(channel, w)
		return fmt.Errorf("subscribe: %w", ctx.Err())
	}
}

// forget drops a waiter that gave up.
func (b *Bus) forget(channel string, w chan struct{}) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.waiters[channel] = slices.DeleteFunc(b.waiters[channel], func(c chan struct{}) bool { return c == w })
	if len(b.waiters[channel]) == 0 {
		delete(b.waiters, channel)
	}
}

// Unsubscribe stops the subscription. The command is written in order with Subscribe's.
func (b *Bus) Unsubscribe(ctx context.Context, userID string) error {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	if err := b.pubsub.Unsubscribe(ctx, b.channel(userID)); err != nil {
		return fmt.Errorf("unsubscribe: %w", err)
	}
	return nil
}

// Presence keeps one sorted set per user: each member is a socket, its score the time the
// entry stops counting. The key's own TTL cleans up after a crash.
type Presence struct {
	client *goredis.Client
	ttl    time.Duration
}

var _ domain.Presence = (*Presence)(nil)

// NewPresence returns a Presence with a client of its own. Close it at shutdown.
func NewPresence(shared *goredis.Client) *Presence {
	return &Presence{client: newClient(shared), ttl: PresenceTTL}
}

// Close closes the Presence's client.
func (p *Presence) Close() error { return p.client.Close() }

func presenceKey(userID string) string { return "realtime:presence:" + userID }

func score(t time.Time) string { return strconv.FormatInt(t.UnixMilli(), 10) }

// Add records or refreshes the socket and returns the live ones, oldest first: socket IDs
// sort by age.
func (p *Presence) Add(ctx context.Context, userID, socketID string, now time.Time) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	key := presenceKey(userID)
	var live *goredis.StringSliceCmd
	_, err := p.client.TxPipelined(ctx, func(tx goredis.Pipeliner) error {
		tx.ZAdd(ctx, key, goredis.Z{Score: float64(now.Add(p.ttl).UnixMilli()), Member: socketID})
		tx.ZRemRangeByScore(ctx, key, "-inf", "("+score(now))
		tx.Expire(ctx, key, p.ttl)
		live = tx.ZRange(ctx, key, 0, -1)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("add presence: %w", err)
	}
	sockets := live.Val()
	slices.Sort(sockets)
	return sockets, nil
}

// Remove forgets the socket and counts the live ones left.
func (p *Presence) Remove(ctx context.Context, userID, socketID string, now time.Time) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	key := presenceKey(userID)
	var left *goredis.IntCmd
	_, err := p.client.TxPipelined(ctx, func(tx goredis.Pipeliner) error {
		tx.ZRem(ctx, key, socketID)
		left = tx.ZCount(ctx, key, score(now), "+inf")
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("remove presence: %w", err)
	}
	return int(left.Val()), nil
}

// Count counts the live sockets.
func (p *Presence) Count(ctx context.Context, userID string, now time.Time) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	n, err := p.client.ZCount(ctx, presenceKey(userID), score(now), "+inf").Result()
	if err != nil {
		return 0, fmt.Errorf("count presence: %w", err)
	}
	return int(n), nil
}

// Online asks about every user in one round trip.
func (p *Presence) Online(ctx context.Context, userIDs []string, now time.Time) (map[string]bool, error) {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	counts := make([]*goredis.IntCmd, len(userIDs))
	_, err := p.client.Pipelined(ctx, func(pipe goredis.Pipeliner) error {
		for i, id := range userIDs {
			counts[i] = pipe.ZCount(ctx, presenceKey(id), score(now), "+inf")
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("read presence: %w", err)
	}
	out := make(map[string]bool, len(userIDs))
	for i, id := range userIDs {
		out[id] = counts[i].Val() > 0
	}
	return out, nil
}
