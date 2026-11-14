// Package aiclient calls the internal AI service (ai/, AI-1) for text embeddings.
//
// The service is optional (NFR-10.2): callers treat an error as "AI unavailable" and carry
// on. Each Embed or EmbedDocument call tries at most twice: it retries once after a 5xx answer, a timeout
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

// Limits of one EmbedDocument call, the same as the service's.
const (
	MaxDocumentChars  = 100_000
	MaxDocumentChunks = 8
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

// DocumentResult is what EmbedDocument returns: one unit-length vector for the whole text,
// the model that made it, how many chunks of at most 512 tokens were embedded and averaged,
// and whether text was left over after the last chunk.
type DocumentResult struct {
	Model     string
	Vector    []float32
	Chunks    int
	Truncated bool
}

// Embedder is what callers depend on; Client and Fake implement it.
type Embedder interface {
	Embed(ctx context.Context, kind Kind, texts []string) (Result, error)
	// EmbedDocument embeds a long stored text as passages: the service splits it by real
	// tokens into at most maxChunks chunks, the start of the text in the first, and averages.
	EmbedDocument(ctx context.Context, text string, maxChunks int) (DocumentResult, error)
	Ping(ctx context.Context) error
	// Model returns the "name@revision" of the loaded model, once the service is ready.
	Model(ctx context.Context) (string, error)
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

type embedDocumentRequest struct {
	Text      string `json:"text"`
	MaxChunks int    `json:"maxChunks"`
}

type embedDocumentResponse struct {
	Model      string    `json:"model"`
	Dimensions int       `json:"dimensions"`
	Vector     []float32 `json:"vector"`
	Chunks     int       `json:"chunks"`
	Truncated  bool      `json:"truncated"`
}

type readyResponse struct {
	Model string `json:"model"`
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

	var out embedResponse
	if err := c.postWithRetry(ctx, "/embed", body, &out); err != nil {
		return Result{}, err
	}
	if err := checkResponse(out, len(texts)); err != nil {
		return Result{}, err
	}
	return Result{Model: out.Model, Vectors: out.Vectors}, nil
}

// EmbedDocument returns one vector for text (POST /embed-document). The text must be 1 to
// MaxDocumentChars characters with at least one word, maxChunks 1 to MaxDocumentChunks.
func (c *Client) EmbedDocument(ctx context.Context, text string, maxChunks int) (DocumentResult, error) {
	if c.token == "" {
		return DocumentResult{}, ErrNotConfigured
	}
	if err := checkDocument(text, maxChunks); err != nil {
		return DocumentResult{}, err
	}
	body, err := json.Marshal(embedDocumentRequest{Text: text, MaxChunks: maxChunks})
	if err != nil {
		return DocumentResult{}, fmt.Errorf("encode embed-document request: %w", err)
	}
	var out embedDocumentResponse
	if err := c.postWithRetry(ctx, "/embed-document", body, &out); err != nil {
		return DocumentResult{}, err
	}
	if err := checkDocumentResponse(out, maxChunks); err != nil {
		return DocumentResult{}, err
	}
	return DocumentResult{Model: out.Model, Vector: out.Vector, Chunks: out.Chunks, Truncated: out.Truncated}, nil
}

// postWithRetry posts body to path and decodes a 200 answer into out, retrying once as the
// package comment says.
func (c *Client) postWithRetry(ctx context.Context, path string, body []byte, out any) error {
	for attempt := 1; ; attempt++ {
		err := c.postOnce(ctx, path, body, out)
		if err == nil || attempt == 2 || !retryable(err) || ctx.Err() != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return errors.Join(err, ctx.Err())
		case <-time.After(c.retryDelay):
		}
	}
}

// Ping reports whether the service has loaded its model (GET /ready). It is one attempt
// under the caller's context: the health check has its own timeout.
func (c *Client) Ping(ctx context.Context) error {
	_, err := c.ready(ctx, false)
	return err
}

// Model returns the model the service has loaded (GET /ready), in one attempt under the
// caller's context.
func (c *Client) Model(ctx context.Context) (string, error) {
	return c.ready(ctx, true)
}

// ready calls GET /ready and, when wantModel, reads the model name from the answer.
func (c *Client) ready(ctx context.Context, wantModel bool) (string, error) {
	if c.token == "" {
		return "", ErrNotConfigured
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/ready", nil)
	if err != nil {
		return "", fmt.Errorf("build ready request: %w", err)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	defer func() {
		// Read what is left so the connection can be reused, then close.
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, responseLimit))
		_ = resp.Body.Close()
	}()
	if resp.StatusCode != http.StatusOK {
		return "", statusError(resp)
	}
	if !wantModel {
		return "", nil
	}
	var out readyResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&out); err != nil {
		return "", fmt.Errorf("%w: decode: %w", ErrBadResponse, err)
	}
	if out.Model == "" {
		return "", fmt.Errorf("%w: no model name", ErrBadResponse)
	}
	return out.Model, nil
}

func (c *Client) postOnce(ctx context.Context, path string, body []byte, out any) error {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build %s request: %w", path, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.http.Do(req)
	if err != nil {
		// Connection refused, reset or the attempt's timeout: the service is unavailable.
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

	if err := json.NewDecoder(io.LimitReader(resp.Body, responseLimit)).Decode(out); err != nil {
		if ctx.Err() != nil { // the timeout struck while reading the body
			return fmt.Errorf("%w: %w", ErrUnavailable, err)
		}
		return fmt.Errorf("%w: decode: %w", ErrBadResponse, err)
	}
	return nil
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

func checkDocument(text string, maxChunks int) error {
	if maxChunks < 1 || maxChunks > MaxDocumentChunks {
		return fmt.Errorf("%w: maxChunks %d, want 1 to %d", ErrInvalidInput, maxChunks, MaxDocumentChunks)
	}
	if strings.TrimSpace(text) == "" {
		return fmt.Errorf("%w: the text is empty", ErrInvalidInput)
	}
	if n := utf8.RuneCountInString(text); n > MaxDocumentChars {
		return fmt.Errorf("%w: the text has %d characters, want at most %d", ErrInvalidInput, n, MaxDocumentChars)
	}
	return nil
}

func checkDocumentResponse(out embedDocumentResponse, maxChunks int) error {
	if out.Model == "" {
		return fmt.Errorf("%w: no model name", ErrBadResponse)
	}
	if out.Dimensions != Dimensions || len(out.Vector) != Dimensions {
		return fmt.Errorf("%w: %d numbers, want %d", ErrBadResponse, len(out.Vector), Dimensions)
	}
	if out.Chunks < 1 || out.Chunks > maxChunks {
		return fmt.Errorf("%w: %d chunks, want 1 to %d", ErrBadResponse, out.Chunks, maxChunks)
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
