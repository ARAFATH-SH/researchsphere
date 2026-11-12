//go:build integration

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	mediapg "github.com/ARAFATH-SH/researchsphere/backend/internal/media/adapter/postgres"
	mediadomain "github.com/ARAFATH-SH/researchsphere/backend/internal/media/domain"
	mediausecase "github.com/ARAFATH-SH/researchsphere/backend/internal/media/usecase"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/accesstoken"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/httpx"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/postgres"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/storage"
)

// samplePDF is a small file that passes the type check as a PDF.
const samplePDF = "%PDF-1.7\n1 0 obj << /Type /Catalog >> endobj\ntrailer << /Root 1 0 R >>\n%%EOF\n"

// eicarPDF holds the EICAR test string after a PDF header; the fake scanner reports it, as a
// real one would.
var eicarPDF = "%PDF-1.7\n" + `X5O!P%@AP[4\PZX54(P^)7CC)7}$` + `EICAR-STANDARD-ANTIVIRUS-TEST-FILE!$H+H*`

// fakeClamd is a VirusScanner that reports the EICAR string and counts each file it scans.
// delay makes a scan take a while, so two workers would overlap if they took the same file.
type fakeClamd struct {
	mu    sync.Mutex
	seen  map[string]int
	delay time.Duration
}

func (f *fakeClamd) Ping(context.Context) error { return nil }

func (f *fakeClamd) Scan(_ context.Context, r io.Reader) (mediadomain.ScanVerdict, error) {
	b, err := io.ReadAll(r)
	if err != nil {
		return mediadomain.ScanVerdict{}, err
	}
	time.Sleep(f.delay)
	f.mu.Lock()
	if f.seen == nil {
		f.seen = map[string]int{}
	}
	f.seen[string(b)]++
	f.mu.Unlock()
	if strings.Contains(string(b), "EICAR-STANDARD-ANTIVIRUS-TEST-FILE") {
		return mediadomain.ScanVerdict{Infected: true, Signature: "Eicar-Test-Signature"}, nil
	}
	return mediadomain.ScanVerdict{}, nil
}

// worker builds a scan worker over the test database and bucket with scanner, as main does.
func (e mediaEnv) worker(scanner mediadomain.VirusScanner) *mediausecase.ScanWorker {
	logger := slog.New(slog.DiscardHandler)
	return mediausecase.NewScanWorker(mediapg.NewStore(e.pool), postgres.NewTransactor(e.pool), scanObjects{files: e.files}, e.files,
		scanner, scanListener{messaging: e.messaging, notifications: e.notifier, logger: logger}, logger)
}

// scanAll runs the worker until nothing is left to scan.
func (e mediaEnv) scanAll(t *testing.T, scanner mediadomain.VirusScanner) {
	t.Helper()
	w := e.worker(scanner)
	for {
		n, err := w.RunOnce(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if n == 0 {
			return
		}
	}
}

// uploadFile posts content as a file named name to the conversation, and deletes its object
// from the bucket when the test ends: emptying the database does not empty the bucket.
func (e mediaEnv) uploadFile(t *testing.T, userID, conversationID, name string, content []byte) (int, map[string]any) {
	t.Helper()
	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	fw, err := w.CreateFormFile("file", name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	rec := e.send(t, userID, http.MethodPost, "/api/v1/conversations/"+conversationID+"/attachments", &b, w.FormDataContentType())
	if rec.Body.Len() == 0 {
		return rec.Code, nil
	}
	body := decodeBody(t, rec.Body.Bytes())
	if id, ok := body["id"].(string); ok && rec.Code == http.StatusCreated {
		key := "conversations/" + conversationID + "/attachments/" + id
		t.Cleanup(func() { _ = e.files.Delete(context.Background(), key) })
	}
	return rec.Code, body
}

func (e mediaEnv) mustUpload(t *testing.T, userID, conversationID, name, content string) string {
	t.Helper()
	code, body := e.uploadFile(t, userID, conversationID, name, []byte(content))
	if code != http.StatusCreated {
		t.Fatalf("upload: %d %v", code, body)
	}
	return body["id"].(string)
}

// sendFiles sends a message with the attachments and no text.
func (e mediaEnv) sendFiles(t *testing.T, from, conversationID string, ids ...string) (int, map[string]any) {
	t.Helper()
	return e.call(t, from, http.MethodPost, "/api/v1/conversations/"+conversationID+"/messages",
		fmt.Sprintf(`{"clientId":%q,"attachmentIds":%s}`, newClientID(), idList(ids...)))
}

// download asks for the attachment; for a 302 it fetches the presigned URL without credentials,
// as a browser does, and returns that answer.
func (e mediaEnv) download(t *testing.T, userID, path string) (int, map[string]any, http.Header, []byte) {
	t.Helper()
	rec := e.send(t, userID, http.MethodGet, path, nil, "")
	if rec.Code != http.StatusFound {
		var body map[string]any
		if rec.Body.Len() > 0 {
			body = decodeBody(t, rec.Body.Bytes())
		}
		return rec.Code, body, nil, nil
	}
	if rec.Header().Get("Cache-Control") != "no-store" || rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("redirect headers = %v", rec.Header())
	}
	header, content := e.follow(t, rec.Header().Get("Location"))
	return rec.Code, nil, header, content
}

// asAdminRaw is asAdmin, answering the recorder (for a redirect's headers).
func (e mediaEnv) asAdminRaw(t *testing.T, adminID, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil)
	token, _, err := accesstoken.New([]byte(testSecret), accessTTL).Sign(adminID, "admin", true, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Cookie", httpx.AccessCookie+"="+token)
	rec := httptest.NewRecorder()
	e.router.ServeHTTP(rec, req)
	return rec
}

// multipartFile is a multipart body with content as the part named file.
func multipartFile(t *testing.T, name, content string) (io.Reader, string) {
	t.Helper()
	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	fw, err := w.CreateFormFile("file", name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return &b, w.FormDataContentType()
}

func (e mediaEnv) follow(t *testing.T, location string) (http.Header, []byte) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, location, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("presigned GET: %d, %v", resp.StatusCode, err)
	}
	return resp.Header, b
}

func attachmentPath(id string) string { return "/api/v1/attachments/" + id }

// isAttachment matches an attachment.updated event for the file and status.
func isAttachment(id, status string) func(map[string]any) bool {
	return func(d map[string]any) bool { return d["attachmentId"] == id && d["status"] == status }
}

// FR-16.7, NFR-13: upload, send without text, nobody downloads before the scan, the scan
// tells the members, then a 5-minute presigned download that is always a file to save.
func TestAttachmentFromUploadToDownload(t *testing.T) {
	e := newMediaEnv(t)
	srv := serve(t, e.router)
	ada, bob := e.newProfileUser(t), e.newProfileUser(t)
	e.link(t, ada, bob)
	id, _ := e.startChat(t, ada, bob, "hello")

	code, up := e.uploadFile(t, ada, id, `../Q3 "draft"\report.pdf`, []byte(samplePDF))
	if code != http.StatusCreated || up["status"] != "pending" || up["name"] != "report.pdf" ||
		up["contentType"] != "application/pdf" || up["size"] != float64(len(samplePDF)) {
		t.Fatalf("upload: %d %v", code, up)
	}
	fileID := up["id"].(string)
	expires, err := time.Parse(time.RFC3339, up["expiresAt"].(string))
	if err != nil || time.Until(expires) < 23*time.Hour || time.Until(expires) > 25*time.Hour {
		t.Errorf("expiresAt = %v (%v)", up["expiresAt"], err)
	}
	// Nobody, the uploader included, downloads before the scan.
	if code, body, _, _ := e.download(t, ada, attachmentPath(fileID)); code != http.StatusConflict || body["code"] != "scan_pending" {
		t.Errorf("uploader before the scan: %d %v", code, body)
	}

	code, sent := e.sendFiles(t, ada, id, fileID)
	if code != http.StatusCreated {
		t.Fatalf("send: %d %v", code, sent)
	}
	msg := sent["message"].(map[string]any)
	files := msg["attachments"].([]any)
	if msg["body"] != nil || len(files) != 1 || files[0].(map[string]any)["status"] != "pending" {
		t.Fatalf("message = %v", msg)
	}
	if code, body, _, _ := e.download(t, bob, attachmentPath(fileID)); code != http.StatusConflict || body["code"] != "scan_pending" {
		t.Errorf("member before the scan: %d %v", code, body)
	}

	bobWS, adaWS := dial(t, srv, bob), dial(t, srv, ada)
	e.scanAll(t, &fakeClamd{})
	for _, ws := range []*wsClient{bobWS, adaWS} {
		ev := ws.expect(t, "attachment.updated", isAttachment(fileID, "clean"))
		if ev.Data["conversationId"] != id || ev.Data["messageId"] != msg["id"] {
			t.Errorf("event = %v", ev.Data)
		}
	}

	code, _, header, content := e.download(t, bob, attachmentPath(fileID))
	if code != http.StatusFound || string(content) != samplePDF {
		t.Fatalf("download: %d, %d bytes", code, len(content))
	}
	if cd := header.Get("Content-Disposition"); !strings.HasPrefix(cd, `attachment; filename="report.pdf"`) {
		t.Errorf("Content-Disposition = %q", cd)
	}
	if ct := header.Get("Content-Type"); ct != "application/pdf" {
		t.Errorf("Content-Type = %q", ct)
	}
	thread := e.thread(t, bob, id)
	if got := thread[0]["attachments"].([]any)[0].(map[string]any); got["status"] != "clean" || got["id"] != fileID {
		t.Errorf("thread = %v", thread[0])
	}
	// A stranger gets the answer of an unknown file (NFR-13).
	eve := e.newProfileUser(t)
	if code, _, _, _ := e.download(t, eve, attachmentPath(fileID)); code != http.StatusNotFound {
		t.Errorf("stranger: %d", code)
	}
	// An attachment is used once.
	if code, body := e.sendFiles(t, ada, id, fileID); code != http.StatusBadRequest || body["code"] != "invalid_attachment" || body["field"] != "attachmentIds" {
		t.Errorf("second use: %d %v", code, body)
	}
	// Deleting the message hides the file at once; the object stays for retention.
	if code, _ := e.deleteMessage(t, ada, id, msg["id"].(string)); code != http.StatusNoContent {
		t.Fatalf("delete: %d", code)
	}
	if code, _, _, _ := e.download(t, bob, attachmentPath(fileID)); code != http.StatusNotFound {
		t.Errorf("deleted message: %d", code)
	}
	if _, err := e.files.Stat(t.Context(), "conversations/"+id+"/attachments/"+fileID); err != nil {
		t.Errorf("object gone after the message was deleted: %v", err)
	}
}

// FR-16.7: malware is never served. The object is deleted at once, the row kept as infected, the
// uploader notified (a system notification) and every member told.
func TestInfectedAttachment(t *testing.T) {
	e := newMediaEnv(t)
	srv := serve(t, e.router)
	ada, bob := e.newProfileUser(t), e.newProfileUser(t)
	e.link(t, ada, bob)
	id, _ := e.startChat(t, ada, bob, "hello")
	fileID := e.mustUpload(t, ada, id, "invoice.pdf", eicarPDF)
	if code, sent := e.sendFiles(t, ada, id, fileID); code != http.StatusCreated {
		t.Fatalf("send: %d %v", code, sent)
	}
	key := "conversations/" + id + "/attachments/" + fileID
	if _, err := e.files.Stat(t.Context(), key); err != nil {
		t.Fatalf("object not stored: %v", err)
	}
	bobWS := dial(t, srv, bob)
	e.scanAll(t, &fakeClamd{})
	bobWS.expect(t, "attachment.updated", isAttachment(fileID, "infected"))

	if _, err := e.files.Stat(t.Context(), key); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("infected object still stored: %v", err)
	}
	if !e.queued(t, key) {
		t.Error("infected object not queued as a backup")
	}
	var status string
	if err := e.pool.QueryRow(t.Context(), "SELECT scan_status FROM stored_files WHERE id = $1", fileID).Scan(&status); err != nil || status != "infected" {
		t.Errorf("row = %s, %v", status, err)
	}
	for _, u := range []string{ada, bob} {
		if code, body, _, _ := e.download(t, u, attachmentPath(fileID)); code != http.StatusGone || body["code"] != "file_removed" {
			t.Errorf("%s: %d %v", u, code, body)
		}
	}
	items, _ := e.notifications(t, ada, "")
	if len(items) == 0 || items[0]["type"] != "attachment_blocked" || items[0]["link"] != "/messaging/"+id || items[0]["actor"] != nil {
		t.Errorf("notifications = %v", items)
	}
	items, _ = e.notifications(t, bob, "")
	for _, it := range items {
		if it["type"] == "attachment_blocked" {
			t.Errorf("bob was told about ada's file: %v", it)
		}
	}
}

// FR-16.7: the type comes from the content, an image loses its EXIF, and 25 MB is the limit.
func TestAttachmentChecks(t *testing.T) {
	e := newMediaEnv(t)
	ada, bob := e.newProfileUser(t), e.newProfileUser(t)
	e.link(t, ada, bob)
	id, _ := e.startChat(t, ada, bob, "hello")

	exe := append([]byte("MZ\x90\x00\x03\x00\x00\x00"), bytes.Repeat([]byte{0}, 256)...)
	if code, body := e.uploadFile(t, ada, id, "report.pdf", exe); code != http.StatusUnsupportedMediaType || body["code"] != "unsupported_type" {
		t.Errorf("renamed exe: %d %v", code, body)
	}
	big := append([]byte("%PDF-"), bytes.Repeat([]byte("x"), mediausecase.MaxAttachmentBytes)...)
	if code, body := e.uploadFile(t, ada, id, "big.pdf", big); code != http.StatusRequestEntityTooLarge || body["code"] != "file_too_large" {
		t.Errorf("over 25 MB: %d %v", code, body)
	}

	jpg, err := os.ReadFile(filepath.Join("..", "..", "internal", "media", "adapter", "imaging", "testdata", "exif.jpg"))
	if err != nil {
		t.Fatal(err)
	}
	code, up := e.uploadFile(t, ada, id, "photo.jpg", jpg)
	if code != http.StatusCreated || up["contentType"] != "image/jpeg" || up["name"] != "photo.jpg" {
		t.Fatalf("photo: %d %v", code, up)
	}
	fileID := up["id"].(string)
	if code, sent := e.sendFiles(t, ada, id, fileID); code != http.StatusCreated {
		t.Fatalf("send: %d %v", code, sent)
	}
	e.scanAll(t, &fakeClamd{})
	_, _, header, content := e.download(t, bob, attachmentPath(fileID))
	if header == nil || bytes.Contains(content, []byte("SECRET-EXIF-MARKER")) || bytes.Contains(content, []byte("Exif\x00\x00")) {
		t.Errorf("EXIF kept in the stored image (%d bytes)", len(content))
	}
}

// FR-16.6, NFR-13: a member who joined after the message cannot download its file; a member
// from before can.
func TestGroupAttachmentFollowsJoinedAt(t *testing.T) {
	e := newMediaEnv(t)
	owner, ben, cat := e.newProfileUser(t), e.newProfileUser(t), e.newProfileUser(t)
	g := e.newGroup(t, owner, ben)
	fileID := e.mustUpload(t, owner, g, "plan.pdf", samplePDF)
	if code, sent := e.sendFiles(t, owner, g, fileID); code != http.StatusCreated {
		t.Fatalf("send: %d %v", code, sent)
	}
	e.scanAll(t, &fakeClamd{})
	e.link(t, owner, cat)
	if code, body := e.addMembers(t, owner, g, cat); code != http.StatusOK {
		t.Fatalf("add: %d %v", code, body)
	}
	if code, _, _, _ := e.download(t, ben, attachmentPath(fileID)); code != http.StatusFound {
		t.Errorf("member from before: %d", code)
	}
	if code, _, _, _ := e.download(t, cat, attachmentPath(fileID)); code != http.StatusNotFound {
		t.Errorf("member who joined later: %d", code)
	}
	// Only a member who can send now may upload.
	if code, _ := e.uploadFile(t, e.newProfileUser(t), g, "x.pdf", []byte(samplePDF)); code != http.StatusNotFound {
		t.Errorf("non-member upload: %d", code)
	}
}

// FR-16.9, NFR-13: the report lists files with their status, and the administrator's download
// goes through the same presigned flow and is recorded.
func TestAdminDownloadsReportedAttachment(t *testing.T) {
	e := newMediaEnv(t)
	ada, bob, admin := e.newProfileUser(t), e.newProfileUser(t), e.newProfileUser(t)
	e.link(t, ada, bob)
	id, _ := e.startChat(t, ada, bob, "hello")
	fileID := e.mustUpload(t, bob, id, "proof.pdf", samplePDF)
	if code, sent := e.sendFiles(t, bob, id, fileID); code != http.StatusCreated {
		t.Fatalf("send: %d %v", code, sent)
	}
	report := e.report(t, ada, id, `{"reason":"spam"}`)
	path := adminReports + "/" + report + "/attachments/" + fileID
	if code, body := e.asAdmin(t, admin, http.MethodGet, path, ""); code != http.StatusConflict || body["code"] != "scan_pending" {
		t.Errorf("before the scan: %d %v", code, body)
	}
	e.scanAll(t, &fakeClamd{})
	code, detail := e.asAdmin(t, admin, http.MethodGet, adminReports+"/"+report, "")
	if code != http.StatusOK {
		t.Fatalf("detail: %d %v", code, detail)
	}
	var listed []any
	for _, m := range detail["messages"].([]any) {
		if files := m.(map[string]any)["attachments"].([]any); len(files) > 0 {
			listed = files
		}
	}
	if len(listed) != 1 || listed[0].(map[string]any)["status"] != "clean" {
		t.Errorf("listed = %v", listed)
	}

	before := e.countRows(t, "SELECT count(*) FROM message_report_attachment_downloads WHERE report_id = $1 AND file_id = $2 AND admin_id = $3", report, fileID, admin)
	req := e.asAdminRaw(t, admin, path)
	if req.Code != http.StatusFound || req.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("admin download: %d %v", req.Code, req.Header())
	}
	header, content := e.follow(t, req.Header().Get("Location"))
	if string(content) != samplePDF || !strings.HasPrefix(header.Get("Content-Disposition"), "attachment;") {
		t.Errorf("content %q, disposition %q", content, header.Get("Content-Disposition"))
	}
	if after := e.countRows(t, "SELECT count(*) FROM message_report_attachment_downloads WHERE report_id = $1 AND file_id = $2 AND admin_id = $3", report, fileID, admin); after != before+1 {
		t.Errorf("audit rows %d -> %d", before, after)
	}
	// A researcher cannot use the administrator's path (SRS 2.3).
	if code, _ := e.call(t, ada, http.MethodGet, path, ""); code != http.StatusForbidden {
		t.Errorf("researcher: %d", code)
	}
}

// FR-16.7: several workers never scan one file twice; each file is scanned exactly once.
func TestTwoWorkersNeverScanAFileTwice(t *testing.T) {
	e := newMediaEnv(t)
	ada, bob := e.newProfileUser(t), e.newProfileUser(t)
	e.link(t, ada, bob)
	id, _ := e.startChat(t, ada, bob, "hello")
	const n = 12
	for i := range n {
		e.mustUpload(t, ada, id, fmt.Sprintf("f%d.pdf", i), fmt.Sprintf("%s%% file %d\n", samplePDF, i))
	}
	scanner := &fakeClamd{delay: 20 * time.Millisecond}
	var wg sync.WaitGroup
	for range 3 {
		w := e.worker(scanner)
		wg.Go(func() {
			for {
				claimed, err := w.RunOnce(context.Background())
				if err != nil {
					t.Error(err)
					return
				}
				if claimed == 0 {
					return
				}
			}
		})
	}
	wg.Wait()
	if len(scanner.seen) != n {
		t.Errorf("%d distinct files scanned, want %d", len(scanner.seen), n)
	}
	for content, times := range scanner.seen {
		if times != 1 {
			t.Errorf("a file was scanned %d times: %q", times, content[len(content)-10:])
		}
	}
	if left := e.countRows(t, "SELECT count(*) FROM stored_files WHERE scan_status <> 'clean'"); left != 0 {
		t.Errorf("%d files not clean", left)
	}
}

// FR-16.7: a file not sent within a day is deleted and its object queued.
func TestUnusedAttachmentIsPurged(t *testing.T) {
	e := newMediaEnv(t)
	ada, bob := e.newProfileUser(t), e.newProfileUser(t)
	e.link(t, ada, bob)
	id, _ := e.startChat(t, ada, bob, "hello")
	fileID := e.mustUpload(t, ada, id, "late.pdf", samplePDF)
	if _, err := e.pool.Exec(t.Context(), "UPDATE message_attachments SET created_at = now() - interval '2 days', expires_at = now() - interval '1 day' WHERE file_id = $1", fileID); err != nil {
		t.Fatal(err)
	}
	if n, err := e.messaging.PurgeExpiredAttachments(t.Context()); err != nil || n != 1 {
		t.Fatalf("purged %d, %v", n, err)
	}
	if !e.queued(t, "conversations/"+id+"/attachments/"+fileID) {
		t.Error("object not queued")
	}
	if code, body := e.sendFiles(t, ada, id, fileID); code != http.StatusBadRequest || body["code"] != "invalid_attachment" {
		t.Errorf("purged file sent: %d %v", code, body)
	}
}

// FR-16.7: 20 uploads an hour; the 21st waits.
func TestAttachmentUploadLimit(t *testing.T) {
	e := newMediaEnv(t)
	ada, bob := e.newProfileUser(t), e.newProfileUser(t)
	e.link(t, ada, bob)
	id, _ := e.startChat(t, ada, bob, "hello")
	for i := range 20 {
		e.mustUpload(t, ada, id, fmt.Sprintf("f%d.pdf", i), samplePDF)
	}
	body, ct := multipartFile(t, "x.pdf", samplePDF)
	rec := e.send(t, ada, http.MethodPost, "/api/v1/conversations/"+id+"/attachments", body, ct)
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
		t.Errorf("21st upload: %d %s", rec.Code, rec.Body)
	}
}
