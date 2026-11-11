package http

import (
	"bytes"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/api/gen"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/messaging/domain"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/httpx"
)

const attachID = "7f3a1c2e-8888-4e8b-b7c1-2d3e4f5a6b7c"

// multipartBody builds a body with one part named field holding content as filename.
func multipartBody(t *testing.T, field, filename, content string) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	if err := w.WriteField("note", "ignored"); err != nil {
		t.Fatal(err)
	}
	p, err := w.CreateFormFile(field, filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return &buf, w.FormDataContentType()
}

// upload posts a multipart body, capped at limit bytes when limit > 0, as cmd/api does.
func upload(t *testing.T, svc *fakeService, field, content string, limit int64) *httptest.ResponseRecorder {
	t.Helper()
	body, ct := multipartBody(t, field, "notes.pdf", content)
	req := httptest.NewRequestWithContext(httpx.WithUserID(t.Context(), caller), http.MethodPost,
		"/api/v1/conversations/"+convID+"/attachments", body)
	req.Header.Set("Content-Type", ct)
	mux := http.NewServeMux()
	gen.HandlerWithOptions(gen.NewStrictHandlerWithOptions(server{Handler: NewHandler(svc)}, nil, gen.StrictHTTPServerOptions{}),
		gen.StdHTTPServerOptions{BaseURL: "/api/v1", BaseRouter: mux})
	rec := httptest.NewRecorder()
	if limit > 0 {
		req.Body = http.MaxBytesReader(rec, req.Body, limit)
	}
	mux.ServeHTTP(rec, req)
	return rec
}

// FR-16.7: the upload answers the stored attachment, pending its scan.
func TestUploadAttachment(t *testing.T) {
	exp := time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)
	svc := &fakeService{upload: domain.Upload{
		Attachment: domain.Attachment{ID: attachID, Name: "notes.pdf", Size: 7, ContentType: "application/pdf", Status: domain.AttachmentPending},
		ExpiresAt:  exp,
	}}
	rec := upload(t, svc, "file", "%PDF-1.", 0)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	b := decode(t, rec)
	if b["id"] != attachID || b["name"] != "notes.pdf" || b["size"] != float64(7) || b["contentType"] != "application/pdf" ||
		b["status"] != "pending" || b["expiresAt"] != "2026-11-10T09:00:00Z" {
		t.Errorf("body = %v", b)
	}
	if want := []any{caller, convID, "notes.pdf", "%PDF-1."}; !equal(svc.args, want) {
		t.Errorf("args = %v, want %v", svc.args, want)
	}
}

func TestUploadAttachmentRefusals(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"too large", domain.ErrFileTooLarge, http.StatusRequestEntityTooLarge, CodeFileTooLarge},
		{"type", domain.ErrUnsupportedType, http.StatusUnsupportedMediaType, CodeUnsupportedType},
		{"pixels", domain.ErrImageTooLarge, http.StatusBadRequest, CodeImageTooLarge},
		{"bad image", domain.ErrInvalidImage, http.StatusBadRequest, CodeInvalidImage},
		{"not a member", domain.ErrNotFound, http.StatusNotFound, httpx.CodeNotFound},
		{"suspended", &domain.SuspendedError{}, http.StatusForbidden, CodeAccountSuspended},
		{"restricted", &domain.RestrictedError{Until: time.Now()}, http.StatusForbidden, CodeMessagingRestricted},
		{"blocked", domain.ErrCannotMessage, http.StatusForbidden, CodeCannotMessage},
		{"blocker", domain.ErrYouBlocked, http.StatusConflict, CodeYouBlocked},
		{"pending", domain.ErrRequestPending, http.StatusConflict, CodeRequestPending},
		{"closed", domain.ErrClosed, http.StatusConflict, CodeConversationClosed},
		{"limit", &domain.RateLimitedError{RetryAfter: 90 * time.Second}, http.StatusTooManyRequests, CodeRateLimited},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := upload(t, &fakeService{err: tc.err}, "file", "x", 0)
			if rec.Code != tc.status || decode(t, rec)["code"] != tc.code {
				t.Errorf("got %d %s, want %d %s", rec.Code, rec.Body, tc.status, tc.code)
			}
			if tc.status == http.StatusTooManyRequests && rec.Header().Get("Retry-After") != "90" {
				t.Errorf("Retry-After = %q", rec.Header().Get("Retry-After"))
			}
		})
	}
	// An unexpected error is a 500.
	if rec := upload(t, &fakeService{err: errors.New("boom")}, "file", "x", 0); rec.Code != http.StatusInternalServerError {
		t.Errorf("unexpected error: %d", rec.Code)
	}
}

// The body cap of cmd/api answers 413; a body without a part named file answers 400.
func TestUploadAttachmentBody(t *testing.T) {
	rec := upload(t, &fakeService{}, "file", strings.Repeat("x", 4096), 1024)
	if rec.Code != http.StatusRequestEntityTooLarge || decode(t, rec)["code"] != CodeFileTooLarge {
		t.Errorf("over the cap: %d %s", rec.Code, rec.Body)
	}
	rec = upload(t, &fakeService{}, "other", "x", 0)
	if rec.Code != http.StatusBadRequest || decode(t, rec)["code"] != httpx.CodeBadRequest {
		t.Errorf("no file part: %d %s", rec.Code, rec.Body)
	}
}

// FR-16.7: attachmentIds go to the usecase, the body may be left out, and a refused ID answers
// invalid_attachment on that field.
func TestSendWithAttachmentIDs(t *testing.T) {
	path := "/api/v1/conversations/" + convID + "/messages"
	m := message
	m.Body, m.HasAttachments = nil, true
	m.Attachments = []domain.Attachment{{ID: attachID, Name: "n.pdf", Size: 3, ContentType: "application/pdf", Status: domain.AttachmentClean}}
	svc := &fakeService{sent: domain.Sent{Message: m, State: domain.StateActive}}
	rec := send(t, svc, http.MethodPost, path, `{"clientId":"`+clientID+`","attachmentIds":["`+attachID+`"]}`, caller)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if want := []any{caller, convID, "<nil>", clientID, attachID}; !equal(svc.args, want) {
		t.Errorf("args = %v, want %v", svc.args, want)
	}
	files := decode(t, rec)["message"].(map[string]any)["attachments"].([]any)
	if len(files) != 1 || files[0].(map[string]any)["status"] != "clean" || files[0].(map[string]any)["name"] != "n.pdf" {
		t.Errorf("attachments = %v", files)
	}
	rec = send(t, &fakeService{err: domain.ErrInvalidAttachment}, http.MethodPost, path, `{"clientId":"`+clientID+`","attachmentIds":["`+attachID+`"]}`, caller)
	if b := decode(t, rec); rec.Code != http.StatusBadRequest || b["code"] != CodeInvalidAttachment || b["field"] != "attachmentIds" {
		t.Errorf("refused: %d %v", rec.Code, b)
	}
	if rec := send(t, &fakeService{}, http.MethodPost, path, `{"clientId":"`+clientID+`","attachmentIds":["nope"]}`, caller); rec.Code != http.StatusBadRequest {
		t.Errorf("not a UUID: %d", rec.Code)
	}
}

// Every message carries attachments, empty when it has none, and none when deleted.
func TestMessageBodyAttachments(t *testing.T) {
	b, err := MessageBody(message)
	if err != nil || b.Attachments == nil || len(b.Attachments) != 0 {
		t.Errorf("none: %v %v", b.Attachments, err)
	}
	m := message
	at := time.Now()
	m.DeletedAt, m.Body = &at, nil
	m.Attachments = []domain.Attachment{{ID: attachID}}
	if b, err := MessageBody(m); err != nil || len(b.Attachments) != 0 {
		t.Errorf("deleted: %v %v", b.Attachments, err)
	}
}

// FR-16.7, NFR-13: a clean file redirects, never cached or sniffed; the scan's other outcomes
// answer their codes.
func TestGetAttachment(t *testing.T) {
	path := "/api/v1/attachments/" + attachID
	svc := &fakeService{}
	rec := send(t, svc, http.MethodGet, path, "", caller)
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "https://bucket.example/k?X-Amz-Signature=abc" ||
		rec.Header().Get("Cache-Control") != "no-store" || rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("redirect: %d %v", rec.Code, rec.Header())
	}
	if want := []any{caller, attachID}; !equal(svc.args, want) {
		t.Errorf("args = %v", svc.args)
	}
	for _, tc := range []struct {
		err    error
		status int
		code   string
	}{
		{domain.ErrNotFound, http.StatusNotFound, httpx.CodeNotFound},
		{domain.ErrScanPending, http.StatusConflict, CodeScanPending},
		{domain.ErrScanFailed, http.StatusConflict, CodeScanFailed},
		{domain.ErrFileRemoved, http.StatusGone, CodeFileRemoved},
	} {
		rec := send(t, &fakeService{err: tc.err}, http.MethodGet, path, "", caller)
		if rec.Code != tc.status || decode(t, rec)["code"] != tc.code || rec.Header().Get("Location") != "" {
			t.Errorf("%v: %d %s", tc.err, rec.Code, rec.Body)
		}
		rec = send(t, &fakeService{err: tc.err}, http.MethodGet, "/api/v1/admin/message-reports/"+convID+"/attachments/"+attachID, "", caller)
		if rec.Code != tc.status || decode(t, rec)["code"] != tc.code {
			t.Errorf("admin %v: %d %s", tc.err, rec.Code, rec.Body)
		}
	}
	svc = &fakeService{}
	rec = send(t, svc, http.MethodGet, "/api/v1/admin/message-reports/"+convID+"/attachments/"+attachID, "", caller)
	if rec.Code != http.StatusFound || rec.Header().Get("Cache-Control") != "no-store" || rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("admin redirect: %d %v", rec.Code, rec.Header())
	}
	if want := []any{caller, convID, attachID}; !equal(svc.args, want) {
		t.Errorf("admin args = %v", svc.args)
	}
	if rec := send(t, &fakeService{}, http.MethodGet, path, "", ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("no session: %d", rec.Code)
	}
}
