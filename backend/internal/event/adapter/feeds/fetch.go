// Package feeds reads the automatic venue import's sources over HTTPS (FR-14.5;
// docs/venues-sync.md): deadline repositories on GitHub, journals from OpenAlex, and journal
// homepages. Every request is polite: a User-Agent with the operator's contact address,
// timeouts, retries with backoff that honour Retry-After, a pace between requests, and a
// cap on what is read. It imports only the event domain.
package feeds

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	neturl "net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Defaults of a fetcher.
const (
	DefaultTimeout   = 15 * time.Second
	DefaultPace      = 250 * time.Millisecond // at most 4 requests a second
	DefaultRetries   = 3
	DefaultBackoff   = time.Second // then 4 s, then 16 s
	maxRetryAfter    = time.Minute
	projectUserAgent = "ResearchSphere/1.0 (+https://github.com/ARAFATH-SH/researchsphere"
)

// UserAgent is the User-Agent of every request: the project and the operator's contact
// address, as GitHub and OpenAlex ask of automated clients.
func UserAgent(contact string) string {
	if contact == "" {
		return projectUserAgent + ")"
	}
	return projectUserAgent + "; mailto:" + contact + ")"
}

// fetcher makes polite GET requests to one service.
type fetcher struct {
	http      *http.Client
	userAgent string
	pace      time.Duration
	retries   int
	backoff   time.Duration

	mu   sync.Mutex
	last time.Time
}

func newFetcher(client *http.Client, userAgent string, pace time.Duration, retries int, backoff time.Duration) *fetcher {
	if client == nil {
		client = &http.Client{Timeout: DefaultTimeout}
	}
	if pace < 0 {
		pace = 0
	}
	if retries < 0 {
		retries = 0
	}
	if backoff <= 0 {
		backoff = DefaultBackoff
	}
	return &fetcher{http: client, userAgent: userAgent, pace: pace, retries: retries, backoff: backoff}
}

// statusError is an answer that is not 200 or 304. ResetAt is set when the service said its
// rate limit is used up (GitHub: 403 or 429 with X-RateLimit-Remaining: 0).
type statusError struct {
	URL     string
	Status  int
	ResetAt time.Time
}

func (e *statusError) Error() string {
	if !e.ResetAt.IsZero() {
		return fmt.Sprintf("%s answered %d: the rate limit is used up until %s", e.URL, e.Status, e.ResetAt.UTC().Format(time.RFC3339))
	}
	return fmt.Sprintf("%s answered %d", e.URL, e.Status)
}

// IsStatus says whether err is an answer with that status.
func IsStatus(err error, status int) bool {
	var se *statusError
	return errors.As(err, &se) && se.Status == status
}

// response is a body read in full (at most the cap) with the headers that matter.
type response struct {
	Status int
	ETag   string
	Body   []byte
}

// errTooLarge: the body is over the cap.
var errTooLarge = errors.New("the answer is larger than allowed")

// wait keeps the pace: at most one request per pace.
func (f *fetcher) wait(ctx context.Context) error {
	f.mu.Lock()
	next := f.last.Add(f.pace)
	now := time.Now()
	if next.Before(now) {
		next = now
	}
	f.last = next
	f.mu.Unlock()
	if d := time.Until(next); d > 0 {
		t := time.NewTimer(d)
		defer t.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
		}
	}
	return nil
}

// get fetches url with the headers given. A 304 answers with an empty body. A timeout, a
// connection error, a 429 or a 5xx is tried again up to retries times, after 1, 4, 16 s
// (times backoff, with up to a quarter more at random) or the Retry-After the answer gives.
// Any other status but 200 is a statusError.
func (f *fetcher) get(ctx context.Context, url string, headers map[string]string, maxBytes int64) (response, error) {
	for attempt := 0; ; attempt++ {
		if err := f.wait(ctx); err != nil {
			return response{}, err
		}
		resp, retryAfter, err := f.once(ctx, url, headers, maxBytes)
		if err == nil {
			return resp, nil
		}
		if ctx.Err() != nil {
			return response{}, ctx.Err()
		}
		if !retryable(err) || attempt >= f.retries {
			return response{}, err
		}
		delay := f.backoff
		for range attempt {
			delay *= 4
		}
		delay += time.Duration(rand.Int64N(int64(delay)/4 + 1)) //nolint:gosec // jitter spreads retries; no secret
		if retryAfter > 0 {
			delay = retryAfter
		}
		t := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			t.Stop()
			return response{}, ctx.Err()
		case <-t.C:
		}
	}
}

// once makes one attempt; retryAfter is the delay a 429 or 503 asked for.
func (f *fetcher) once(ctx context.Context, url string, headers map[string]string, maxBytes int64) (response, time.Duration, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return response{}, 0, err
	}
	req.Header.Set("User-Agent", f.userAgent)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := f.http.Do(req)
	if err != nil {
		var ue *neturl.Error
		if errors.As(err, &ue) {
			ue.URL = redact(ue.URL)
		}
		return response{}, 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	out := response{Status: resp.StatusCode, ETag: resp.Header.Get("ETag")}
	switch resp.StatusCode {
	case http.StatusNotModified:
		return out, 0, nil
	case http.StatusOK:
	default:
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		se := &statusError{URL: redact(url), Status: resp.StatusCode}
		if resp.Header.Get("X-RateLimit-Remaining") == "0" {
			if n, err := strconv.ParseInt(resp.Header.Get("X-RateLimit-Reset"), 10, 64); err == nil && n > 0 {
				se.ResetAt = time.Unix(n, 0)
			}
		}
		return response{}, parseRetryAfter(resp.Header.Get("Retry-After")), se
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return response{}, 0, err
	}
	if int64(len(body)) > maxBytes {
		return response{}, 0, fmt.Errorf("%s: %w", redact(url), errTooLarge)
	}
	out.Body = body
	return out, 0, nil
}

// retryable is a failure worth another try: a timeout, a dropped connection, 429 or 5xx.
func retryable(err error) bool {
	var se *statusError
	if errors.As(err, &se) {
		// A used-up rate limit lasts up to an hour: waiting here would only hold the run.
		if !se.ResetAt.IsZero() {
			return false
		}
		return se.Status == http.StatusTooManyRequests || se.Status >= 500
	}
	if errors.Is(err, errTooLarge) {
		return false
	}
	var ne net.Error
	if errors.As(err, &ne) {
		return true
	}
	return errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF)
}

// parseRetryAfter reads Retry-After in seconds, at most a minute; 0 when absent or a date.
func parseRetryAfter(s string) time.Duration {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n <= 0 {
		return 0
	}
	d := time.Duration(n) * time.Second
	return min(d, maxRetryAfter)
}

// redact drops the query of a URL from messages: it may hold an API key.
func redact(url string) string {
	if i := strings.IndexByte(url, '?'); i >= 0 {
		return url[:i]
	}
	return url
}
