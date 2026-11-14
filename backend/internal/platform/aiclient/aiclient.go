// Package aiclient calls the internal AI service (ai/, AI-1) for text embeddings.
//
// The service is optional (NFR-10.2): callers treat an error as "AI unavailable" and carry
// on. Each Embed call tries at most twice: it retries once after a 5xx answer, a timeout
// or a connection error, never after a 4xx answer or once the caller's context is done.
package aiclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"
)

// Dimensions is the size of every embedding: the vector(768) columns (docs/decisions.md).
const Dimensions = 768

// Limits of one Embed call, the same as the service's.
const (
	MaxTexts     = 32
	MaxTextChars = 8000
)

// Defaults for Config.
const (
	// DefaultTimeout bounds one attempt. The service gives up after 30 s (504), so this
	// leaves it time to say so.
	DefaultTimeout    = 35 * time.Second
	DefaultRetryDelay = 200 * time.Millisecond
)

// responseLimit caps the bytes read from the service: 32 vectors of 768 numbers as JSON
// text are under 1 MB.
const responseLimit = 4 << 20

// Kind says what the texts are for: e5 embeds stored text and search queries differently.
type Kind string

// The kinds the service accepts.
const (
	KindPassage Kind = "passage" // stored text: profiles, projects, posts, venues
	KindQuery   Kind = "query"   // a search query
)

// Result is what Embed returns: one unit-length vector per text, in order, and the model
// that made them as "name@revision" (AI-1.6).
type Result struct {
	Model   string
	Vectors [][]float32
}

// Embedder is what callers depend on; Client and Fake implement it.
type Embedder interface {
	Embed(ctx context.Context, kind Kind, texts []string) (Result, error)
	Ping(ctx context.Context) error
}

// Errors. A failed call wraps one of them, so callers can use errors.Is.
var (
	// ErrNotConfigured means AI_SERVICE_TOKEN is empty, so the service is not called.
	ErrNotConfigured = errors.New("ai service is not configured")
	// ErrInvalidInput means the texts break the limits; nothing was sent.
	ErrInvalidInput = errors.New("invalid input for the ai service")
	// ErrUnavailable means the service did not answer, timed out, is loading its model or
	// answered with a server error.
	ErrUnavailable = errors.New("ai service unavailable")
	// ErrBadResponse means the service answered 200 with something that is not a valid
	// embedding, or refused the request with a 4xx.
	ErrBadResponse = errors.New("bad response from the ai service")
)

// StatusError is an answer other than 200. Code is the service's error code, such as
// model_loading; the message is not kept, so nothing from the request can leak through it.
type StatusError struct {
	StatusCode int
	Code       string
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("ai service answered %d %s", e.StatusCode, e.Code)
}

// Unwrap classifies the answer: a 5xx is ErrUnavailable, anything else ErrBadResponse.
func (e *StatusError) Unwrap() error {
	if e.StatusCode >= 500 {
		return ErrUnavailable
	}
	return ErrBadResponse
}

// Config holds the client's settings. Token is a secret: never log it.
type Config struct {
	BaseURL    string // such as http://127.0.0.1:8090, without a trailing slash
	Token      string // empty: every call returns ErrNotConfigured
	Timeout    time.Duration
	RetryDelay time.Duration
	HTTPClient *http.Client
}

// Client calls the AI service over HTTP.
type Client struct {
	baseURL    string
	token      string
	timeout    time.Duration
	retryDelay time.Duration
	http       *http.Client
}

var _ Embedder = (*Client)(nil)

// New returns a Client. Zero durations mean the defaults.
func New(cfg Config) *Client {
	c := &Client{
		baseURL:    strings.TrimRight(cfg.BaseURL, "/"),
		token:      cfg.Token,
		timeout:    cfg.Timeout,
		retryDelay: cfg.RetryDelay,
		http:       cfg.HTTPClient,
	}
	if c.timeout <= 0 {
		c.timeout = DefaultTimeout
	}
	if c.retryDelay <= 0 {
		c.retryDelay = DefaultRetryDelay
	}
	if c.http == nil {
		c.http = &http.Client{}
	}
	return c
}

type embedRequest struct {
	Kind  Kind     `json:"kind"`
	Texts []string `json:"texts"`
}

type embedResponse struct {
	Model      string      `json:"model"`
	Dimensions int         `json:"dimensions"`
	Vectors    [][]float32 `json:"vectors"`
}

type errorBody struct {
	Code string `json:"code"`
}

// Embed returns one vector per text. Texts longer than the model's 512 tokens are cut off
// by the service, not refused.
func (c *Client) Embed(ctx context.Context, kind Kind, texts []string) (Result, error) {
	if c.token == "" {
		return Result{}, ErrNotConfigured
	}
	if err := checkInput(kind, texts); err != nil {
		return Result{}, err
	}
	body, err := json.Marshal(embedRequest{Kind: kind, Texts: texts})
	if err != nil {
		return Result{}, fmt.Errorf("encode embed request: %w", err)
	}

	var res Result
	for attempt := 1; ; attempt++ {
		res, err = c.embedOnce(ctx, body, len(texts))
		if err == nil || attempt == 2 || !retryable(err) || ctx.Err() != nil {
			return res, err
		}
		select {
		case <-ctx.Done():
			return Result{}, errors.Join(err, ctx.Err())
		case <-time.After(c.retryDelay):
		}
	}
}

// Ping reports whether the service has loaded its model (GET /ready). It is one attempt
// under the caller's context: the health check has its own timeout.
func (c *Client) Ping(ctx context.Context) error {
	if c.token == "" {
		return ErrNotConfigured
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/ready", nil)
	if err != nil {
		return fmt.Errorf("build ready request: %w", err)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	defer func() {
		// Read what is left so the connection can be reused, then close.
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, responseLimit))
		_ = resp.Body.Close()
	}()
	if resp.StatusCode != http.StatusOK {
		return statusError(resp)
	}
	return nil
}

func (c *Client) embedOnce(ctx context.Context, body []byte, n int) (Result, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/embed", bytes.NewReader(body))
	if err != nil {
		return Result{}, fmt.Errorf("build embed request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.http.Do(req)
	if err != nil {
		// Connection refused, reset or the attempt's timeout: the service is unavailable.
		return Result{}, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	defer func() {
		// Read what is left so the connection can be reused, then close.
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, responseLimit))
		_ = resp.Body.Close()
	}()
	if resp.StatusCode != http.StatusOK {
		return Result{}, statusError(resp)
	}

	var out embedResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, responseLimit)).Decode(&out); err != nil {
		if ctx.Err() != nil { // the timeout struck while reading the body
			return Result{}, fmt.Errorf("%w: %w", ErrUnavailable, err)
		}
		return Result{}, fmt.Errorf("%w: decode: %w", ErrBadResponse, err)
	}
	if err := checkResponse(out, n); err != nil {
		return Result{}, err
	}
	return Result{Model: out.Model, Vectors: out.Vectors}, nil
}

// retryable: 5xx, timeouts and connection errors, all wrapped as ErrUnavailable.
func retryable(err error) bool {
	return errors.Is(err, ErrUnavailable)
}

func checkInput(kind Kind, texts []string) error {
	if kind != KindPassage && kind != KindQuery {
		return fmt.Errorf("%w: kind must be passage or query", ErrInvalidInput)
	}
	if len(texts) == 0 || len(texts) > MaxTexts {
		return fmt.Errorf("%w: %d texts, want 1 to %d", ErrInvalidInput, len(texts), MaxTexts)
	}
	for i, t := range texts {
		// The service counts characters (code points), as Python does.
		if n := utf8.RuneCountInString(t); n == 0 || n > MaxTextChars {
			return fmt.Errorf("%w: text %d has %d characters, want 1 to %d", ErrInvalidInput, i, n, MaxTextChars)
		}
	}
	return nil
}

func checkResponse(out embedResponse, n int) error {
	if out.Model == "" {
		return fmt.Errorf("%w: no model name", ErrBadResponse)
	}
	if out.Dimensions != Dimensions {
		return fmt.Errorf("%w: %d dimensions, want %d", ErrBadResponse, out.Dimensions, Dimensions)
	}
	if len(out.Vectors) != n {
		return fmt.Errorf("%w: %d vectors for %d texts", ErrBadResponse, len(out.Vectors), n)
	}
	for i, v := range out.Vectors {
		if len(v) != Dimensions {
			return fmt.Errorf("%w: vector %d has %d numbers, want %d", ErrBadResponse, i, len(v), Dimensions)
		}
	}
	return nil
}

func statusError(resp *http.Response) error {
	var body errorBody
	_ = json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&body)
	return &StatusError{StatusCode: resp.StatusCode, Code: body.Code}
}
