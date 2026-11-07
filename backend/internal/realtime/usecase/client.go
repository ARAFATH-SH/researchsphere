package usecase

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/realtime/domain"
)

// Client is one open socket.
type Client struct {
	hub     *Hub
	member  *member // holds this client's reference
	userID  string
	id      string
	sock    domain.Socket
	expires time.Time
	send    chan []byte
	done    chan struct{}
	once    sync.Once

	// The incoming-event window; Receive runs on one goroutine only.
	windowStart time.Time
	windowCount int
}

// close ends the client once, without waiting: dispatch calls it, and a client that does not
// answer must not hold up anyone else.
func (c *Client) close(code domain.CloseCode) {
	c.once.Do(func() {
		close(c.done)
		c.hub.wg.Add(1)
		go func() {
			defer c.hub.wg.Done()
			c.hub.left(c)
			if err := c.sock.Close(code); err != nil {
				c.hub.logger.Debug("realtime: close socket", slog.Any("error", err))
			}
		}()
	})
}

// Done ends the client after the read side failed: the connection is gone.
func (c *Client) Done() { c.close(domain.CloseNormal) }

// writeLoop writes frames one at a time, and closes the socket when the access token expires.
func (c *Client) writeLoop() {
	expiry := time.NewTimer(max(0, c.expires.Sub(c.hub.now())))
	defer expiry.Stop()
	for {
		select {
		case <-c.done:
			return
		case <-expiry.C:
			c.close(domain.CloseTokenExpired)
			return
		case frame := <-c.send:
			ctx, cancel := context.WithTimeout(context.Background(), c.hub.cfg.WriteTimeout)
			err := c.sock.Write(ctx, frame)
			cancel()
			if err != nil {
				c.close(domain.CloseTryLater)
				return
			}
		}
	}
}

// heartbeat pings the client and refreshes its presence. A ping that gets no pong in time
// means the connection is dead. It has its own goroutine so a slow pong delays no frame.
func (c *Client) heartbeat() {
	t := time.NewTicker(c.hub.cfg.PingInterval)
	defer t.Stop()
	for {
		select {
		case <-c.done:
			return
		case <-t.C:
			ctx, cancel := context.WithTimeout(context.Background(), c.hub.cfg.PongTimeout)
			err := c.sock.Ping(ctx)
			cancel()
			if err != nil {
				c.close(domain.CloseTryLater)
				return
			}
			ctx, cancel = c.hub.callContext()
			if _, err := c.hub.presence.Add(ctx, c.userID, c.id, c.hub.now()); err != nil {
				c.hub.logger.WarnContext(ctx, "realtime: refresh presence", slog.Any("error", err))
			}
			cancel()
		}
	}
}

// Receive handles one frame from the client. Only typing is accepted; anything else is
// ignored and logged at debug level, without its content.
func (c *Client) Receive(ctx context.Context, frame []byte) {
	if !c.allow() {
		c.hub.logger.DebugContext(ctx, "realtime: incoming event over the limit", slog.String("user_id", c.userID))
		return
	}
	var e domain.ClientEvent
	if err := json.Unmarshal(frame, &e); err != nil {
		c.hub.logger.DebugContext(ctx, "realtime: frame is not an event", slog.String("user_id", c.userID))
		return
	}
	id := e.Data.ConversationID
	switch {
	case e.Type == domain.ClientTypingStart && id != "":
		c.hub.typingStart(ctx, c.userID, id)
	case e.Type == domain.ClientTypingStop && id != "":
		c.hub.typingStop(ctx, c.userID, id)
	default:
		c.hub.logger.DebugContext(ctx, "realtime: unknown event", slog.String("user_id", c.userID))
	}
}

// allow counts the frame against a fixed window.
func (c *Client) allow() bool {
	now := c.hub.now()
	if now.Sub(c.windowStart) >= ClientEventWindow {
		c.windowStart, c.windowCount = now, 0
	}
	c.windowCount++
	return c.windowCount <= MaxClientEvents
}
