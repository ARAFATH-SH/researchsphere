package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	compiledomain "github.com/ARAFATH-SH/researchsphere/backend/internal/compile/domain"
	compileusecase "github.com/ARAFATH-SH/researchsphere/backend/internal/compile/usecase"
)

// okCompiles answers every compile operation with a fixed job, so the router tests reach the
// handlers.
type okCompiles struct{}

const okCompileID = "0192f0a0-0000-7000-8000-0000000c0001"

func okJob() compiledomain.Job {
	return compiledomain.Job{ID: okCompileID, ProjectID: okProjectID, Status: compiledomain.StatusQueued, Engine: "pdflatex",
		MainFilePath: "main.tex", QueuedAt: time.Date(2026, 11, 23, 10, 0, 0, 0, time.UTC), LiveTextConfirmed: true}
}

func (okCompiles) Start(_ context.Context, _, projectID string) (compiledomain.Job, error) {
	if projectID != okProjectID {
		return compiledomain.Job{}, compiledomain.ErrNotFound
	}
	return okJob(), nil
}

func (okCompiles) Latest(_ context.Context, _, projectID string) (compiledomain.Job, error) {
	if projectID != okProjectID {
		return compiledomain.Job{}, compiledomain.ErrNotFound
	}
	return okJob(), nil
}

func (okCompiles) Get(_ context.Context, _, _, jobID string) (compiledomain.Job, error) {
	if jobID != okCompileID {
		return compiledomain.Job{}, compiledomain.ErrNotFound
	}
	return okJob(), nil
}

func (okCompiles) Log(context.Context, string, string, string) (string, error) {
	return "This is pdfTeX", nil
}

func (okCompiles) OpenPDF(context.Context, string, string) (compiledomain.PDF, io.ReadCloser, error) {
	return compiledomain.PDF{CompileID: okCompileID, Size: 9, Title: "Graph learning"}, io.NopCloser(strings.NewReader("%PDF-1.5\n")), nil
}

func (okCompiles) Claim(context.Context, string) (compiledomain.Claimed, bool, error) {
	return compiledomain.Claimed{}, false, nil
}

func (okCompiles) PrepareSource(context.Context, string) (compileusecase.Prepared, error) {
	return compileusecase.Prepared{}, compiledomain.ErrNotFound
}

func (okCompiles) WriteSource(context.Context, compileusecase.Prepared, io.Writer) error { return nil }

func (okCompiles) StorePDF(context.Context, string, io.Reader) error {
	return compiledomain.ErrNotFound
}

func (okCompiles) Finish(context.Context, string, compiledomain.Result) error {
	return compiledomain.ErrNotFound
}

// The compile routes need a session with a verified email and the platform action
// platformGuards names; the PDF carries the headers that keep its scripts sandboxed.
func TestCompileRoutes(t *testing.T) {
	router := testRouter()
	const p = "/api/v1/projects/" + okProjectID
	tests := []struct {
		name, method, path string
		cookie             bool
		want               int
		wantBody           string
	}{
		{"start", http.MethodPost, p + "/compiles", true, http.StatusAccepted, `"status":"queued"`},
		{"start without a session", http.MethodPost, p + "/compiles", false, http.StatusUnauthorized, ""},
		{"latest", http.MethodGet, p + "/compiles/latest", true, http.StatusOK, `"mainFilePath":"main.tex"`},
		{"one", http.MethodGet, p + "/compiles/" + okCompileID, true, http.StatusOK, `"engine":"pdflatex"`},
		{"log", http.MethodGet, p + "/compiles/" + okCompileID + "/log", true, http.StatusOK, "This is pdfTeX"},
		{"pdf", http.MethodGet, p + "/pdf?disposition=inline", true, http.StatusOK, "%PDF-"},
		{"pdf, bad disposition", http.MethodGet, p + "/pdf?disposition=open", true, http.StatusBadRequest, ""},
		{"archive", http.MethodGet, p + "/archive", true, http.StatusOK, "PK-"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			role := ""
			if tt.cookie {
				role = "researcher"
			}
			rec := sendAs(t, router, tt.method, tt.path, nil, "", role)
			if rec.Code != tt.want || !strings.Contains(rec.Body.String(), tt.wantBody) {
				t.Fatalf("status = %d, body %s; want %d with %q", rec.Code, rec.Body, tt.want, tt.wantBody)
			}
			if tt.name == "pdf" {
				h := rec.Header()
				if h.Get("Content-Type") != "application/pdf" || h.Get("X-Content-Type-Options") != "nosniff" ||
					!strings.HasPrefix(h.Get("Content-Security-Policy"), "sandbox") ||
					!strings.HasPrefix(h.Get("Content-Disposition"), "inline") || h.Get("X-Compile-Id") != okCompileID {
					t.Errorf("PDF headers: %v", h)
				}
			}
			if tt.name == "archive" && !strings.HasPrefix(rec.Header().Get("Content-Disposition"), "attachment") {
				t.Errorf("archive headers: %v", rec.Header())
			}
		})
	}
}

// The compile worker's operations need its own token: the sync server's token is refused, and
// the public port never serves them (slice 23).
func TestCompileWorkerRoutesNeedTheirToken(t *testing.T) {
	full := testRouter()
	internal, public := internalOnly(full), publicOnly(full)
	claim := func(h http.Handler, token string) int {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/internal/compile/claim", strings.NewReader(`{"workerId":"w1"}`))
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}
	if got := claim(internal, testCompileToken); got != http.StatusNoContent {
		t.Errorf("compile token: status %d, want 204", got)
	}
	if got := claim(internal, testSyncToken); got != http.StatusUnauthorized {
		t.Errorf("sync token: status %d, want 401", got)
	}
	if got := claim(internal, ""); got != http.StatusUnauthorized {
		t.Errorf("no token: status %d, want 401", got)
	}
	if got := claim(public, testCompileToken); got != http.StatusNotFound {
		t.Errorf("public port: status %d, want 404", got)
	}
}
