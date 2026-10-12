package redis

import (
	"log/slog"
	"net"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/redis"
)

// Every method returns an error when Redis cannot be reached, so the usecase can log it
// and let the login go on (docs/decisions.md).
func TestAttemptsReturnErrorsWhenRedisIsDown(t *testing.T) {
	client := goredis.NewClient(&goredis.Options{Addr: "127.0.0.1:1", MaxRetries: -1, DialerRetries: 1, DialTimeout: time.Second})
	a := New(client)
	t.Cleanup(func() {
		_ = a.Close()
		_ = client.Close()
	})
	ctx := t.Context()

	if _, err := a.RecordFailure(ctx, "ada@example.org", time.Minute); err == nil {
		t.Error("RecordFailure() error = nil")
	}
	if err := a.Reset(ctx, "ada@example.org"); err == nil {
		t.Error("Reset() error = nil")
	}
	if err := a.Lock(ctx, "ada@example.org", time.Now().Add(time.Minute)); err == nil {
		t.Error("Lock() error = nil")
	}
	if _, err := a.LockedUntil(ctx, "ada@example.org"); err == nil {
		t.Error("LockedUntil() error = nil")
	}
}

// hangingRedis accepts connections and never answers, like a Redis that has stopped
// responding. It returns the address.
func hangingRedis(t *testing.T) string {
	t.Helper()
	var lc net.ListenConfig
	ln, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var conns []net.Conn
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			conns = append(conns, c) // read nothing, answer nothing
		}
	}()
	t.Cleanup(func() {
		_ = ln.Close()
		<-done
		for _, c := range conns {
			_ = c.Close()
		}
	})
	return ln.Addr().String()
}

// A counter call gives up after callTimeout even when Redis hangs, although the shared
// client it is made from (the production settings) would wait seconds. The calls a
// failed login can make together (check the lock, count the failure) end well within a
// second, so the login goes on (fail open) without a long wait.
func TestAttemptsGiveUpWhenRedisHangs(t *testing.T) {
	client, err := redis.NewClient("redis://"+hangingRedis(t)+"/0", slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	a := New(client)
	t.Cleanup(func() {
		_ = a.Close()
		_ = client.Close()
	})
	ctx := t.Context()

	if o := client.Options(); o.ContextTimeoutEnabled || o.ReadTimeout == callTimeout || o.PoolSize == 5 {
		t.Errorf("New changed the shared client's settings: %+v", o)
	}

	begin := time.Now()
	if _, err := a.LockedUntil(ctx, "ada@example.org"); err == nil {
		t.Error("LockedUntil() error = nil, want a timeout")
	}
	if _, err := a.RecordFailure(ctx, "ada@example.org", time.Minute); err == nil {
		t.Error("RecordFailure() error = nil, want a timeout")
	}
	if elapsed := time.Since(begin); elapsed > time.Second {
		t.Errorf("two counter calls took %v, want under 1s (each gives up after %v)", elapsed, callTimeout)
	}

	for name, call := range map[string]func() error{
		"Reset": func() error { return a.Reset(ctx, "ada@example.org") },
		"Lock":  func() error { return a.Lock(ctx, "ada@example.org", time.Now().Add(time.Minute)) },
	} {
		begin := time.Now()
		if err := call(); err == nil {
			t.Errorf("%s() error = nil, want a timeout", name)
		}
		if elapsed := time.Since(begin); elapsed > 2*callTimeout {
			t.Errorf("%s() took %v, want about %v", name, elapsed, callTimeout)
		}
	}
}
