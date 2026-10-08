package main

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// freePort returns a TCP port that was free a moment ago.
func freePort(t *testing.T) int {
	t.Helper()
	var lc net.ListenConfig
	ln, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	return ln.Addr().(*net.TCPAddr).Port
}

// syncBuffer is a bytes.Buffer safe to read while the server writes logs to it.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func TestRunShutsDownWhenContextIsCancelled(t *testing.T) {
	const password = "s3cret-pw"
	port := strconv.Itoa(freePort(t))
	vars := map[string]string{
		// Nothing listens on these ports; the clients connect lazily, so run starts anyway.
		"DATABASE_URL": "postgres://u:" + password + "@127.0.0.1:1/db?sslmode=disable",
		"REDIS_URL":    "redis://:" + password + "@127.0.0.1:1/0",
		"API_PORT":     port,
	}
	ctx, cancel := context.WithCancel(context.Background())
	var logs syncBuffer
	done := make(chan error, 1)
	go func() { done <- run(ctx, func(k string) string { return vars[k] }, &logs) }()

	// Wait until the server answers, then ask for a shutdown.
	client := &http.Client{Timeout: time.Second}
	url := "http://127.0.0.1:" + port + "/api/v1/nope"
	deadline := time.Now().Add(5 * time.Second)
	for {
		req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
		resp, err := client.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode != http.StatusNotFound {
				t.Errorf("status = %d, want 404", resp.StatusCode)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("server did not start: %v\n%s", err, logs.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run() = %v, want nil", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("run() did not return after cancel")
	}
	out := logs.String()
	if !strings.Contains(out, "shutdown complete") {
		t.Errorf("logs lack shutdown complete:\n%s", out)
	}
	if strings.Contains(out, password) {
		t.Errorf("logs leak the password:\n%s", out)
	}
}

func TestRunStopsOnMissingSettings(t *testing.T) {
	err := run(context.Background(), func(string) string { return "" }, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "DATABASE_URL") || !strings.Contains(err.Error(), "REDIS_URL") {
		t.Fatalf("run() error = %v, want one naming DATABASE_URL and REDIS_URL", err)
	}
}
