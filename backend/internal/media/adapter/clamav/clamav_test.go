package clamav

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"strings"
	"testing"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/media/domain"
)

// fakeClamd is a TCP server that speaks clamd's z-commands. For INSTREAM it reads the chunks,
// records their sizes and what they held, and answers with answer(data).
type fakeClamd struct {
	ln       net.Listener
	answer   func(data []byte) string
	stopAt   int // when > 0, answer and close after this many bytes (StreamMaxLength)
	commands chan string
	chunks   chan []int
	data     chan []byte
}

func newFakeClamd(t *testing.T, answer func([]byte) string) *fakeClamd {
	t.Helper()
	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeClamd{ln: ln, answer: answer, commands: make(chan string, 10), chunks: make(chan []int, 10), data: make(chan []byte, 10)}
	t.Cleanup(func() { _ = ln.Close() })
	go f.serve()
	return f
}

func (f *fakeClamd) addr() string { return f.ln.Addr().String() }

func (f *fakeClamd) serve() {
	for {
		conn, err := f.ln.Accept()
		if err != nil {
			return
		}
		go f.handle(conn)
	}
}

func (f *fakeClamd) handle(conn net.Conn) {
	defer func() { _ = conn.Close() }()
	r := bufio.NewReader(conn)
	cmd, err := r.ReadString(0)
	if err != nil {
		return
	}
	cmd = strings.TrimSuffix(cmd, "\x00")
	f.commands <- cmd
	switch cmd {
	case "zPING":
		_, _ = conn.Write([]byte("PONG\x00"))
	case "zINSTREAM":
		var sizes []int
		var data []byte
		for {
			var n uint32
			if err := binary.Read(r, binary.BigEndian, &n); err != nil {
				return
			}
			if n == 0 {
				break
			}
			sizes = append(sizes, int(n))
			chunk := make([]byte, n)
			if _, err := io.ReadFull(r, chunk); err != nil {
				return
			}
			data = append(data, chunk...)
			if f.stopAt > 0 && len(data) >= f.stopAt {
				_, _ = conn.Write([]byte("INSTREAM size limit exceeded. ERROR\x00"))
				return
			}
		}
		f.chunks <- sizes
		f.data <- data
		_, _ = conn.Write([]byte(f.answer(data) + "\x00"))
	}
}

func TestPing(t *testing.T) {
	f := newFakeClamd(t, nil)
	if err := New(f.addr()).Ping(t.Context()); err != nil {
		t.Fatal(err)
	}
	if cmd := <-f.commands; cmd != "zPING" {
		t.Errorf("command = %q", cmd)
	}
}

// A clamd that does not listen (down, or loading its signatures) is unavailable, not an error
// about a file.
func TestUnreachable(t *testing.T) {
	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	c := New(addr)
	if err := c.Ping(t.Context()); !errors.Is(err, domain.ErrScannerUnavailable) {
		t.Errorf("Ping: %v", err)
	}
	if _, err := c.Scan(t.Context(), strings.NewReader("x")); !errors.Is(err, domain.ErrScannerUnavailable) {
		t.Errorf("Scan: %v", err)
	}
}

// INSTREAM: chunks of at most 64 KiB, each with its 4-byte big-endian length, then a zero
// length; the answer decides the verdict.
func TestScanFramingAndAnswers(t *testing.T) {
	for _, tc := range []struct {
		name     string
		answer   string
		infected bool
		sig      string
		fileErr  bool
	}{
		{"clean", "stream: OK", false, "", false},
		{"found", "stream: Eicar-Test-Signature FOUND", true, "Eicar-Test-Signature", false},
		{"error", "stream: Can't allocate memory ERROR", false, "", true},
		{"unexpected", "what?", false, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeClamd(t, func([]byte) string { return tc.answer })
			payload := bytes.Repeat([]byte("abcdefgh"), (150<<10)/8+3) // 150 KiB and a bit
			v, err := New(f.addr()).Scan(t.Context(), bytes.NewReader(payload))
			if <-f.commands != "zINSTREAM" {
				t.Error("not an INSTREAM")
			}
			sizes, data := <-f.chunks, <-f.data
			if !bytes.Equal(data, payload) {
				t.Errorf("clamd got %d bytes, want %d", len(data), len(payload))
			}
			for _, n := range sizes {
				if n > chunkSize {
					t.Errorf("chunk of %d bytes", n)
				}
			}
			if len(sizes) != 3 {
				t.Errorf("chunks = %v", sizes)
			}
			var fe *domain.ScanFileError
			switch {
			case tc.fileErr:
				if !errors.As(err, &fe) {
					t.Errorf("err = %v, want ScanFileError", err)
				}
			case err != nil:
				t.Fatal(err)
			case v.Infected != tc.infected || v.Signature != tc.sig:
				t.Errorf("verdict = %+v", v)
			}
		})
	}
}

// clamd refuses a stream over its StreamMaxLength and closes; that is an error about the file,
// so it counts as a try, and not as clamd being away.
func TestScanOverStreamLimit(t *testing.T) {
	f := newFakeClamd(t, func([]byte) string { return "stream: OK" })
	f.stopAt = 100 << 10
	_, err := New(f.addr()).Scan(t.Context(), bytes.NewReader(make([]byte, 4<<20)))
	var fe *domain.ScanFileError
	if !errors.As(err, &fe) || !strings.Contains(fe.Reason, "size limit") {
		t.Errorf("err = %v, want ScanFileError about the size limit", err)
	}
}

// An error reading the file is returned as it is: neither the scanner's fault nor the file's.
func TestScanSourceError(t *testing.T) {
	f := newFakeClamd(t, func([]byte) string { return "stream: OK" })
	boom := errors.New("bucket went away")
	_, err := New(f.addr()).Scan(t.Context(), io.MultiReader(strings.NewReader("abc"), errReader{boom}))
	var fe *domain.ScanFileError
	if !errors.Is(err, boom) || errors.Is(err, domain.ErrScannerUnavailable) || errors.As(err, &fe) {
		t.Errorf("err = %v", err)
	}
}

type errReader struct{ err error }

func (r errReader) Read([]byte) (int, error) { return 0, r.err }

func TestParse(t *testing.T) {
	if v, err := parse("stream: Win.Test.EICAR_HDB-1 FOUND"); err != nil || !v.Infected || v.Signature != "Win.Test.EICAR_HDB-1" {
		t.Errorf("FOUND: %+v %v", v, err)
	}
	if v, err := parse("stream: OK"); err != nil || v.Infected {
		t.Errorf("OK: %+v %v", v, err)
	}
}
