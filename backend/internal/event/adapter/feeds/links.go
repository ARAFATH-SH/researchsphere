package feeds

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"syscall"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/event/domain"
)

// Defaults of a link check.
const (
	DefaultLinkTimeout = 5 * time.Second
	maxRedirects       = 5
	maxProbeBytes      = 64 << 10
)

// LinkConfig sets up a LinkChecker.
type LinkConfig struct {
	Contact string
	Timeout time.Duration
	// AllowPrivate lets checks reach loopback and private addresses: tests only.
	AllowPrivate bool
	// Now is the clock of a check's time; nil means time.Now.
	Now func() time.Time
}

// Links checks that a journal homepage answers over https (docs/decisions.md, 2026-11-24):
// a HEAD request (a GET when HEAD is not allowed) with a short timeout, at most 5 redirects
// that all stay https, never to a private, loopback or link-local address.
type Links struct {
	http      *http.Client
	userAgent string
	now       func() time.Time
}

var _ domain.LinkChecker = (*Links)(nil)

// errNotHTTPS: a redirect left https.
var errNotHTTPS = errors.New("redirect away from https")

// errPrivate: the address is not on the public internet.
var errPrivate = errors.New("address is not public")

// cgnat is the carrier-grade NAT range, private in practice.
var cgnat = netip.MustParsePrefix("100.64.0.0/10")

// publicAddr says whether an address may be checked.
func publicAddr(a netip.Addr) bool {
	a = a.Unmap()
	return a.IsValid() && !a.IsLoopback() && !a.IsPrivate() && !a.IsLinkLocalUnicast() && !a.IsLinkLocalMulticast() &&
		!a.IsInterfaceLocalMulticast() && !a.IsMulticast() && !a.IsUnspecified() && !cgnat.Contains(a)
}

// NewLinks returns a LinkChecker.
func NewLinks(cfg LinkConfig) *Links {
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = DefaultLinkTimeout
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	dialer := &net.Dialer{Timeout: timeout}
	if !cfg.AllowPrivate {
		// Checked on the address actually dialled, after DNS: a name that resolves to a
		// private address is refused too.
		dialer.Control = func(_, address string, _ syscall.RawConn) error {
			ap, err := netip.ParseAddrPort(address)
			if err != nil || !publicAddr(ap.Addr()) {
				return errPrivate
			}
			return nil
		}
	}
	transport := &http.Transport{
		DialContext:           dialer.DialContext,
		TLSHandshakeTimeout:   timeout,
		ResponseHeaderTimeout: timeout,
		MaxIdleConnsPerHost:   1,
		IdleConnTimeout:       30 * time.Second,
		Proxy:                 nil,
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxRedirects {
				return http.ErrUseLastResponse
			}
			if req.URL.Scheme != "https" {
				return errNotHTTPS
			}
			return nil
		},
	}
	return &Links{http: client, userAgent: UserAgent(cfg.Contact), now: cfg.Now}
}

// Check asks for u. It is OK when an answer comes over https and says the page exists: any
// status below 500 except 404 and 410 (many publishers answer 403 to anything but a
// browser, and the page is still there for a person). A timeout, a TLS failure, a redirect
// away from https or too many redirects is not OK.
func (l *Links) Check(ctx context.Context, u string) domain.LinkCheck {
	out := domain.LinkCheck{CheckedAt: l.now()}
	if !strings.HasPrefix(u, "https://") {
		return out
	}
	status, err := l.request(ctx, http.MethodHead, u)
	if err == nil && (status == http.StatusMethodNotAllowed || status == http.StatusNotImplemented) {
		status, err = l.request(ctx, http.MethodGet, u)
	}
	if err != nil {
		return out
	}
	out.FinalStatus = status
	out.OK = status < 500 && status != http.StatusNotFound && status != http.StatusGone &&
		status != http.StatusMovedPermanently && status != http.StatusFound &&
		status != http.StatusSeeOther && status != http.StatusTemporaryRedirect && status != http.StatusPermanentRedirect
	return out
}

func (l *Links) request(ctx context.Context, method, u string) (int, error) {
	req, err := http.NewRequestWithContext(ctx, method, u, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("User-Agent", l.userAgent)
	req.Header.Set("Accept", "text/html,*/*;q=0.8")
	resp, err := l.http.Do(req)
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxProbeBytes))
	if resp.Request != nil && resp.Request.URL.Scheme != "https" {
		return 0, errNotHTTPS
	}
	return resp.StatusCode, nil
}
