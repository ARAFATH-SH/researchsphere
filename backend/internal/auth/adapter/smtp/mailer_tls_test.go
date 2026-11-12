package smtp

import (
	"bufio"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"errors"
	"io"
	"math/big"
	"net"
	"net/mail"
	"net/textproto"
	"strings"
	"testing"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/auth/domain"
)

// selfSigned returns a certificate for 127.0.0.1 and a pool that trusts it.
func selfSigned(t *testing.T) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "fake smtp"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(cert)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, pool
}

// tlsServerOptions shape providerServer.
type tlsServerOptions struct {
	implicitTLS bool // TLS from the first byte (port 465)
	offerTLS    bool // advertise STARTTLS
}

// session is what providerServer saw on one connection.
type session struct {
	encrypted    bool   // TLS was on when MAIL FROM arrived
	authUser     string // from AUTH PLAIN
	authPassword string
	mailFrom     bool // a MAIL FROM arrived at all
	data         string
}

// providerServer is a fake mail provider: STARTTLS or implicit TLS, and AUTH PLAIN.
func providerServer(t *testing.T, o tlsServerOptions) (port int, pool *x509.CertPool, got <-chan session) {
	t.Helper()
	cert, pool := selfSigned(t)
	cfg := &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}
	var lc net.ListenConfig
	ln, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	ch := make(chan session, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		var s session
		defer func() { ch <- s }()
		encrypted := o.implicitTLS
		if o.implicitTLS {
			conn = tls.Server(conn, cfg)
		}
		defer func() { _ = conn.Close() }()
		tp := textproto.NewConn(conn)
		_ = tp.PrintfLine("220 fake provider ESMTP")
		for {
			line, err := tp.ReadLine()
			if err != nil {
				return
			}
			verb, arg, _ := strings.Cut(line, " ")
			switch strings.ToUpper(verb) {
			case "EHLO":
				lines := []string{"250-fake"}
				if o.offerTLS && !encrypted {
					lines = append(lines, "250-STARTTLS")
				}
				lines = append(lines, "250 AUTH PLAIN")
				for _, l := range lines {
					_ = tp.PrintfLine("%s", l)
				}
			case "STARTTLS":
				_ = tp.PrintfLine("220 go ahead")
				tc := tls.Server(conn, cfg)
				if err := tc.HandshakeContext(t.Context()); err != nil {
					return
				}
				conn, encrypted = tc, true
				tp = textproto.NewConn(conn)
			case "AUTH":
				mech, b64, _ := strings.Cut(arg, " ")
				raw, err := base64.StdEncoding.DecodeString(b64)
				parts := strings.Split(string(raw), "\x00")
				if mech != "PLAIN" || err != nil || len(parts) != 3 {
					_ = tp.PrintfLine("535 bad auth")
					continue
				}
				s.authUser, s.authPassword = parts[1], parts[2]
				_ = tp.PrintfLine("235 OK")
			case "MAIL":
				s.mailFrom, s.encrypted = true, encrypted
				_ = tp.PrintfLine("250 OK")
			case "RCPT":
				_ = tp.PrintfLine("250 OK")
			case "DATA":
				_ = tp.PrintfLine("354 go ahead")
				b, err := io.ReadAll(tp.DotReader())
				if err != nil {
					return
				}
				s.data = string(b)
				_ = tp.PrintfLine("250 OK")
			case "QUIT":
				_ = tp.PrintfLine("221 bye")
				return
			default:
				_ = tp.PrintfLine("502 not implemented")
			}
		}
	}()
	return ln.Addr().(*net.TCPAddr).Port, pool, ch
}

func providerMailer(port int, pool *x509.CertPool, mode TLSMode) *Mailer {
	m := New(Options{
		Host: "127.0.0.1", Port: port, TLS: mode,
		From:     mail.Address{Name: "ResearchSphere", Address: "no-reply@rs.example"},
		Username: "apikey", Password: "provider-secret",
	})
	m.rootCAs = pool
	return m
}

func waitSession(t *testing.T, got <-chan session) session {
	t.Helper()
	select {
	case s := <-got:
		return s
	case <-time.After(5 * time.Second):
		t.Fatal("the server saw no session")
		return session{}
	}
}

// FR-17.1: with a provider, the message goes encrypted and logged in, and carries a
// Message-ID at the sender's domain.
func TestSendThroughProvider(t *testing.T) {
	for _, tt := range []struct {
		name string
		mode TLSMode
		opts tlsServerOptions
	}{
		{"starttls", StartTLS, tlsServerOptions{offerTLS: true}},
		{"implicit tls", ImplicitTLS, tlsServerOptions{implicitTLS: true}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			port, pool, got := providerServer(t, tt.opts)
			m := providerMailer(port, pool, tt.mode)
			if err := m.SendVerification(t.Context(), domain.VerificationEmail{To: "ada@example.org", FullName: "Ada", Link: "https://rs.example/verify-email#token=x"}); err != nil {
				t.Fatalf("SendVerification: %v", err)
			}
			s := waitSession(t, got)
			if !s.encrypted {
				t.Error("MAIL FROM was sent before TLS was on")
			}
			if s.authUser != "apikey" || s.authPassword != "provider-secret" {
				t.Errorf("auth = %q / %q, want apikey / provider-secret", s.authUser, s.authPassword)
			}
			msg, err := mail.ReadMessage(bufio.NewReader(strings.NewReader(s.data)))
			if err != nil {
				t.Fatal(err)
			}
			if id := msg.Header.Get("Message-ID"); !strings.HasPrefix(id, "<") || !strings.HasSuffix(id, "@rs.example>") {
				t.Errorf("Message-ID = %q, want <...@rs.example>", id)
			}
		})
	}
}

// SMTP_TLS=starttls never falls back to plain text: a server that does not offer STARTTLS
// gets no login and no message.
func TestStartTLSRequired(t *testing.T) {
	port, pool, got := providerServer(t, tlsServerOptions{offerTLS: false})
	m := providerMailer(port, pool, StartTLS)
	err := m.SendVerification(t.Context(), domain.VerificationEmail{To: "ada@example.org", Link: "https://rs.example/verify-email#token=x"})
	if !errors.Is(err, errNoStartTLS) {
		t.Fatalf("error = %v, want errNoStartTLS", err)
	}
	s := waitSession(t, got)
	if s.authPassword != "" || s.mailFrom {
		t.Errorf("server saw auth %q or MAIL FROM %v without TLS", s.authPassword, s.mailFrom)
	}
}

// A certificate the system does not trust is refused, so a provider cannot be impersonated.
func TestUntrustedCertificateRefused(t *testing.T) {
	port, _, got := providerServer(t, tlsServerOptions{offerTLS: true})
	m := providerMailer(port, nil, StartTLS) // the system pool does not trust the test certificate
	if err := m.SendVerification(t.Context(), domain.VerificationEmail{To: "ada@example.org", Link: "https://rs.example/verify-email#token=x"}); err == nil {
		t.Fatal("error = nil, want a certificate error")
	}
	if s := waitSession(t, got); s.authPassword != "" || s.mailFrom {
		t.Errorf("server saw auth or MAIL FROM after a failed handshake")
	}
}
