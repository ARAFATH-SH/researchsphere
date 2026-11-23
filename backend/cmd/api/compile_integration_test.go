//go:build integration

// End-to-end tests of compiling (FR-13.1, FR-13.2, FR-13.4 to FR-13.7; slice 23) through the
// real router, usecases, PostgreSQL, Redis and MinIO. The compile worker is played by the
// test on the internal listener with the compile token; the sandbox itself is tested in
// compile/ (make test-compile-sandbox).
package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/config"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/testenv"
	projectredis "github.com/ARAFATH-SH/researchsphere/backend/internal/project/adapter/redis"
)

// compileJSON calls a compile worker operation on the internal listener with its token.
func (e mediaEnv) compileJSON(t *testing.T, method, path, body string) (int, map[string]any) {
	t.Helper()
	rec := sendInternal(t, e.internal, method, path, body, testCompileToken)
	if rec.Body.Len() == 0 {
		return rec.Code, nil
	}
	return rec.Code, decodeBody(t, rec.Body.Bytes())
}

func (e mediaEnv) compileRaw(t *testing.T, method, path, contentType string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), method, path, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+testCompileToken)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	rec := httptest.NewRecorder()
	e.internal.ServeHTTP(rec, req)
	return rec
}

// workerAlive records a compile worker as alive, as its claims do.
func (e mediaEnv) workerAlive(t *testing.T) {
	t.Helper()
	if err := e.rdb.Set(t.Context(), "compile:worker:seen", "1", time.Minute).Err(); err != nil {
		t.Fatal(err)
	}
}

// cleanPDFs deletes the compiled PDFs of the project from MinIO when the test ends.
func (e mediaEnv) cleanPDFs(t *testing.T, projectID string) {
	t.Helper()
	t.Cleanup(func() {
		ctx := context.Background()
		rows, err := e.pool.Query(ctx, `SELECT object_key FROM stored_files WHERE project_id = $1 AND purpose = 'pdf'
			UNION SELECT object_key FROM storage_deletions WHERE object_key LIKE 'projects/' || $1::text || '/compiles/%'`, projectID)
		if err != nil {
			return
		}
		defer rows.Close()
		for rows.Next() {
			var k string
			if rows.Scan(&k) == nil {
				_ = e.files.Delete(ctx, k)
			}
		}
	})
}

func compilesPath(projectID string) string { return "/api/v1/projects/" + projectID + "/compiles" }

func (e mediaEnv) claim(t *testing.T) map[string]any {
	t.Helper()
	code, job := e.compileJSON(t, http.MethodPost, "/api/v1/internal/compile/claim", `{"workerId":"test-worker"}`)
	if code != http.StatusOK {
		t.Fatalf("claim: %d %v", code, job)
	}
	return job
}

func (e mediaEnv) finish(t *testing.T, jobID string, result map[string]any) int {
	t.Helper()
	b, _ := json.Marshal(result)
	code, body := e.compileJSON(t, http.MethodPost, "/api/v1/internal/compile/jobs/"+jobID+"/finish", string(b))
	if code != http.StatusNoContent && code != http.StatusNotFound {
		t.Fatalf("finish: %d %v", code, body)
	}
	return code
}

func (e mediaEnv) sourceTar(t *testing.T, jobID string) map[string]string {
	t.Helper()
	rec := e.compileRaw(t, http.MethodGet, "/api/v1/internal/compile/jobs/"+jobID+"/source", "", nil)
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "application/x-tar" {
		t.Fatalf("source: %d %s", rec.Code, rec.Body)
	}
	out := map[string]string{}
	tr := tar.NewReader(rec.Body)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return out
		}
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(tr)
		out[h.Name] = string(b)
	}
}

func alphaPNG(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "internal", "media", "adapter", "imaging", "testdata", "alpha.png"))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// infectedPDF is a PDF figure that the fake scanner finds infected (EICAR).
var infectedPDF = []byte(eicarPDF)

// FR-13.1, FR-13.2, FR-13.4, FR-13.6 and SRS 2.3: the whole flow, with its role rules.
func TestCompileFlow(t *testing.T) {
	e := newModEnv(t)
	owner, editor, viewer, pid, _ := e.collabProject(t)
	e.cleanPDFs(t, pid)
	stranger := e.projectUser(t)
	path := "/api/v1/projects/" + pid

	// Figures: one clean, one infected, one still pending (FR-12.2's scan).
	if code, body := e.uploadFigure(t, owner, path, "figures/clean.png", alphaPNG(t)); code != http.StatusCreated {
		t.Fatalf("upload: %d %v", code, body)
	}
	if code, body := e.uploadFigure(t, owner, path, "figures/bad.pdf", infectedPDF); code != http.StatusCreated {
		t.Fatalf("upload: %d %v", code, body)
	}
	e.scanAll(t, &fakeClamd{})
	if code, body := e.uploadFigure(t, owner, path, "figures/pending.png", alphaPNG(t)); code != http.StatusCreated {
		t.Fatalf("upload: %d %v", code, body)
	}

	// SRS 2.3: editors and the owner compile; a viewer may not; a non-member and an
	// administrator outside the project do not see it.
	code, body := e.json(t, viewer, http.MethodPost, compilesPath(pid), "")
	wantError(t, "viewer", code, body, http.StatusForbidden, "forbidden")
	code, body = e.json(t, stranger, http.MethodPost, compilesPath(pid), "")
	wantError(t, "non-member", code, body, http.StatusNotFound, "not_found")
	rec := e.sendRole(t, e.admin, "admin", true, http.MethodPost, compilesPath(pid), "")
	if rec.Code != http.StatusNotFound {
		t.Errorf("administrator: %d %s", rec.Code, rec.Body)
	}
	// NFR-10.2: no worker, no queue: an answer at once.
	code, body = e.json(t, editor, http.MethodPost, compilesPath(pid), "")
	wantError(t, "no worker", code, body, http.StatusServiceUnavailable, "compile_unavailable")

	e.workerAlive(t)
	code, first := e.json(t, editor, http.MethodPost, compilesPath(pid), "")
	if code != http.StatusAccepted || first["status"] != "queued" || first["mainFilePath"] != "main.tex" || first["engine"] != "pdflatex" {
		t.Fatalf("start: %d %v", code, first)
	}
	firstID := first["id"].(string)
	// FR-13.4: one compile per project at a time, and the refusal names it.
	code, body = e.json(t, owner, http.MethodPost, compilesPath(pid), "")
	if code != http.StatusConflict || body["code"] != "compile_running" || body["compileId"] != firstID {
		t.Errorf("second start: %d %v", code, body)
	}

	job := e.claim(t)
	if job["id"] != firstID || job["projectId"] != pid || job["mainFilePath"] != "main.tex" {
		t.Fatalf("claimed %v", job)
	}
	if _, latest := e.json(t, viewer, http.MethodGet, compilesPath(pid)+"/latest", ""); latest["status"] != "running" {
		t.Errorf("latest while running: %v", latest)
	}

	// The worker reads the files: text, folders and the clean figure only.
	files := e.sourceTar(t, firstID)
	var names []string
	for n := range files {
		names = append(names, n)
	}
	slices.Sort(names)
	if !slices.Equal(names, []string{"figures/", "figures/clean.png", "main.tex"}) {
		t.Errorf("source entries %v", names)
	}
	if !strings.Contains(files["main.tex"], `\documentclass`) || files["figures/clean.png"] != string(alphaPNG(t)) {
		t.Error("source content differs from the project's")
	}

	// The PDF: refused when it is not one, stored when it is.
	if rec := e.compileRaw(t, http.MethodPut, "/api/v1/internal/compile/jobs/"+firstID+"/pdf", "application/pdf", []byte("<html>")); rec.Code != http.StatusBadRequest {
		t.Errorf("not a PDF: %d %s", rec.Code, rec.Body)
	}
	pdf := []byte("%PDF-1.5\n% sample\n%%EOF\n")
	if rec := e.compileRaw(t, http.MethodPut, "/api/v1/internal/compile/jobs/"+firstID+"/pdf", "application/pdf", pdf); rec.Code != http.StatusNoContent {
		t.Fatalf("store PDF: %d %s", rec.Code, rec.Body)
	}
	log := "This is pdfTeX\n(./main.tex\n./main.tex:3: Undefined control sequence.\nl.3 \\foo\n)\nOutput written on main.pdf (1 page, 24 bytes).\n"
	if code := e.finish(t, firstID, map[string]any{"outcome": "completed", "exitCode": 12, "durationMs": 2100, "log": log, "bibtexLog": ""}); code != http.StatusNoContent {
		t.Fatalf("finish: %d", code)
	}
	// A second report of a finished job changes nothing.
	if code := e.finish(t, firstID, map[string]any{"outcome": "timed_out", "durationMs": 1, "log": "", "bibtexLog": ""}); code != http.StatusNoContent {
		t.Errorf("second finish: %d", code)
	}

	code, done := e.json(t, viewer, http.MethodGet, compilesPath(pid)+"/latest", "")
	errs, _ := done["errors"].([]any)
	if code != http.StatusOK || done["status"] != "succeeded" || done["hasPdf"] != true || done["hasLog"] != true || len(errs) != 1 ||
		done["pages"] != float64(1) || done["durationMs"] != float64(2100) {
		t.Fatalf("finished: %d %v", code, done)
	}
	if p := errs[0].(map[string]any); p["file"] != "main.tex" || p["line"] != float64(3) || p["severity"] != "error" {
		t.Errorf("error %v", p)
	}
	skipped, _ := done["skipped"].([]any)
	reasons := map[string]string{}
	for _, s := range skipped {
		m := s.(map[string]any)
		reasons[m["path"].(string)] = m["reason"].(string)
	}
	if reasons["figures/bad.pdf"] != "infected" || reasons["figures/pending.png"] != "scan_pending" || len(reasons) != 2 {
		t.Errorf("skipped %v", skipped)
	}

	// FR-13.6: members download the PDF, with the headers that sandbox its scripts.
	rec = e.sendRole(t, viewer, "researcher", true, http.MethodGet, path+"/pdf?disposition=inline", "")
	h := rec.Header()
	if rec.Code != http.StatusOK || !bytes.Equal(rec.Body.Bytes(), pdf) || h.Get("Content-Type") != "application/pdf" ||
		h.Get("X-Content-Type-Options") != "nosniff" || !strings.HasPrefix(h.Get("Content-Security-Policy"), "sandbox") ||
		!strings.HasPrefix(h.Get("Content-Disposition"), "inline") || h.Get("X-Compile-Id") != firstID {
		t.Errorf("PDF: %d %v", rec.Code, h)
	}
	if rec := e.sendRole(t, stranger, "researcher", true, http.MethodGet, path+"/pdf", ""); rec.Code != http.StatusNotFound {
		t.Errorf("PDF for a non-member: %d", rec.Code)
	}
	rec = e.sendRole(t, viewer, "researcher", true, http.MethodGet, compilesPath(pid)+"/"+firstID+"/log", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Undefined control sequence") {
		t.Errorf("log: %d %s", rec.Code, rec.Body)
	}
	firstPDFKey := ""
	if err := e.pool.QueryRow(t.Context(), `SELECT sf.object_key FROM compile_jobs j JOIN stored_files sf ON sf.id = j.pdf_file_id WHERE j.id = $1`, firstID).Scan(&firstPDFKey); err != nil {
		t.Fatal(err)
	}
	if !e.objectExists(t, firstPDFKey) {
		t.Error("the PDF is not in storage")
	}

	// A failed compile keeps the last successful PDF (SRS 7, Retention), but only the newest log.
	code, second := e.json(t, owner, http.MethodPost, compilesPath(pid), "")
	if code != http.StatusAccepted {
		t.Fatalf("second compile: %d %v", code, second)
	}
	e.claim(t)
	e.sourceTar(t, second["id"].(string))
	e.finish(t, second["id"].(string), map[string]any{"outcome": "completed", "exitCode": 12, "durationMs": 900, "log": "! Emergency stop.\n", "bibtexLog": ""})
	_, latest := e.json(t, owner, http.MethodGet, compilesPath(pid)+"/latest", "")
	if latest["status"] != "failed" || latest["failure"] != "no_pdf" || latest["hasPdf"] != false {
		t.Errorf("failed compile: %v", latest)
	}
	if rec := e.sendRole(t, owner, "researcher", true, http.MethodGet, path+"/pdf", ""); rec.Code != http.StatusOK || rec.Header().Get("X-Compile-Id") != firstID {
		t.Errorf("PDF after a failure: %d %v", rec.Code, rec.Header())
	}
	code, body = e.json(t, owner, http.MethodGet, compilesPath(pid)+"/"+firstID+"/log", "")
	wantError(t, "older log", code, body, http.StatusNotFound, "no_log")

	// A newer success replaces the PDF; the old one's object is queued for deletion.
	_, third := e.json(t, owner, http.MethodPost, compilesPath(pid), "")
	e.claim(t)
	if rec := e.compileRaw(t, http.MethodPut, "/api/v1/internal/compile/jobs/"+third["id"].(string)+"/pdf", "application/pdf", pdf); rec.Code != http.StatusNoContent {
		t.Fatal(rec.Body)
	}
	e.finish(t, third["id"].(string), map[string]any{"outcome": "completed", "durationMs": 900, "log": "", "bibtexLog": ""})
	if !e.queued(t, firstPDFKey) {
		t.Error("the replaced PDF's object is not queued for deletion")
	}
	if n := e.countRows(t, `SELECT count(*) FROM stored_files WHERE project_id = $1 AND purpose = 'pdf'`, pid); n != 1 {
		t.Errorf("%d PDFs kept, want 1", n)
	}
}

// FR-13.5 and the lease: a worker that stops never holds the project's slot; its job fails as
// worker_lost, and a late report of it is refused.
func TestCompileLeaseExpiry(t *testing.T) {
	e := newModEnv(t)
	_, editor, _, pid, _ := e.collabProject(t)
	e.workerAlive(t)
	_, job := e.json(t, editor, http.MethodPost, compilesPath(pid), "")
	e.claim(t)
	if _, err := e.pool.Exec(t.Context(), `UPDATE compile_jobs SET lease_until = now() - interval '1 second' WHERE id = $1`, job["id"]); err != nil {
		t.Fatal(err)
	}
	if n, err := e.compiles.Sweep(t.Context()); err != nil || n != 1 {
		t.Fatalf("sweep: %d %v", n, err)
	}
	_, latest := e.json(t, editor, http.MethodGet, compilesPath(pid)+"/latest", "")
	if latest["status"] != "failed" || latest["failure"] != "worker_lost" {
		t.Errorf("lost job: %v", latest)
	}
	if code := e.finish(t, job["id"].(string), map[string]any{"outcome": "completed", "durationMs": 1, "log": "", "bibtexLog": ""}); code != http.StatusNotFound {
		t.Errorf("late finish: %d", code)
	}
	if code, body := e.compileJSON(t, http.MethodGet, "/api/v1/internal/compile/jobs/"+job["id"].(string)+"/source", ""); code != http.StatusNotFound {
		t.Errorf("late source: %d %v", code, body)
	}
	if code, body := e.json(t, editor, http.MethodPost, compilesPath(pid), ""); code != http.StatusAccepted {
		t.Errorf("the slot is free again: %d %v", code, body)
	}
}

// The input limit: a project too large to compile fails without running.
func TestCompileInputTooLarge(t *testing.T) {
	cfg := config.Config{CompileMaxPDFBytes: 1 << 20, CompileMaxInputBytes: 10, CompileQueueMax: 5, ArchiveMaxBytes: 1 << 20}
	e := newMediaEnvOnPool(t, testenv.DB(t), "", "", envOptions{compile: &cfg})
	owner := e.projectUser(t)
	pid, _ := e.ownProject(t, owner, testenv.Word(t)+" big")
	e.workerAlive(t)
	_, job := e.json(t, owner, http.MethodPost, compilesPath(pid), "")
	e.claim(t)
	code, body := e.compileJSON(t, http.MethodGet, "/api/v1/internal/compile/jobs/"+job["id"].(string)+"/source", "")
	wantError(t, "too large", code, body, http.StatusConflict, "input_too_large")
	_, latest := e.json(t, owner, http.MethodGet, compilesPath(pid)+"/latest", "")
	if latest["status"] != "failed" || latest["failure"] != "input_too_large" {
		t.Errorf("job: %v", latest)
	}
}

// Live edits are compiled: when a file is open on the sync server, the compile asks it to
// store the project's documents and reads the text it wrote; without an answer it compiles the
// stored text and says so.
func TestCompileUsesLiveText(t *testing.T) {
	e := newModEnv(t)
	_, editor, _, pid, mainID := e.collabProject(t)
	e.workerAlive(t)
	if err := e.rdb.Set(t.Context(), projectredis.LivePrefix+mainID, "1", time.Minute).Err(); err != nil {
		t.Fatal(err)
	}

	// No sync server listens: the stored text, not confirmed.
	_, job := e.json(t, editor, http.MethodPost, compilesPath(pid), "")
	e.claim(t)
	files := e.sourceTar(t, job["id"].(string))
	if !strings.Contains(files["main.tex"], `\documentclass`) {
		t.Errorf("stored text: %q", files["main.tex"])
	}
	e.finish(t, job["id"].(string), map[string]any{"outcome": "completed", "durationMs": 1, "log": "", "bibtexLog": ""})
	if _, latest := e.json(t, editor, http.MethodGet, compilesPath(pid)+"/latest", ""); latest["liveTextConfirmed"] != false {
		t.Errorf("unconfirmed flush reported as confirmed: %v", latest)
	}

	// A sync server that stores the live text when asked, then answers ok.
	sub := e.rdb.Subscribe(t.Context(), projectredis.SyncChannel(e.rdb.Options().DB))
	t.Cleanup(func() { _ = sub.Close() })
	if _, err := sub.Receive(t.Context()); err != nil {
		t.Fatal(err)
	}
	responder := goredis.NewClient(e.rdb.Options())
	t.Cleanup(func() { _ = responder.Close() })
	go func() {
		for m := range sub.Channel() {
			var ev projectredis.SyncEvent
			if json.Unmarshal([]byte(m.Payload), &ev) != nil || ev.Kind != projectredis.SyncFlush || ev.ProjectID != pid {
				continue
			}
			_, _ = e.pool.Exec(context.Background(), `UPDATE project_files SET content = 'LIVE TEXT', size_bytes = 9, version = version + 1 WHERE id = $1`, mainID)
			responder.LPush(context.Background(), projectredis.FlushAckPrefix+ev.RequestID, "ok")
		}
	}()
	_, job = e.json(t, editor, http.MethodPost, compilesPath(pid), "")
	e.claim(t)
	if files := e.sourceTar(t, job["id"].(string)); files["main.tex"] != "LIVE TEXT" {
		t.Errorf("compiled text %q, want the live text", files["main.tex"])
	}
	e.finish(t, job["id"].(string), map[string]any{"outcome": "completed", "durationMs": 1, "log": "", "bibtexLog": ""})
	if _, latest := e.json(t, editor, http.MethodGet, compilesPath(pid)+"/latest", ""); latest["liveTextConfirmed"] != true {
		t.Errorf("confirmed flush: %v", latest)
	}
}

// FR-13.1: the owner chooses the main file; the compile uses it.
func TestMainFileSetting(t *testing.T) {
	e := newModEnv(t)
	owner, editor, _, pid, _ := e.collabProject(t)
	path := "/api/v1/projects/" + pid
	if code, body := e.json(t, owner, http.MethodPost, path+"/files", `{"path":"paper/Thesis.tex","kind":"text","content":"x"}`); code != http.StatusCreated {
		t.Fatalf("create: %d %v", code, body)
	}
	code, body := e.json(t, editor, http.MethodPatch, path, `{"mainFilePath":"paper/Thesis.tex"}`)
	wantError(t, "editor", code, body, http.StatusForbidden, "forbidden")
	code, body = e.json(t, owner, http.MethodPatch, path, `{"mainFilePath":"paper/missing.tex"}`)
	wantError(t, "missing file", code, body, http.StatusConflict, "main_file_invalid")
	code, body = e.json(t, owner, http.MethodPatch, path, `{"mainFilePath":null}`)
	wantError(t, "null", code, body, http.StatusBadRequest, "bad_request")
	if code, body := e.json(t, owner, http.MethodPatch, path, `{"mainFilePath":"paper/thesis.tex"}`); code != http.StatusOK {
		t.Fatalf("set: %d %v", code, body)
	}
	if _, _, main := e.tree(t, owner, path); main != "paper/Thesis.tex" {
		t.Errorf("main file %q", main)
	}
	e.workerAlive(t)
	if _, job := e.json(t, editor, http.MethodPost, compilesPath(pid), ""); job["mainFilePath"] != "paper/Thesis.tex" {
		t.Errorf("compile main file: %v", job)
	}
	// FR-13.4's other refusal: no text file at the main file's path.
	_, files, _ := e.tree(t, owner, path)
	if code, body := e.json(t, owner, http.MethodDelete, path+"/files/"+files["paper"]["id"].(string), ""); code != http.StatusNoContent {
		t.Fatalf("delete: %d %v", code, body)
	}
	if _, err := e.pool.Exec(t.Context(), `DELETE FROM compile_jobs WHERE project_id = $1`, pid); err != nil {
		t.Fatal(err)
	}
	code, body = e.json(t, editor, http.MethodPost, compilesPath(pid), "")
	wantError(t, "main file gone", code, body, http.StatusConflict, "main_file_missing")
}

// FR-13.7: members download the files as a ZIP; figures only when clean.
func TestProjectArchive(t *testing.T) {
	e := newModEnv(t)
	owner, _, viewer, pid, _ := e.collabProject(t)
	stranger := e.projectUser(t)
	path := "/api/v1/projects/" + pid
	if code, body := e.json(t, owner, http.MethodPost, path+"/files", `{"path":"chapters/intro.tex","kind":"text","content":"intro"}`); code != http.StatusCreated {
		t.Fatalf("create: %d %v", code, body)
	}
	if code, body := e.uploadFigure(t, owner, path, "figures/clean.png", alphaPNG(t)); code != http.StatusCreated {
		t.Fatalf("upload: %d %v", code, body)
	}
	if code, body := e.uploadFigure(t, owner, path, "figures/bad.pdf", infectedPDF); code != http.StatusCreated {
		t.Fatalf("upload: %d %v", code, body)
	}
	e.scanAll(t, &fakeClamd{})
	if code, body := e.uploadFigure(t, owner, path, "figures/pending.png", alphaPNG(t)); code != http.StatusCreated {
		t.Fatalf("upload: %d %v", code, body)
	}

	if rec := e.sendRole(t, stranger, "researcher", true, http.MethodGet, path+"/archive", ""); rec.Code != http.StatusNotFound {
		t.Errorf("non-member: %d", rec.Code)
	}
	rec := e.sendRole(t, viewer, "researcher", true, http.MethodGet, path+"/archive", "")
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "application/zip" ||
		!strings.HasPrefix(rec.Header().Get("Content-Disposition"), "attachment") {
		t.Fatalf("archive: %d %v", rec.Code, rec.Header())
	}
	zr, err := zip.NewReader(bytes.NewReader(rec.Body.Bytes()), int64(rec.Body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, f := range zr.File {
		r, _ := f.Open()
		b, _ := io.ReadAll(r)
		got[f.Name] = string(b)
	}
	if got["chapters/intro.tex"] != "intro" || got["figures/clean.png"] != string(alphaPNG(t)) || !strings.Contains(got["main.tex"], `\documentclass`) {
		t.Errorf("archive contents: %v", keys(got))
	}
	if _, ok := got["figures/bad.pdf"]; ok {
		t.Error("an infected figure is in the archive")
	}
	if _, ok := got["figures/pending.png"]; ok {
		t.Error("an unscanned figure is in the archive")
	}
}

func keys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

// compile.updated reaches every member, viewers included, when a compile is queued, starts
// and ends (docs/realtime.md); a non-member hears nothing.
func TestCompileEvents(t *testing.T) {
	e := newModEnv(t)
	owner, editor, viewer, pid, _ := e.collabProject(t)
	stranger := e.projectUser(t)
	srv := serve(t, e.router)
	clients := map[string]*wsClient{}
	for _, u := range []string{owner, editor, viewer, stranger} {
		clients[u] = dial(t, srv, u)
	}
	e.workerAlive(t)
	_, job := e.json(t, editor, http.MethodPost, compilesPath(pid), "")
	id := job["id"].(string)
	status := func(want string) func(map[string]any) bool {
		return func(d map[string]any) bool {
			c, _ := d["compile"].(map[string]any)
			return d["projectId"] == pid && c["id"] == id && c["status"] == want
		}
	}
	for _, u := range []string{owner, editor, viewer} {
		clients[u].expect(t, "compile.updated", status("queued"))
	}
	e.claim(t)
	for _, u := range []string{owner, editor, viewer} {
		clients[u].expect(t, "compile.updated", status("running"))
	}
	e.finish(t, id, map[string]any{"outcome": "timed_out", "durationMs": 60000, "log": "", "bibtexLog": ""})
	for _, u := range []string{owner, editor, viewer} {
		clients[u].expect(t, "compile.updated", status("timed_out"))
	}
	clients[stranger].silent(t, "compile.updated", 300*time.Millisecond)
}
