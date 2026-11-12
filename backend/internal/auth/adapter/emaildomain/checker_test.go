package emaildomain

import (
	"bytes"
	"context"
	"log/slog"
	"net"
	"net/netip"
	"strings"
	"testing"
	"time"
)

// fakeResolver answers from maps; a missing domain is NXDOMAIN. mxErr and ipErr, when set,
// are returned for every lookup of that kind. block makes every lookup wait for the context.
type fakeResolver struct {
	mx    map[string][]*net.MX
	ips   map[string][]netip.Addr
	mxErr error
	ipErr error
	block bool
	calls int
}

func notFound(name string) error {
	return &net.DNSError{Err: "no such host", Name: name, IsNotFound: true}
}

func (r *fakeResolver) LookupMX(ctx context.Context, name string) ([]*net.MX, error) {
	r.calls++
	if r.block {
		<-ctx.Done()
		return nil, &net.DNSError{Err: "i/o timeout", Name: name, IsTimeout: true}
	}
	if r.mxErr != nil {
		return nil, r.mxErr
	}
	if mx, ok := r.mx[name]; ok {
		return mx, nil
	}
	return nil, notFound(name)
}

func (r *fakeResolver) LookupNetIP(ctx context.Context, _, host string) ([]netip.Addr, error) {
	r.calls++
	if r.block {
		<-ctx.Done()
		return nil, &net.DNSError{Err: "i/o timeout", Name: host, IsTimeout: true}
	}
	if r.ipErr != nil {
		return nil, r.ipErr
	}
	if ips, ok := r.ips[host]; ok {
		return ips, nil
	}
	return nil, notFound(host)
}

func newChecker(r Resolver) (*Checker, *bytes.Buffer) {
	var logs bytes.Buffer
	return NewWithResolver(r, 50*time.Millisecond, slog.New(slog.NewTextHandler(&logs, nil))), &logs
}

func TestEmbeddedListLoads(t *testing.T) {
	c, _ := newChecker(&fakeResolver{})
	if len(c.disposable) < 1000 {
		t.Fatalf("disposable list has %d domains, want thousands", len(c.disposable))
	}
	for _, d := range []string{"gmail.com", "example.com", "outlook.com", "diu.edu.bd"} {
		if _, ok := c.disposable[d]; ok {
			t.Errorf("%s is on the disposable list", d)
		}
	}
}

func TestAllowsEmailDomain(t *testing.T) {
	good := []*net.MX{{Host: "mx1.uni.example.", Pref: 10}}
	timeoutErr := &net.DNSError{Err: "i/o timeout", Name: "x", IsTimeout: true}
	tempErr := &net.DNSError{Err: "server misbehaving", Name: "x", IsTemporary: true}

	tests := []struct {
		name      string
		domain    string
		resolver  *fakeResolver
		want      bool
		wantLog   bool // a lookup failure is logged
		wantNoDNS bool // decided without DNS
	}{
		{"domain with MX", "uni.example", &fakeResolver{mx: map[string][]*net.MX{"uni.example": good}}, true, false, false},
		{"upper case and trailing dot", "UNI.Example.", &fakeResolver{mx: map[string][]*net.MX{"uni.example": good}}, true, false, false},
		{"disposable", "mailinator.com", &fakeResolver{mx: map[string][]*net.MX{"mailinator.com": good}}, false, false, true},
		{"subdomain of a disposable domain", "x.y.mailinator.com", &fakeResolver{mx: map[string][]*net.MX{"x.y.mailinator.com": good}}, false, false, true},
		{"null MX (RFC 7505)", "nomail.example", &fakeResolver{mx: map[string][]*net.MX{"nomail.example": {{Host: ".", Pref: 0}}}}, false, false, false},
		{"no MX but an A record", "a-only.example", &fakeResolver{ips: map[string][]netip.Addr{"a-only.example": {netip.MustParseAddr("192.0.2.1")}}}, true, false, false},
		{"no MX but an AAAA record", "aaaa-only.example", &fakeResolver{ips: map[string][]netip.Addr{"aaaa-only.example": {netip.MustParseAddr("2001:db8::1")}}}, true, false, false},
		{"no MX and no address", "nothing.example", &fakeResolver{mx: map[string][]*net.MX{"nothing.example": {}}, ips: map[string][]netip.Addr{"nothing.example": {}}}, false, false, false},
		{"domain does not exist", "nxdomain.example", &fakeResolver{}, false, false, false},
		{"MX lookup times out", "slow.example", &fakeResolver{mxErr: timeoutErr}, true, true, false},
		{"MX lookup fails temporarily", "flaky.example", &fakeResolver{mxErr: tempErr}, true, true, false},
		{"no MX, address lookup fails", "half.example", &fakeResolver{ipErr: tempErr}, true, true, false},
		{"resolver hangs past the timeout", "hang.example", &fakeResolver{block: true}, true, true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, logs := newChecker(tt.resolver)
			start := time.Now()
			if got := c.AllowsEmailDomain(t.Context(), tt.domain); got != tt.want {
				t.Errorf("AllowsEmailDomain(%q) = %v, want %v", tt.domain, got, tt.want)
			}
			if time.Since(start) > time.Second {
				t.Errorf("took %v; the timeout is not applied", time.Since(start))
			}
			if logged := strings.Contains(logs.String(), "email domain lookup failed"); logged != tt.wantLog {
				t.Errorf("logged = %v, want %v: %s", logged, tt.wantLog, logs)
			}
			if tt.wantNoDNS && tt.resolver.calls != 0 {
				t.Errorf("%d DNS lookups for a listed domain, want none", tt.resolver.calls)
			}
		})
	}
}
