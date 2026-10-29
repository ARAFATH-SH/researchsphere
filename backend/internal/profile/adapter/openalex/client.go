// Package openalex implements the profile domain's WorkSource with the OpenAlex API
// (FR-5.2): GET /works/doi:<doi>. It imports only the profile domain.
package openalex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/profile/domain"
)

// Defaults for Config.
const (
	DefaultBaseURL    = "https://api.openalex.org"
	DefaultTimeout    = 5 * time.Second
	DefaultRetryDelay = 500 * time.Millisecond
)

// maxBody caps the response read; a work with the selected fields is a few kilobytes.
const maxBody = 1 << 20

// fields are the only fields asked for (OpenAlex's select parameter).
const fields = "id,doi,title,display_name,publication_year,primary_location,abstract_inverted_index"

// userAgent names the project, as OpenAlex asks of API users.
const userAgent = "ResearchSphere/1.0 (+https://github.com/ARAFATH-SH/researchsphere)"

// Config says where OpenAlex is and how long to wait. Zero values take the defaults.
type Config struct {
	BaseURL string
	// Mailto is sent as the mailto parameter, which puts requests in OpenAlex's "polite
	// pool". Empty sends none.
	Mailto     string
	Timeout    time.Duration // per attempt
	RetryDelay time.Duration // before the one retry
}

// Client fetches works from OpenAlex.
type Client struct {
	base       string
	mailto     string
	retryDelay time.Duration
	http       *http.Client
}

var _ domain.WorkSource = (*Client)(nil)

// New returns a Client.
func New(cfg Config) *Client {
	c := &Client{
		base:       strings.TrimRight(cfg.BaseURL, "/"),
		mailto:     cfg.Mailto,
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
var errRetry = errors.New("openalex: retryable failure")

// FetchWork returns the work with the bare, lower-case DOI. A 404 gives
// domain.ErrWorkNotFound. Another 4xx gives domain.ErrMetadataUnavailable at once; a 5xx
// or a timeout is tried once more and then gives domain.ErrMetadataUnavailable, as does
// an answer that is not the JSON of a work. When ctx ends, its error is returned.
func (c *Client) FetchWork(ctx context.Context, doi string) (domain.WorkMetadata, error) {
	w, err := c.fetch(ctx, doi)
	if errors.Is(err, errRetry) {
		select {
		case <-time.After(c.retryDelay):
		case <-ctx.Done():
			return domain.WorkMetadata{}, ctx.Err()
		}
		w, err = c.fetch(ctx, doi)
	}
	switch {
	case err == nil:
		return w, nil
	case ctx.Err() != nil:
		return domain.WorkMetadata{}, ctx.Err()
	case errors.Is(err, domain.ErrWorkNotFound):
		return domain.WorkMetadata{}, err
	default:
		return domain.WorkMetadata{}, fmt.Errorf("%w: %w", domain.ErrMetadataUnavailable, err)
	}
}

// fetch makes one attempt.
func (c *Client) fetch(ctx context.Context, doi string) (domain.WorkMetadata, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.workURL(doi), nil)
	if err != nil {
		return domain.WorkMetadata{}, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.userAgent())
	resp, err := c.http.Do(req)
	if err != nil {
		var ne net.Error
		if errors.As(err, &ne) && ne.Timeout() && ctx.Err() == nil {
			return domain.WorkMetadata{}, fmt.Errorf("%w: %w", errRetry, err)
		}
		return domain.WorkMetadata{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	switch {
	case resp.StatusCode == http.StatusOK:
	case resp.StatusCode == http.StatusNotFound:
		return domain.WorkMetadata{}, domain.ErrWorkNotFound
	case resp.StatusCode >= 500:
		return domain.WorkMetadata{}, fmt.Errorf("%w: status %d", errRetry, resp.StatusCode)
	default:
		return domain.WorkMetadata{}, fmt.Errorf("openalex: status %d", resp.StatusCode)
	}
	var body work
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBody)).Decode(&body); err != nil {
		var ne net.Error
		if errors.As(err, &ne) && ne.Timeout() && ctx.Err() == nil {
			return domain.WorkMetadata{}, fmt.Errorf("%w: %w", errRetry, err)
		}
		return domain.WorkMetadata{}, fmt.Errorf("openalex: decode work: %w", err)
	}
	return body.metadata(doi), nil
}

// workURL is {base}/works/doi:{doi}, with every character of the DOI but the slashes
// escaped, and the select and mailto parameters.
func (c *Client) workURL(doi string) string {
	q := url.Values{"select": {fields}}
	if c.mailto != "" {
		q.Set("mailto", c.mailto)
	}
	path := strings.ReplaceAll(url.PathEscape(doi), "%2F", "/")
	return c.base + "/works/doi:" + path + "?" + q.Encode()
}

func (c *Client) userAgent() string {
	if c.mailto == "" {
		return userAgent
	}
	return strings.TrimSuffix(userAgent, ")") + "; mailto:" + c.mailto + ")"
}

// work is the part of an OpenAlex work that is read.
type work struct {
	Title           *string `json:"title"`
	DisplayName     *string `json:"display_name"`
	PublicationYear *int    `json:"publication_year"`
	PrimaryLocation *struct {
		LandingPageURL *string `json:"landing_page_url"`
		Source         *struct {
			DisplayName *string `json:"display_name"`
		} `json:"source"`
	} `json:"primary_location"`
	AbstractInvertedIndex map[string][]int `json:"abstract_inverted_index"`
}

// metadata maps the work to WorkMetadata for doi. The title falls back to the display
// name; the abstract is rebuilt from the inverted index.
func (w work) metadata(doi string) domain.WorkMetadata {
	m := domain.WorkMetadata{DOI: doi, Year: w.PublicationYear}
	switch {
	case w.Title != nil && strings.TrimSpace(*w.Title) != "":
		m.Title = *w.Title
	case w.DisplayName != nil:
		m.Title = *w.DisplayName
	}
	if l := w.PrimaryLocation; l != nil {
		m.URL = l.LandingPageURL
		if l.Source != nil {
			m.Venue = l.Source.DisplayName
		}
	}
	if a := RebuildAbstract(w.AbstractInvertedIndex); a != "" {
		m.Abstract = &a
	}
	return m
}

// RebuildAbstract turns OpenAlex's abstract_inverted_index (each word with the positions
// it stands at) back into text: the words in position order, joined by single spaces.
// Positions no word claims are skipped; of two words at one position, the first in
// byte order is kept. An empty or missing index gives "".
func RebuildAbstract(index map[string][]int) string {
	byPos := make(map[int]string)
	for word, positions := range index {
		for _, p := range positions {
			// Two words at one position should not happen; keep the same one every time.
			if old, ok := byPos[p]; p >= 0 && (!ok || word < old) {
				byPos[p] = word
			}
		}
	}
	positions := make([]int, 0, len(byPos))
	for p := range byPos {
		positions = append(positions, p)
	}
	slices.Sort(positions)
	words := make([]string, len(positions))
	for i, p := range positions {
		words[i] = byPos[p]
	}
	return strings.Join(words, " ")
}
