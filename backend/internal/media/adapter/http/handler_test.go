package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/api/gen"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/media/domain"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/httpx"
)

const callerID = "00000000-0000-0000-0000-000000000001"

// fakeService records what it was given and returns err from every operation. Upload
// reads the whole file first, as the usecase does.
type fakeService struct {
	err     error
	userID  string
	purpose domain.Purpose
	data    []byte
	removed []domain.Purpose
	fileID  string
}

func (f *fakeService) Upload(_ context.Context, userID string, p domain.Purpose, r io.Reader) (string, error) {
	f.userID, f.purpose = userID, p
	data, err := io.ReadAll(r)
	f.data = data
	if err != nil {
		return "", err
	}
	if f.err != nil {
		return "", f.err
	}
	return "3f1c2b9e-8a41-4d7e-9c55-2f6a1b0e7d10", nil
}

func (f *fakeService) Remove(_ context.Context, userID string, p domain.Purpose) error {
	f.userID = userID
	f.removed = append(f.removed, p)
	return f.err
}

func (f *fakeService) FileURL(_ context.Context, callerID, fileID string) (*url.URL, error) {
	f.userID, f.fileID = callerID, fileID
	if f.err != nil {
		return nil, f.err
	}
	return url.Parse("http://localhost:9000/researchsphere/users/x/photo/y.jpg?X-Amz-Signature=abc")
}

type server struct {
	*Handler
	others
}

type others struct{ gen.StrictServerInterface }

// request is one call to the media operations. limit > 0 caps the body as cmd/api does.
type request struct {
	method, path string
	body         io.Reader
	contentType  string
	caller       string
	limit        int64
}

func send(t *testing.T, svc *fakeService, r request) *httptest.ResponseRecorder {
	t.Helper()
	ctx := t.Context()
	if r.caller != "" {
		ctx = httpx.WithUserID(ctx, r.caller)
	}
	if r.body == nil {
		r.body = http.NoBody
	}
	req := httptest.NewRequestWithContext(ctx, r.method, r.path, r.body)
	if r.contentType != "" {
		req.Header.Set("Content-Type", r.contentType)
	}
	mux := http.NewServeMux()
	gen.HandlerWithOptions(gen.NewStrictHandlerWithOptions(server{Handler: NewHandler(svc)}, nil, gen.StrictHTTPServerOptions{
		RequestErrorHandlerFunc: func(w http.ResponseWriter, _ *http.Request, _ error) {
			w.WriteHeader(http.StatusBadRequest)
		},
		ResponseErrorHandlerFunc: func(w http.ResponseWriter, _ *http.Request, _ error) {
			w.WriteHeader(http.StatusInternalServerError)
		},
	}), gen.StdHTTPServerOptions{BaseURL: "/api/v1", BaseRouter: mux})
	rec := httptest.NewRecorder()
	var h http.Handler = mux
	if r.limit > 0 {
		h = http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			req.Body = http.MaxBytesReader(w, req.Body, r.limit)
			mux.ServeHTTP(w, req)
		})
	}
	h.ServeHTTP(rec, req)
	return rec
}

// form returns a multipart body with the given parts, in order, and its content type.
func form(t *testing.T, parts ...[2]string) (io.Reader, string) {
	t.Helper()
	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	for _, p := range parts {
		fw, err := w.CreateFormFile(p[0], "picture.jpg")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(fw, p[1]); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return &b, w.FormDataContentType()
}

func code(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var e gen.Error
	if err := json.Unmarshal(rec.Body.Bytes(), &e); err != nil {
		t.Fatalf("body %q is not an error: %v", rec.Body, err)
	}
	return e.Code
}

func TestUploadStoresTheFilePart(t *testing.T) {
	for _, tt := range []struct {
		path    string
		purpose domain.Purpose
	}{{"/api/v1/profiles/me/photo", domain.PurposePhoto}, {"/api/v1/profiles/me/cover", domain.PurposeCover}} {
		t.Run(string(tt.purpose), func(t *testing.T) {
			svc := &fakeService{}
			body, ct := form(t, [2]string{"other", "ignored"}, [2]string{"file", "image bytes"})
			rec := send(t, svc, request{method: http.MethodPut, path: tt.path, body: body, contentType: ct, caller: callerID})
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
			}
			var got gen.ProfileImage
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if got.Url != "/api/v1/media/3f1c2b9e-8a41-4d7e-9c55-2f6a1b0e7d10" {
				t.Errorf("url = %q", got.Url)
			}
			if svc.userID != callerID || svc.purpose != tt.purpose || string(svc.data) != "image bytes" {
				t.Errorf("service got %s, %s, %q", svc.userID, svc.purpose, svc.data)
			}
		})
	}
}

func TestUploadErrors(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"too large", domain.ErrFileTooLarge, http.StatusRequestEntityTooLarge, CodeFileTooLarge},
		{"unsupported", domain.ErrUnsupportedType, http.StatusUnsupportedMediaType, CodeUnsupportedType},
		{"huge dimensions", domain.ErrImageTooLarge, http.StatusBadRequest, CodeImageTooLarge},
		{"unreadable", domain.ErrInvalidImage, http.StatusBadRequest, CodeInvalidImage},
		{"no profile", domain.ErrNoProfile, http.StatusNotFound, httpx.CodeNotFound},
		{"unexpected", errors.New("database down"), http.StatusInternalServerError, ""},
	}
	for _, tt := range tests {
		for _, path := range []string{"/api/v1/profiles/me/photo", "/api/v1/profiles/me/cover"} {
			t.Run(tt.name+" "+path, func(t *testing.T) {
				body, ct := form(t, [2]string{"file", "x"})
				rec := send(t, &fakeService{err: tt.err}, request{method: http.MethodPut, path: path, body: body, contentType: ct, caller: callerID})
				if rec.Code != tt.status {
					t.Fatalf("status = %d, want %d (body %s)", rec.Code, tt.status, rec.Body)
				}
				if tt.code != "" && code(t, rec) != tt.code {
					t.Errorf("code = %q, want %q", code(t, rec), tt.code)
				}
			})
		}
	}
}

func TestUploadRateLimited(t *testing.T) {
	for _, tt := range []struct {
		left time.Duration
		want string
	}{{42*time.Minute + 300*time.Millisecond, "2521"}, {200 * time.Millisecond, "1"}, {0, "1"}} {
		body, ct := form(t, [2]string{"file", "x"})
		svc := &fakeService{err: &domain.RateLimitedError{RetryAfter: tt.left}}
		rec := send(t, svc, request{method: http.MethodPut, path: "/api/v1/profiles/me/cover", body: body, contentType: ct, caller: callerID})
		if rec.Code != http.StatusTooManyRequests || code(t, rec) != CodeRateLimited {
			t.Fatalf("status = %d, body %s; want 429 rate_limited", rec.Code, rec.Body)
		}
		if got := rec.Header().Get("Retry-After"); got != tt.want {
			t.Errorf("left %s: Retry-After = %q, want %q", tt.left, got, tt.want)
		}
	}
}

func TestUploadBodyProblems(t *testing.T) {
	noFile, noFileCT := form(t, [2]string{"picture", "x"})
	big, bigCT := form(t, [2]string{"file", strings.Repeat("x", 2000)})
	tests := []struct {
		name   string
		body   io.Reader
		ct     string
		limit  int64
		status int
		code   string
	}{
		{"no part named file", noFile, noFileCT, 0, http.StatusBadRequest, httpx.CodeBadRequest},
		{"body over the cap", big, bigCT, 1000, http.StatusRequestEntityTooLarge, CodeFileTooLarge},
		{"cut-off multipart", strings.NewReader("--b\r\nContent-Disposition: form-data; name=\"file\"\r\n\r\nabc"), "multipart/form-data; boundary=b", 0, http.StatusBadRequest, httpx.CodeBadRequest},
		{"json body", strings.NewReader(`{"file":"x"}`), "application/json", 0, http.StatusBadRequest, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := send(t, &fakeService{}, request{method: http.MethodPut, path: "/api/v1/profiles/me/photo",
				body: tt.body, contentType: tt.ct, caller: callerID, limit: tt.limit})
			if rec.Code != tt.status {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, tt.status, rec.Body)
			}
			if tt.code != "" && code(t, rec) != tt.code {
				t.Errorf("code = %q, want %q", code(t, rec), tt.code)
			}
		})
	}
}

func TestRemove(t *testing.T) {
	svc := &fakeService{}
	for _, path := range []string{"/api/v1/profiles/me/photo", "/api/v1/profiles/me/cover"} {
		if rec := send(t, svc, request{method: http.MethodDelete, path: path, caller: callerID}); rec.Code != http.StatusNoContent {
			t.Errorf("DELETE %s: status = %d, want 204", path, rec.Code)
		}
	}
	if len(svc.removed) != 2 || svc.removed[0] != domain.PurposePhoto || svc.removed[1] != domain.PurposeCover || svc.userID != callerID {
		t.Errorf("removed %v for %s", svc.removed, svc.userID)
	}
}

func TestGetMedia(t *testing.T) {
	svc := &fakeService{}
	rec := send(t, svc, request{method: http.MethodGet, path: "/api/v1/media/abc", caller: callerID})
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302 (body %s)", rec.Code, rec.Body)
	}
	if loc := rec.Header().Get("Location"); !strings.Contains(loc, "X-Amz-Signature") {
		t.Errorf("Location = %q, want the presigned URL", loc)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "private, max-age=300" {
		t.Errorf("Cache-Control = %q", cc)
	}
	if svc.fileID != "abc" || svc.userID != callerID {
		t.Errorf("service got file %q for %q", svc.fileID, svc.userID)
	}

	rec = send(t, &fakeService{err: domain.ErrNotFound}, request{method: http.MethodGet, path: "/api/v1/media/abc", caller: callerID})
	if rec.Code != http.StatusNotFound || code(t, rec) != httpx.CodeNotFound {
		t.Errorf("unknown file: status = %d, body %s; want 404 not_found", rec.Code, rec.Body)
	}
	if rec.Header().Get("Cache-Control") != "" {
		t.Error("a 404 must not be cached as a redirect")
	}
}

func TestWithoutUser(t *testing.T) {
	body, ct := form(t, [2]string{"file", "x"})
	for _, r := range []request{
		{method: http.MethodPut, path: "/api/v1/profiles/me/photo", body: body, contentType: ct},
		{method: http.MethodDelete, path: "/api/v1/profiles/me/cover"},
		{method: http.MethodGet, path: "/api/v1/media/abc"},
	} {
		if rec := send(t, &fakeService{}, r); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s: status = %d, want 401", r.method, r.path, rec.Code)
		}
	}
}
