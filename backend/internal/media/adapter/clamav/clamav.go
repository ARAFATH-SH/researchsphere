// Package clamav implements the media domain's VirusScanner with clamd's TCP protocol
// (FR-16.7, NFR-13). Only two commands are used: PING, and INSTREAM, which streams a file in
// chunks, each a 4-byte big-endian length and its bytes, ended by a zero length. The z prefix
// makes clamd read and write NUL-terminated lines. Standard library only, so the scanner client
// adds no dependency (docs/decisions.md 2026-11-09).
package clamav

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/media/domain"
)

const (
	// chunkSize is the most bytes in one INSTREAM chunk.
	chunkSize = 64 << 10
	// dialTimeout bounds connecting; pingTimeout the whole PING.
	dialTimeout = 3 * time.Second
	pingTimeout = 5 * time.Second
	// maxReply bounds what is read of clamd's answer.
	maxReply = 4096
)

// Client talks to one clamd.
type Client struct {
	addr string
}

var _ domain.VirusScanner = (*Client)(nil)

// New returns a Client for clamd at addr (host:port). It does not connect until used.
func New(addr string) *Client {
	return &Client{addr: addr}
}

func (c *Client) dial(ctx context.Context) (net.Conn, error) {
	d := net.Dialer{Timeout: dialTimeout}
	conn, err := d.DialContext(ctx, "tcp", c.addr)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", domain.ErrScannerUnavailable, err) //nolint:errorlint // the kind is ErrScannerUnavailable
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	return conn, nil
}

// Ping answers nil when clamd replies PONG. clamd does not listen while it loads its
// signatures, so a starting clamd is unavailable too.
func (c *Client) Ping(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, pingTimeout)
	defer cancel()
	conn, err := c.dial(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	if _, err := conn.Write([]byte("zPING\x00")); err != nil {
		return fmt.Errorf("%w: ping: %v", domain.ErrScannerUnavailable, err) //nolint:errorlint // as in dial
	}
	reply, err := readReply(conn)
	if err != nil {
		return err
	}
	if reply != "PONG" {
		return fmt.Errorf("%w: ping answered %q", domain.ErrScannerUnavailable, reply)
	}
	return nil
}

// sourceError marks an error from the reader being scanned, so it is not taken for the
// scanner's.
type sourceError struct{ err error }

func (e *sourceError) Error() string { return "read the file to scan: " + e.err.Error() }
func (e *sourceError) Unwrap() error { return e.err }

// Scan streams r to clamd. clamd answers "stream: OK", "stream: <name> FOUND" or
// "<reason> ERROR". When clamd stops reading early (it refuses a stream over its
// StreamMaxLength and closes), the write fails; its answer is still read, so the refusal counts
// as an error about the file and not as clamd being away.
func (c *Client) Scan(ctx context.Context, r io.Reader) (domain.ScanVerdict, error) {
	conn, err := c.dial(ctx)
	if err != nil {
		return domain.ScanVerdict{}, err
	}
	defer func() { _ = conn.Close() }()

	writeErr := stream(conn, r)
	var src *sourceError
	if errors.As(writeErr, &src) {
		return domain.ScanVerdict{}, src
	}
	reply, readErr := readReply(conn)
	if readErr != nil {
		if writeErr != nil {
			return domain.ScanVerdict{}, fmt.Errorf("%w: stream: %v", domain.ErrScannerUnavailable, writeErr) //nolint:errorlint // as in dial
		}
		return domain.ScanVerdict{}, readErr
	}
	return parse(reply)
}

// stream writes the INSTREAM command, the chunks and the zero length.
func stream(w io.Writer, r io.Reader) error {
	if _, err := w.Write([]byte("zINSTREAM\x00")); err != nil {
		return err
	}
	buf := make([]byte, 4+chunkSize)
	for {
		n, rerr := io.ReadFull(r, buf[4:])
		if n > 0 {
			binary.BigEndian.PutUint32(buf[:4], uint32(n)) //nolint:gosec // n <= chunkSize
			if _, err := w.Write(buf[:4+n]); err != nil {
				return err
			}
		}
		if errors.Is(rerr, io.EOF) || errors.Is(rerr, io.ErrUnexpectedEOF) {
			break
		}
		if rerr != nil {
			return &sourceError{err: rerr}
		}
	}
	_, err := w.Write([]byte{0, 0, 0, 0})
	return err
}

// readReply reads one NUL-terminated answer, or what came before clamd closed.
func readReply(r io.Reader) (string, error) {
	b, err := io.ReadAll(io.LimitReader(r, maxReply))
	if err != nil && len(b) == 0 {
		return "", fmt.Errorf("%w: read answer: %v", domain.ErrScannerUnavailable, err) //nolint:errorlint // as in dial
	}
	if i := bytes.IndexByte(b, 0); i >= 0 {
		b = b[:i]
	}
	reply := strings.TrimSpace(string(b))
	if reply == "" {
		return "", fmt.Errorf("%w: empty answer", domain.ErrScannerUnavailable)
	}
	return reply, nil
}

// parse reads an INSTREAM answer.
func parse(reply string) (domain.ScanVerdict, error) {
	body := strings.TrimPrefix(reply, "stream: ")
	switch {
	case body == "OK":
		return domain.ScanVerdict{}, nil
	case strings.HasSuffix(body, " FOUND"):
		return domain.ScanVerdict{Infected: true, Signature: strings.TrimSuffix(body, " FOUND")}, nil
	case strings.HasSuffix(body, "ERROR"):
		return domain.ScanVerdict{}, &domain.ScanFileError{Reason: strings.TrimSpace(strings.TrimSuffix(body, "ERROR"))}
	}
	return domain.ScanVerdict{}, &domain.ScanFileError{Reason: "unexpected answer: " + reply}
}
