// Package apiclient calls the API's internal compile operations (docs/openapi.yaml, tag
// internal, security compileToken; docs/compile.md). It is written by hand to keep this
// module free of dependencies; the backend's integration tests and make test-compile-e2e
// keep it and the contract together.
package apiclient

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
)

// Errors the API answers.
var (
	// ErrGone: the job is not running any more (finished, lost its lease or unknown).
	ErrGone = errors.New("apiclient: the job is not running")
	// ErrInputTooLarge: the project's files pass the input limit; the API failed the job.
	ErrInputTooLarge = errors.New("apiclient: the files are too large to compile")
	// ErrPDFTooLarge: the API refused the PDF as too large.
	ErrPDFTooLarge = errors.New("apiclient: the PDF is too large")
)

// Client calls the internal operations with the compile service's token.
type Client struct {
	base  string
	token string
	http  *http.Client
}

// New returns a client for the API's internal listener at base (http://127.0.0.1:8081).
func New(base, token string) *Client {
	return &Client{base: strings.TrimRight(base, "/") + "/api/v1/internal/compile", token: token,
		http: &http.Client{Transport: &http.Transport{MaxIdleConns: 10, IdleConnTimeout: 30 * time.Second}}}
}

// Job is a claimed compile (CompileJob).
type Job struct {
	ID           string `json:"id"`
	ProjectID    string `json:"projectId"`
	MainFilePath string `json:"mainFilePath"`
	Engine       string `json:"engine"`
}

// Result is how a run ended (CompileResult).
type Result struct {
	Outcome    string  `json:"outcome"` // completed, timed_out or failed
	Failure    *string `json:"failure,omitempty"`
	ExitCode   *int    `json:"exitCode,omitempty"`
	DurationMs int     `json:"durationMs"`
	Log        string  `json:"log"`
	BibtexLog  string  `json:"bibtexLog"`
}

func (c *Client) request(ctx context.Context, method, path, contentType string, body io.Reader, size int64) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if size >= 0 {
		req.ContentLength = size
	}
	res, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", method, path, err)
	}
	return res, nil
}

func statusError(res *http.Response) error {
	var e struct{ Code, Message string }
	b, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
	_ = json.Unmarshal(b, &e)
	return fmt.Errorf("apiclient: %s: %d %s", res.Request.URL.Path, res.StatusCode, e.Code)
}

// Claim takes a queued job, waiting up to 10 s on the API's side; nil when none came.
func (c *Client) Claim(ctx context.Context, workerID string) (*Job, error) {
	b, err := json.Marshal(map[string]string{"workerId": workerID})
	if err != nil {
		return nil, err
	}
	res, err := c.request(ctx, http.MethodPost, "/claim", "application/json", bytes.NewReader(b), int64(len(b)))
	if err != nil {
		return nil, err
	}
	defer func() { _ = res.Body.Close() }()
	switch res.StatusCode {
	case http.StatusNoContent:
		return nil, nil //nolint:nilnil // no job is the ordinary answer
	case http.StatusOK:
		var j Job
		if err := json.NewDecoder(io.LimitReader(res.Body, 64<<10)).Decode(&j); err != nil {
			return nil, fmt.Errorf("apiclient: claim: %w", err)
		}
		return &j, nil
	}
	return nil, statusError(res)
}

// Source opens the job's files as a tar stream. Close it.
func (c *Client) Source(ctx context.Context, jobID string) (io.ReadCloser, error) {
	res, err := c.request(ctx, http.MethodGet, "/jobs/"+jobID+"/source", "", nil, -1)
	if err != nil {
		return nil, err
	}
	switch res.StatusCode {
	case http.StatusOK:
		return res.Body, nil
	case http.StatusNotFound:
		_ = res.Body.Close()
		return nil, ErrGone
	case http.StatusConflict:
		_ = res.Body.Close()
		return nil, ErrInputTooLarge
	}
	defer func() { _ = res.Body.Close() }()
	return nil, statusError(res)
}

// StorePDF sends the PDF of size bytes.
func (c *Client) StorePDF(ctx context.Context, jobID string, pdf io.Reader, size int64) error {
	res, err := c.request(ctx, http.MethodPut, "/jobs/"+jobID+"/pdf", "application/pdf", pdf, size)
	if err != nil {
		return err
	}
	defer func() { _ = res.Body.Close() }()
	switch res.StatusCode {
	case http.StatusNoContent:
		return nil
	case http.StatusNotFound:
		return ErrGone
	case http.StatusRequestEntityTooLarge:
		return ErrPDFTooLarge
	}
	return statusError(res)
}

// Finish reports how the run ended.
func (c *Client) Finish(ctx context.Context, jobID string, r Result) error {
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	res, err := c.request(ctx, http.MethodPost, "/jobs/"+jobID+"/finish", "application/json", bytes.NewReader(b), int64(len(b)))
	if err != nil {
		return err
	}
	defer func() { _ = res.Body.Close() }()
	switch res.StatusCode {
	case http.StatusNoContent:
		return nil
	case http.StatusNotFound:
		return ErrGone
	}
	return statusError(res)
}
