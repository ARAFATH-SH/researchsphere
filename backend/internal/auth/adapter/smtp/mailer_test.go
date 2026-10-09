package smtp

import (
	"context"
	"errors"
	"io"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/textproto"
	"strings"
	"testing"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/auth/domain"
)

// received is one message accepted by fakeServer.
type received struct {
	from, to string
	data     string
}

// fakeServer is a minimal SMTP server that accepts one message per connection.
func fakeServer(t *testing.T) (host string, port int, got <-chan received) {
	t.Helper()
	var lc net.ListenConfig
	ln, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	ch := make(chan received, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		tp := textproto.NewConn(conn)
		var r received
		_ = tp.PrintfLine("220 fake ESMTP")
		for {
			line, err := tp.ReadLine()
			if err != nil {
				return
			}
			cmd := strings.ToUpper(strings.SplitN(line, " ", 2)[0])
			switch cmd {
			case "EHLO", "HELO":
				_ = tp.PrintfLine("250 fake")
			case "MAIL":
				r.from = line
				_ = tp.PrintfLine("250 OK")
			case "RCPT":
				r.to = line
				_ = tp.PrintfLine("250 OK")
			case "DATA":
				_ = tp.PrintfLine("354 go ahead")
				b, err := io.ReadAll(tp.DotReader())
				if err != nil {
					return
				}
				r.data = string(b)
				_ = tp.PrintfLine("250 OK")
			case "QUIT":
				_ = tp.PrintfLine("221 bye")
				ch <- r
				return
			default:
				_ = tp.PrintfLine("502 not implemented")
			}
		}
	}()
	addr := ln.Addr().(*net.TCPAddr)
	return "127.0.0.1", addr.Port, ch
}

func TestSendVerification(t *testing.T) {
	host, port, got := fakeServer(t)
	m := New(host, port, mail.Address{Name: "ResearchSphere", Address: "no-reply@rs.example"})
	link := "https://rs.example/verify-email#token=" + strings.Repeat("A", 43)

	err := m.SendVerification(t.Context(), domain.VerificationEmail{To: "ada@example.org", FullName: "Ada Lovelace", Link: link})
	if err != nil {
		t.Fatalf("SendVerification() error = %v", err)
	}

	var r received
	select {
	case r = <-got:
	case <-time.After(5 * time.Second):
		t.Fatal("server received nothing")
	}
	if r.from != "MAIL FROM:<no-reply@rs.example>" || !strings.HasPrefix(r.to, "RCPT TO:<ada@example.org>") {
		t.Errorf("envelope from %q to %q", r.from, r.to)
	}

	msg, err := mail.ReadMessage(strings.NewReader(r.data))
	if err != nil {
		t.Fatalf("message does not parse: %v\n%s", err, r.data)
	}
	if got := msg.Header.Get("To"); got != `"Ada Lovelace" <ada@example.org>` {
		t.Errorf("To = %q", got)
	}
	if got := msg.Header.Get("From"); got != `"ResearchSphere" <no-reply@rs.example>` {
		t.Errorf("From = %q", got)
	}
	body, err := io.ReadAll(quotedprintable.NewReader(msg.Body))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), link) {
		t.Errorf("body lacks the link:\n%s", body)
	}
	if !strings.Contains(string(body), "Hello Ada Lovelace,") {
		t.Errorf("body lacks the greeting:\n%s", body)
	}
}

func TestSendVerificationCannotInjectHeaders(t *testing.T) {
	host, port, got := fakeServer(t)
	m := New(host, port, mail.Address{Address: "no-reply@rs.example"})

	err := m.SendVerification(t.Context(), domain.VerificationEmail{
		To: "ada@example.org", FullName: "Ada\r\nBcc: victim@evil.test", Link: "https://rs.example/verify-email#token=x",
	})
	if err != nil {
		t.Fatal(err)
	}
	r := <-got
	msg, err := mail.ReadMessage(strings.NewReader(r.data))
	if err != nil {
		t.Fatal(err)
	}
	if bcc := msg.Header.Get("Bcc"); bcc != "" {
		t.Errorf("injected Bcc header %q", bcc)
	}
	for _, line := range strings.Split(r.data, "\r\n") {
		if strings.HasPrefix(line, "Bcc:") {
			t.Errorf("message has a Bcc line: %q", line)
		}
	}
}

func TestSendVerificationRefusesBrokenRecipient(t *testing.T) {
	m := New("127.0.0.1", 1, mail.Address{Address: "no-reply@rs.example"})
	err := m.SendVerification(t.Context(), domain.VerificationEmail{To: "a@example.org\r\nBcc: x@evil.test", Link: "x"})
	if err == nil {
		t.Fatal("error = nil, want a refusal")
	}
}

func TestSendVerificationServerDown(t *testing.T) {
	var lc net.ListenConfig
	ln, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close() // nothing listens any more

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	m := New("127.0.0.1", port, mail.Address{Address: "no-reply@rs.example"})
	err = m.SendVerification(ctx, domain.VerificationEmail{To: "a@example.org", Link: "https://rs.example/verify-email#token=secret"})
	if err == nil {
		t.Fatal("error = nil, want a dial error")
	}
	if strings.Contains(err.Error(), "secret") {
		t.Errorf("error %q contains the link", err)
	}
	var opErr *net.OpError
	if !errors.As(err, &opErr) {
		t.Errorf("error %v does not wrap the dial error", err)
	}
}
