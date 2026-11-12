// Package emaildomain decides whether an email domain may sign up (FR-17.2): not a known
// disposable domain, and able to receive email. It implements the auth domain's
// EmailDomainChecker with the standard library resolver and an embedded list.
//
// disposable_domains.txt is disposable_email_blocklist.conf from
// https://github.com/disposable-email-domains/disposable-email-domains (CC0 1.0), one domain
// per line. `make update-disposable-domains` downloads the latest one.
package emaildomain

import (
	"bufio"
	"context"
	_ "embed"
	"errors"
	"log/slog"
	"net"
	"net/netip"
	"strings"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/auth/domain"
)

// LookupTimeout bounds the DNS lookups of one check, MX and addresses together.
const LookupTimeout = 2 * time.Second

//go:embed disposable_domains.txt
var disposableList string

// Resolver is the part of net.Resolver the checker uses; tests pass a fake.
type Resolver interface {
	LookupMX(ctx context.Context, name string) ([]*net.MX, error)
	LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error)
}

// Checker implements domain.EmailDomainChecker.
type Checker struct {
	resolver   Resolver
	disposable map[string]struct{}
	timeout    time.Duration
	logger     *slog.Logger
}

var _ domain.EmailDomainChecker = (*Checker)(nil)

// New returns a Checker that asks the system's resolver.
func New(logger *slog.Logger) *Checker {
	return NewWithResolver(net.DefaultResolver, LookupTimeout, logger)
}

// NewWithResolver returns a Checker that asks r, each check taking at most timeout.
func NewWithResolver(r Resolver, timeout time.Duration, logger *slog.Logger) *Checker {
	return &Checker{resolver: r, disposable: parseList(disposableList), timeout: timeout, logger: logger}
}

func parseList(list string) map[string]struct{} {
	set := map[string]struct{}{}
	sc := bufio.NewScanner(strings.NewReader(list))
	for sc.Scan() {
		d := strings.ToLower(strings.TrimSpace(sc.Text()))
		if d != "" && !strings.HasPrefix(d, "#") {
			set[d] = struct{}{}
		}
	}
	return set
}

// AllowsEmailDomain reports false when the domain, or a domain it belongs to, is on the
// disposable list, or when DNS says for certain that it cannot receive email: a null MX
// (RFC 7505), or no MX and no A or AAAA record (RFC 5321 falls back to the address). Any
// other DNS failure, a timeout included, allows the domain and is logged: a slow resolver
// must not stop people from signing up.
func (c *Checker) AllowsEmailDomain(ctx context.Context, d string) bool {
	d = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(d)), ".")
	if c.isDisposable(d) {
		return false
	}
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	mx, err := c.resolver.LookupMX(ctx, d)
	switch {
	case err == nil && len(mx) > 0:
		if isNullMX(mx) {
			return false
		}
		return true
	case err != nil && !isNotFound(err):
		c.logLookupFailure(ctx, d, "mx", err)
		return true
	}

	// No MX record: mail goes to the domain's own address, if it has one.
	addrs, err := c.resolver.LookupNetIP(ctx, "ip", d)
	switch {
	case err == nil && len(addrs) > 0:
		return true
	case err == nil || isNotFound(err):
		return false
	default:
		c.logLookupFailure(ctx, d, "address", err)
		return true
	}
}

// isDisposable checks d and every parent domain with at least two labels, so
// x.mailinator.com is caught by mailinator.com.
func (c *Checker) isDisposable(d string) bool {
	for {
		if _, ok := c.disposable[d]; ok {
			return true
		}
		_, parent, ok := strings.Cut(d, ".")
		if !ok || !strings.Contains(parent, ".") {
			return false
		}
		d = parent
	}
}

// isNullMX reports an RFC 7505 null MX: the domain says it accepts no email. It is a single
// record whose host is the root (".").
func isNullMX(mx []*net.MX) bool {
	return len(mx) == 1 && (mx[0].Host == "." || mx[0].Host == "")
}

// isNotFound reports a definite "no such records" answer (NXDOMAIN or no data), as opposed
// to a failure to get an answer.
func isNotFound(err error) bool {
	var dnsErr *net.DNSError
	return errors.As(err, &dnsErr) && dnsErr.IsNotFound
}

// logLookupFailure logs the domain, never the address (the local part is personal data).
func (c *Checker) logLookupFailure(ctx context.Context, d, kind string, err error) {
	c.logger.WarnContext(ctx, "email domain lookup failed; sign-up allowed",
		slog.String("domain", d), slog.String("lookup", kind), slog.Any("error", err))
}
