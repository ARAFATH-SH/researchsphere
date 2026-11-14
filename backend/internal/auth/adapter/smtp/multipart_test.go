package smtp

import (
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"strings"
	"testing"
)

func TestSendMultipart(t *testing.T) {
	host, port, got := fakeServer(t)
	m := New(Options{Host: host, Port: port, From: mail.Address{Name: "ResearchSphere", Address: "no-reply@rs.example"}, TLS: NoTLS})

	err := m.SendMultipart(t.Context(), Message{
		To: "ada@example.org", ToName: "Ada\r\nBcc: victim@evil.test", Subject: "You have 3 unread messages\r\nBcc: victim@evil.test",
		Text: "Plain version ✓", HTML: "<p>HTML version ✓</p>",
		ListUnsubscribeURL: "https://rs.example/api/v1/unsubscribe?token=abc.def",
	})
	if err != nil {
		t.Fatalf("SendMultipart() error = %v", err)
	}
	r := <-got
	msg, err := mail.ReadMessage(strings.NewReader(r.data))
	if err != nil {
		t.Fatalf("message does not parse: %v\n%s", err, r.data)
	}
	if bcc := msg.Header.Get("Bcc"); bcc != "" {
		t.Errorf("injected Bcc header: %q", bcc)
	}
	if got, want := msg.Header.Get("List-Unsubscribe"), "<https://rs.example/api/v1/unsubscribe?token=abc.def>"; got != want {
		t.Errorf("List-Unsubscribe = %q, want %q", got, want)
	}
	if got, want := msg.Header.Get("List-Unsubscribe-Post"), "List-Unsubscribe=One-Click"; got != want {
		t.Errorf("List-Unsubscribe-Post = %q, want %q", got, want)
	}
	mt, params, err := mime.ParseMediaType(msg.Header.Get("Content-Type"))
	if err != nil || mt != "multipart/alternative" {
		t.Fatalf("Content-Type = %q (%v)", msg.Header.Get("Content-Type"), err)
	}
	mr := multipart.NewReader(msg.Body, params["boundary"])
	wantParts := []struct{ typ, body string }{
		{"text/plain; charset=utf-8", "Plain version ✓"},
		{"text/html; charset=utf-8", "<p>HTML version ✓</p>"},
	}
	for _, w := range wantParts {
		p, err := mr.NextRawPart()
		if err != nil {
			t.Fatalf("part %s: %v", w.typ, err)
		}
		if got := p.Header.Get("Content-Type"); got != w.typ {
			t.Errorf("part type = %q, want %q", got, w.typ)
		}
		b, err := io.ReadAll(quotedprintable.NewReader(p))
		if err != nil {
			t.Fatal(err)
		}
		if got := strings.TrimSpace(string(b)); got != w.body {
			t.Errorf("part %s body = %q, want %q", w.typ, got, w.body)
		}
	}
	if _, err := mr.NextRawPart(); err != io.EOF {
		t.Errorf("more than two parts: %v", err)
	}
}

func TestSendMultipartWithoutUnsubscribe(t *testing.T) {
	m := New(Options{Host: "127.0.0.1", Port: 1, From: mail.Address{Address: "no-reply@rs.example"}, TLS: NoTLS})
	data, err := m.multipartMessage(mail.Address{Address: "ada@example.org"}, Message{Subject: "Hi", Text: "a", HTML: "b"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "List-Unsubscribe") {
		t.Error("List-Unsubscribe headers without a URL")
	}
}

func TestSendMultipartRefusesBrokenHeaders(t *testing.T) {
	m := New(Options{Host: "127.0.0.1", Port: 1, From: mail.Address{Address: "no-reply@rs.example"}, TLS: NoTLS})
	for name, msg := range map[string]Message{
		"recipient with a line break": {To: "ada@example.org\r\nBcc: x@evil.test"},
		"URL with a line break":       {To: "ada@example.org", ListUnsubscribeURL: "https://rs.example/u\r\nBcc: x@evil.test"},
		"URL closing the bracket":     {To: "ada@example.org", ListUnsubscribeURL: "https://rs.example/u>, <mailto:x@evil.test"},
	} {
		t.Run(name, func(t *testing.T) {
			// Port 1 refuses connections: an error before dialing is the refusal under test.
			err := m.SendMultipart(t.Context(), msg)
			if err == nil || strings.Contains(err.Error(), "dial") {
				t.Errorf("SendMultipart() error = %v, want a refusal before sending", err)
			}
		})
	}
}
