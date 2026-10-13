// Package smtp implements the auth domain's Mailer over SMTP. In development it talks
// to Mailpit (docker-compose.yml), which shows the messages at http://localhost:8025.
package smtp

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/auth/domain"
)

// DefaultTimeout bounds one delivery when the context has no deadline.
const DefaultTimeout = 10 * time.Second

// Mailer sends plain-text email through one SMTP server. It uses STARTTLS when the
// server offers it; it does not log in (Mailpit needs neither).
type Mailer struct {
	addr string
	host string
	from mail.Address
	now  func() time.Time
}

var _ domain.Mailer = (*Mailer)(nil)

// New returns a Mailer for the server at host:port that sends from from.
func New(host string, port int, from mail.Address) *Mailer {
	return &Mailer{addr: net.JoinHostPort(host, strconv.Itoa(port)), host: host, from: from, now: time.Now}
}

// SendVerification sends the verification link. Errors never contain the message,
// so they are safe to log.
func (m *Mailer) SendVerification(ctx context.Context, msg domain.VerificationEmail) error {
	name := oneLine(msg.FullName)
	body := fmt.Sprintf(`Hello %s,

Please confirm your email address for ResearchSphere by opening this link:

%s

The link works once and expires in 24 hours. If it has expired, open it anyway:
the page lets you ask for a new one. If you did not register, you can ignore this email.
`, name, msg.Link)
	return m.send(ctx, mail.Address{Name: name, Address: msg.To}, "Confirm your email address", body)
}

// SendPasswordReset sends the password reset link (FR-2.3). Errors never contain the
// message, so they are safe to log.
func (m *Mailer) SendPasswordReset(ctx context.Context, msg domain.PasswordResetEmail) error {
	name := oneLine(msg.FullName)
	body := fmt.Sprintf(`Hello %s,

Someone asked to reset the password of your ResearchSphere account. To choose a new
password, open this link:

%s

The link works once and expires in 1 hour. Setting a new password signs you out on
every device. If you did not ask for this, you can ignore this email: your password
stays the same.
`, name, msg.Link)
	return m.send(ctx, mail.Address{Name: name, Address: msg.To}, "Reset your ResearchSphere password", body)
}

func (m *Mailer) send(ctx context.Context, to mail.Address, subject, body string) error {
	data, err := m.message(to, subject, body)
	if err != nil {
		return err
	}

	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, DefaultTimeout)
		defer cancel()
	}
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", m.addr)
	if err != nil {
		return fmt.Errorf("smtp: dial: %w", err)
	}
	deadline, _ := ctx.Deadline()
	if err := conn.SetDeadline(deadline); err != nil {
		_ = conn.Close()
		return fmt.Errorf("smtp: set deadline: %w", err)
	}

	c, err := smtp.NewClient(conn, m.host)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("smtp: greeting: %w", err)
	}
	defer func() { _ = c.Close() }()

	if ok, _ := c.Extension("STARTTLS"); ok {
		if err := c.StartTLS(&tls.Config{ServerName: m.host, MinVersion: tls.VersionTLS12}); err != nil {
			return fmt.Errorf("smtp: starttls: %w", err)
		}
	}
	if err := c.Mail(m.from.Address); err != nil {
		return fmt.Errorf("smtp: mail from: %w", err)
	}
	if err := c.Rcpt(to.Address); err != nil {
		return fmt.Errorf("smtp: rcpt to: %w", err)
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("smtp: data: %w", err)
	}
	if _, err := w.Write(data); err != nil {
		return fmt.Errorf("smtp: write message: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("smtp: end message: %w", err)
	}
	if err := c.Quit(); err != nil {
		return fmt.Errorf("smtp: quit: %w", err)
	}
	return nil
}

// message builds the headers and a quoted-printable UTF-8 body. mail.Address.String
// quotes or encodes the display name, so a name cannot add headers.
func (m *Mailer) message(to mail.Address, subject, body string) ([]byte, error) {
	if strings.ContainsAny(to.Address, "\r\n") {
		return nil, fmt.Errorf("smtp: recipient address contains a line break")
	}
	var buf bytes.Buffer
	header := func(k, v string) { buf.WriteString(k + ": " + v + "\r\n") }
	header("From", m.from.String())
	header("To", to.String())
	header("Subject", mime.QEncoding.Encode("utf-8", subject))
	header("Date", m.now().Format(time.RFC1123Z))
	header("MIME-Version", "1.0")
	header("Content-Type", "text/plain; charset=utf-8")
	header("Content-Transfer-Encoding", "quoted-printable")
	buf.WriteString("\r\n")

	qp := quotedprintable.NewWriter(&buf)
	if _, err := qp.Write([]byte(strings.ReplaceAll(body, "\n", "\r\n"))); err != nil {
		return nil, fmt.Errorf("smtp: encode body: %w", err)
	}
	if err := qp.Close(); err != nil {
		return nil, fmt.Errorf("smtp: encode body: %w", err)
	}
	return buf.Bytes(), nil
}

// oneLine removes line breaks so a name stays on one line.
func oneLine(s string) string {
	return strings.Join(strings.Fields(strings.NewReplacer("\r", " ", "\n", " ").Replace(s)), " ")
}
