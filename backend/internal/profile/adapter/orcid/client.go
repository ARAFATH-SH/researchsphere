// Package orcid implements the profile domain's ORCIDSource with the ORCID public API
// v3.0 (FR-5.3): GET /v3.0/<orcid>/works. It imports only the profile domain.
package orcid

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/profile/domain"
)

// Defaults for Config.
const (
	DefaultBaseURL    = "https://pub.orcid.org"
	DefaultTimeout    = 5 * time.Second
	DefaultRetryDelay = 500 * time.Millisecond
)

// maxBody caps the response read; a work summary is about 2 KB, so this holds a few
// thousand works.
const maxBody = 8 << 20

// userAgent names the project, as for OpenAlex.
const userAgent = "ResearchSphere/1.0 (+https://github.com/ARAFATH-SH/researchsphere)"

// Config says where ORCID is and how long to wait. Zero values take the defaults.
type Config struct {
	BaseURL    string
	Timeout    time.Duration // per attempt
	RetryDelay time.Duration // before the one retry
}

// Client lists works from ORCID.
type Client struct {
	base       string
	retryDelay time.Duration
	http       *http.Client
}

var _ domain.ORCIDSource = (*Client)(nil)

// New returns a Client.
func New(cfg Config) *Client {
	c := &Client{
		base:       strings.TrimRight(cfg.BaseURL, "/"),
		retryDelay: cfg.RetryDelay,
		http:       &http.Client{Timeout: cfg.Timeout},
	}
	if c.base == "" {
		c.base = DefaultBaseURL
	}
	if c.http.Timeout <= 0 {
		c.http.Timeout = DefaultTimeout
	}
	if c.retryDelay <= 0 {
		c.retryDelay = DefaultRetryDelay
	}
	return c
}

// errRetry marks an attempt that failed in a way worth one more try: a 5xx answer or a
// timeout.
var errRetry = errors.New("orcid: retryable failure")

// ListWorks returns the public works of the ORCID iD, one per work group, in ORCID's
// order; works without a title are left out. A 404 gives domain.ErrORCIDNotFound. Another
// 4xx gives domain.ErrMetadataUnavailable at once; a 5xx or a timeout is tried once more
// and then gives domain.ErrMetadataUnavailable, as does an answer that is not the JSON of
// a works list. When ctx ends, its error is returned.
func (c *Client) ListWorks(ctx context.Context, orcidID string) ([]domain.ORCIDWork, error) {
	ws, err := c.fetch(ctx, orcidID)
	if errors.Is(err, errRetry) {
		select {
		case <-time.After(c.retryDelay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		ws, err = c.fetch(ctx, orcidID)
	}
	switch {
	case err == nil:
		return ws, nil
	case ctx.Err() != nil:
		return nil, ctx.Err()
	case errors.Is(err, domain.ErrORCIDNotFound):
		return nil, err
	default:
		return nil, fmt.Errorf("%w: %w", domain.ErrMetadataUnavailable, err)
	}
}

// fetch makes one attempt.
func (c *Client) fetch(ctx context.Context, orcidID string) ([]domain.ORCIDWork, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/v3.0/"+url.PathEscape(orcidID)+"/works", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)
	resp, err := c.http.Do(req)
	if err != nil {
		var ne net.Error
		if errors.As(err, &ne) && ne.Timeout() && ctx.Err() == nil {
			return nil, fmt.Errorf("%w: %w", errRetry, err)
		}
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	switch {
	case resp.StatusCode == http.StatusOK:
	case resp.StatusCode == http.StatusNotFound:
		return nil, domain.ErrORCIDNotFound
	case resp.StatusCode >= 500:
		return nil, fmt.Errorf("%w: status %d", errRetry, resp.StatusCode)
	default:
		return nil, fmt.Errorf("orcid: status %d", resp.StatusCode)
	}
	var body works
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBody)).Decode(&body); err != nil {
		var ne net.Error
		if errors.As(err, &ne) && ne.Timeout() && ctx.Err() == nil {
			return nil, fmt.Errorf("%w: %w", errRetry, err)
		}
		return nil, fmt.Errorf("orcid: decode works: %w", err)
	}
	if body.Group == nil {
		return nil, errors.New("orcid: answer has no group list")
	}
	return body.items(), nil
}

// works is the part of an ORCID works list that is read.
type works struct {
	Group []struct {
		ExternalIDs *externalIDs `json:"external-ids"`
		Summaries   []summary    `json:"work-summary"`
	} `json:"group"`
}

type summary struct {
	PutCode *json.Number `json:"put-code"`
	Title   *struct {
		Title *value `json:"title"`
	} `json:"title"`
	ExternalIDs     *externalIDs `json:"external-ids"`
	URL             *value       `json:"url"`
	Type            *string      `json:"type"`
	PublicationDate *struct {
		Year *value `json:"year"`
	} `json:"publication-date"`
	JournalTitle *value `json:"journal-title"`
}

type externalIDs struct {
	IDs []struct {
		Type         string  `json:"external-id-type"`
		Value        *string `json:"external-id-value"`
		Relationship *string `json:"external-id-relationship"`
	} `json:"external-id"`
}

type value struct {
	Value *string `json:"value"`
}

func (v *value) text() *string {
	if v == nil || v.Value == nil {
		return nil
	}
	return v.Value
}

// items maps each group to one work, from its first summary (ORCID's preferred version).
// A group without a put-code or a title is left out.
func (w works) items() []domain.ORCIDWork {
	out := make([]domain.ORCIDWork, 0, len(w.Group))
	for _, g := range w.Group {
		if len(g.Summaries) == 0 {
			continue
		}
		s := g.Summaries[0]
		var title string
		if s.Title != nil {
			if t := s.Title.Title.text(); t != nil {
				title = *t
			}
		}
		if s.PutCode == nil || strings.TrimSpace(title) == "" {
			continue
		}
		item := domain.ORCIDWork{
			PutCode: s.PutCode.String(),
			Title:   title,
			Venue:   s.JournalTitle.text(),
			Type:    s.Type,
			URL:     s.URL.text(),
		}
		if d := s.PublicationDate; d != nil {
			if y := d.Year.text(); y != nil {
				if n, err := strconv.Atoi(strings.TrimSpace(*y)); err == nil {
					item.Year = &n
				}
			}
		}
		item.DOI = s.ExternalIDs.doi()
		if item.DOI == nil {
			item.DOI = g.ExternalIDs.doi()
		}
		out = append(out, item)
	}
	return out
}

// doi returns the first DOI whose relationship is self, else the first DOI of any other
// relationship (such as part-of, a chapter's book), else nil.
func (e *externalIDs) doi() *string {
	if e == nil {
		return nil
	}
	var other *string
	for _, id := range e.IDs {
		if !strings.EqualFold(id.Type, "doi") || id.Value == nil || strings.TrimSpace(*id.Value) == "" {
			continue
		}
		if id.Relationship != nil && strings.EqualFold(*id.Relationship, "self") {
			return id.Value
		}
		if other == nil {
			other = id.Value
		}
	}
	return other
}
