package main

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	mediadomain "github.com/ARAFATH-SH/researchsphere/backend/internal/media/domain"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/accesstoken"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/authz"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/httpserver"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/httpx"
)

const okFileID = "3f1c2b9e-8a41-4d7e-9c55-2f6a1b0e7d10"

// okMedia is a media service that reads the whole upload and accepts it, and serves every
// file. The size limits are checked by the router's body cap in these tests.
type okMedia struct{}

func (okMedia) Upload(_ context.Context, _ string, _ mediadomain.Purpose, r io.Reader) (string, error) {
	if _, err := io.Copy(io.Discard, r); err != nil {
		return "", err
	}
	return okFileID, nil
}

func (okMedia) Remove(context.Context, string, mediadomain.Purpose) error { return nil }

func (okMedia) FileURL(context.Context, string, string) (*url.URL, error) {
	return url.Parse("http://localhost:9000/researchsphere/x.jpg?X-Amz-Signature=abc")
}

// imageForm returns a multipart body with a file part of n bytes, and its content type.
func imageForm(t *testing.T, n int) (*bytes.Buffer, string) {
	t.Helper()
	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	fw, err := w.CreateFormFile("file", "photo.jpg")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write(bytes.Repeat([]byte{0xFF}, n)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return &b, w.FormDataContentType()
}

func sendAs(t *testing.T, router http.Handler, method, path string, body io.Reader, contentType, role string) *httptest.ResponseRecorder {
	t.Helper()
	if body == nil {
		body = http.NoBody
	}
	req := httptest.NewRequestWithContext(t.Context(), method, path, body)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if role != "" {
		token, _, err := accesstoken.New([]byte(testSecret), accessTTL).Sign(okUserID, role, true, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Cookie", httpx.AccessCookie+"="+token)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

// The media routes go through authz (FR-3.2, SRS 2.3) like the profile routes.
func TestMediaRoutesThroughRouter(t *testing.T) {
	router := testRouter()
	type route struct {
		method, path string
		upload       bool
		status       int
	}
	routes := []route{
		{http.MethodPut, "/api/v1/profiles/me/photo", true, http.StatusOK},
		{http.MethodDelete, "/api/v1/profiles/me/photo", false, http.StatusNoContent},
		{http.MethodPut, "/api/v1/profiles/me/cover", true, http.StatusOK},
		{http.MethodDelete, "/api/v1/profiles/me/cover", false, http.StatusNoContent},
		{http.MethodGet, "/api/v1/media/" + okFileID, false, http.StatusFound},
	}
	send := func(t *testing.T, r route, role string) *httptest.ResponseRecorder {
		t.Helper()
		var (
			body io.Reader
			ct   string
		)
		if r.upload {
			body, ct = imageForm(t, 100)
		}
		return sendAs(t, router, r.method, r.path, body, ct, role)
	}
	for _, r := range routes {
		name := r.method + " " + r.path
		for _, role := range []authz.Role{authz.RoleResearcher, authz.RoleAdmin} {
			t.Run(name+"/"+string(role), func(t *testing.T) {
				if rec := send(t, r, string(role)); rec.Code != r.status {
					t.Errorf("status = %d, want %d; body %s", rec.Code, r.status, rec.Body)
				}
			})
		}
		t.Run(name+"/unknown role", func(t *testing.T) {
			rec := send(t, r, "visitor")
			if rec.Code != http.StatusForbidden || rec.Body.String() != forbiddenBody {
				t.Errorf("got %d %q, want 403 with exactly %q", rec.Code, rec.Body, forbiddenBody)
			}
		})
		t.Run(name+"/not logged in", func(t *testing.T) {
			checkUnauthenticated(t, "no token", send(t, r, ""))
		})
	}
}

func TestMediaRoutesAreGuarded(t *testing.T) {
	want := map[string]authz.Action{
		"PUT /api/v1/profiles/me/photo":    authz.ActAsMember,
		"DELETE /api/v1/profiles/me/photo": authz.ActAsMember,
		"PUT /api/v1/profiles/me/cover":    authz.ActAsMember,
		"DELETE /api/v1/profiles/me/cover": authz.ActAsMember,
		"GET /api/v1/media/{fileId}":       authz.ViewContent,
	}
	for route, action := range want {
		if got := platformGuards[route]; got != action {
			t.Errorf("platformGuards[%q] = %q, want %q", route, got, action)
		}
	}
	for _, r := range httpserver.Routes() {
		_, path, _ := strings.Cut(r, " ")
		if strings.HasPrefix(path, httpserver.BaseURL+"/media") {
			if _, ok := want[r]; !ok {
				t.Errorf("route %q has no expected action in this test; add it", r)
			}
		}
	}
}

// The router caps an upload's body while it is read (FR-4.6): 5 MB for a photo and 8 MB
// for a cover, plus room for the multipart framing.
func TestUploadBodyIsCapped(t *testing.T) {
	router := testRouter()
	tests := []struct {
		path   string
		n      int
		status int
	}{
		{"/api/v1/profiles/me/photo", 5 << 20, http.StatusOK},
		{"/api/v1/profiles/me/photo", 5<<20 + multipartOverhead, http.StatusRequestEntityTooLarge},
		{"/api/v1/profiles/me/cover", 8 << 20, http.StatusOK},
		{"/api/v1/profiles/me/cover", 8<<20 + multipartOverhead, http.StatusRequestEntityTooLarge},
	}
	for _, tt := range tests {
		body, ct := imageForm(t, tt.n)
		rec := sendAs(t, router, http.MethodPut, tt.path, body, ct, string(authz.RoleResearcher))
		if rec.Code != tt.status {
			t.Errorf("%s with %d bytes: status = %d, want %d (body %s)", tt.path, tt.n, rec.Code, tt.status, rec.Body)
		}
		if tt.status == http.StatusRequestEntityTooLarge && !strings.Contains(rec.Body.String(), `"file_too_large"`) {
			t.Errorf("%s: body = %s, want file_too_large", tt.path, rec.Body)
		}
	}
	for route := range bodyLimits {
		if _, ok := platformGuards[route]; !ok {
			t.Errorf("bodyLimits names %q, which has no platform guard", route)
		}
	}
}
