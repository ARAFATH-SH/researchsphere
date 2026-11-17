//go:build integration

// End-to-end tests of project files (FR-10.4, FR-12.1, FR-12.2, FR-12.6) through the real
// router, usecase, PostgreSQL store, media checks, MinIO and the scan worker with a fake
// scanner. They reuse the environments of media_integration_test.go.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/storage"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/testenv"
	projectusecase "github.com/ARAFATH-SH/researchsphere/backend/internal/project/usecase"
)

// member gives userID role in the project directly, as accepting a request would.
func (e mediaEnv) member(t *testing.T, projectID, userID, role string) {
	t.Helper()
	if _, err := e.pool.Exec(t.Context(), `INSERT INTO project_members (project_id, user_id, role) VALUES ($1, $2, $3)`,
		projectID, userID, role); err != nil {
		t.Fatal(err)
	}
}

// tree lists the paths of the project's files as userID sees them.
func (e mediaEnv) tree(t *testing.T, userID, projectPath string) ([]string, map[string]map[string]any, string) {
	t.Helper()
	code, body := e.json(t, userID, http.MethodGet, projectPath+"/files", "")
	if code != http.StatusOK {
		t.Fatalf("tree: %d %v", code, body)
	}
	var paths []string
	byPath := map[string]map[string]any{}
	for _, it := range itemsOf(body) {
		paths = append(paths, it["path"].(string))
		byPath[it["path"].(string)] = it
	}
	return paths, byPath, body["mainFilePath"].(string)
}

// startUpload asks for a presigned POST of size bytes to path.
func (e mediaEnv) startUpload(t *testing.T, userID, projectPath, path string, size int) (int, map[string]any) {
	t.Helper()
	body, err := json.Marshal(map[string]any{"path": path, "size": size})
	if err != nil {
		t.Fatal(err)
	}
	code, ticket := e.json(t, userID, http.MethodPost, projectPath+"/files/uploads", string(body))
	if code == http.StatusCreated {
		key := ticket["fields"].(map[string]any)["key"].(string)
		t.Cleanup(func() { _ = e.files.Delete(context.Background(), key) })
	}
	return code, ticket
}

// postToStorage sends content with a presigned POST, as the browser does: the fields, then
// the file part. It returns storage's status.
func postToStorage(t *testing.T, ticket map[string]any, content []byte) int {
	t.Helper()
	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	for k, v := range ticket["fields"].(map[string]any) {
		if err := w.WriteField(k, v.(string)); err != nil {
			t.Fatal(err)
		}
	}
	fw, err := w.CreateFormFile("file", "upload.bin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, ticket["url"].(string), &b)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	return resp.StatusCode
}

// completeUpload records the figure of an upload.
func (e mediaEnv) completeUpload(t *testing.T, userID, projectPath, uploadID string) (int, map[string]any) {
	t.Helper()
	code, body := e.json(t, userID, http.MethodPost, projectPath+"/files/uploads/"+uploadID+"/complete", "")
	if id, ok := body["id"].(string); ok && code == http.StatusCreated {
		key := e.figureKey(t, id)
		t.Cleanup(func() { _ = e.files.Delete(context.Background(), key) })
	}
	return code, body
}

// uploadFigure sends content as a figure at path the way the web client does: start, POST to
// storage, complete. Objects are deleted from the bucket when the test ends.
func (e mediaEnv) uploadFigure(t *testing.T, userID, projectPath, path string, content []byte) (int, map[string]any) {
	t.Helper()
	code, ticket := e.startUpload(t, userID, projectPath, path, len(content))
	if code != http.StatusCreated {
		return code, ticket
	}
	if status := postToStorage(t, ticket, content); status != http.StatusNoContent {
		t.Fatalf("storage refused the POST: %d", status)
	}
	return e.completeUpload(t, userID, projectPath, ticket["id"].(string))
}

func (e mediaEnv) figureKey(t *testing.T, fileID string) string {
	t.Helper()
	var key string
	if err := e.pool.QueryRow(t.Context(), `SELECT sf.object_key FROM project_files f JOIN stored_files sf ON sf.id = f.stored_file_id
		WHERE f.id = $1`, fileID).Scan(&key); err != nil {
		t.Fatal(err)
	}
	return key
}

func (e mediaEnv) objectExists(t *testing.T, key string) bool {
	t.Helper()
	_, err := e.files.Stat(t.Context(), key)
	if err != nil && !errors.Is(err, storage.ErrNotFound) {
		t.Fatal(err)
	}
	return err == nil
}

// FR-10.4: a new project starts with main.tex holding its title and its owner's name.
func TestNewProjectHasMainTex(t *testing.T) {
	e := newMediaEnv(t)
	ada := e.projectUser(t)
	e.rename(t, ada, "Ada Lovelace")
	_, path := e.ownProject(t, ada, "Graphs & 100% recall")
	paths, files, main := e.tree(t, ada, path)
	if !slices.Equal(paths, []string{"main.tex"}) || main != "main.tex" {
		t.Fatalf("tree = %v, main %q", paths, main)
	}
	f := files["main.tex"]
	if f["kind"] != "text" || f["version"] != 1.0 || f["updatedBy"].(map[string]any)["userId"] != ada {
		t.Errorf("main.tex = %v", f)
	}
	code, body := e.json(t, ada, http.MethodGet, path+"/files/"+f["id"].(string), "")
	content, _ := body["content"].(string)
	if code != http.StatusOK || !strings.Contains(content, `\title{Graphs \& 100\% recall}`) || !strings.Contains(content, `\author{Ada Lovelace}`) {
		t.Errorf("main.tex: %d\n%s", code, content)
	}
}

// SRS 2.3: members read, editors and the owner write, viewers only read; non-members and
// administrators outside the project get not found; a removed member loses access at once.
func TestProjectFileRoles(t *testing.T) {
	e := newMediaEnv(t)
	owner, editor, viewer, stranger, admin := e.projectUser(t), e.projectUser(t), e.projectUser(t), e.projectUser(t), e.projectUser(t)
	id, path := e.ownProject(t, owner, testenv.Word(t)+" roles")
	e.member(t, id, editor, "editor")
	e.member(t, id, viewer, "viewer")
	_, files, _ := e.tree(t, owner, path)
	main := path + "/files/" + files["main.tex"]["id"].(string)

	for _, who := range []string{owner, editor, viewer} {
		if code, body := e.json(t, who, http.MethodGet, main, ""); code != http.StatusOK || body["content"] == nil {
			t.Errorf("read by a member: %d %v", code, body)
		}
	}
	for name, who := range map[string]string{"non-member": stranger} {
		code, body := e.json(t, who, http.MethodGet, path+"/files", "")
		wantError(t, name+" lists", code, body, http.StatusNotFound, "not_found")
		code, body = e.json(t, who, http.MethodPost, path+"/files", `{"path":"x.tex","kind":"text"}`)
		wantError(t, name+" creates", code, body, http.StatusNotFound, "not_found")
	}
	// An administrator has no right inside a project (SRS 2.3).
	rec := e.sendRole(t, admin, "admin", true, http.MethodGet, path+"/files", "")
	if rec.Code != http.StatusNotFound {
		t.Errorf("administrator: %d %s", rec.Code, rec.Body)
	}

	code, body := e.json(t, viewer, http.MethodPost, path+"/files", `{"path":"v.tex","kind":"text"}`)
	wantError(t, "viewer creates", code, body, http.StatusForbidden, "forbidden")
	code, body = e.json(t, viewer, http.MethodPut, main+"/content", `{"content":"x","version":1}`)
	wantError(t, "viewer saves", code, body, http.StatusForbidden, "forbidden")
	code, body = e.json(t, viewer, http.MethodDelete, main, "")
	wantError(t, "viewer deletes", code, body, http.StatusForbidden, "forbidden")
	code, body = e.uploadFigure(t, viewer, path, "v.png", []byte("x"))
	wantError(t, "viewer uploads", code, body, http.StatusForbidden, "forbidden")

	if code, body := e.json(t, editor, http.MethodPost, path+"/files", `{"path":"e.tex","kind":"text","content":"hi"}`); code != http.StatusCreated {
		t.Errorf("editor creates: %d %v", code, body)
	}
	// Removed: the very next request is refused.
	if code, body := e.json(t, owner, http.MethodDelete, path+"/members/"+editor, ""); code != http.StatusNoContent {
		t.Fatalf("remove: %d %v", code, body)
	}
	code, body = e.json(t, editor, http.MethodGet, path+"/files", "")
	wantError(t, "removed member reads", code, body, http.StatusNotFound, "not_found")
	code, body = e.json(t, editor, http.MethodPut, main+"/content", `{"content":"x","version":1}`)
	wantError(t, "removed member saves", code, body, http.StatusNotFound, "not_found")
}

// FR-12.1: folders, rename and move with everything inside, case-insensitive paths, the path
// rules, deleting a folder with its contents; FR-12.6: saves with versions.
func TestProjectFileTree(t *testing.T) {
	e := newMediaEnv(t)
	ada, bob := e.projectUser(t), e.projectUser(t)
	id, path := e.ownProject(t, ada, testenv.Word(t)+" tree")
	e.member(t, id, bob, "editor")

	code, f := e.json(t, ada, http.MethodPost, path+"/files", `{"path":"src/sections/intro.tex","kind":"text","content":"Intro"}`)
	if code != http.StatusCreated || f["size"] != 5.0 {
		t.Fatalf("create nested: %d %v", code, f)
	}
	paths, files, _ := e.tree(t, ada, path)
	if want := []string{"main.tex", "src", "src/sections", "src/sections/intro.tex"}; !slices.Equal(paths, want) {
		t.Errorf("tree = %v, want %v", paths, want)
	}
	code, body := e.json(t, ada, http.MethodPost, path+"/files", `{"path":"SRC/Sections/INTRO.tex","kind":"text"}`)
	wantError(t, "same path, other case", code, body, http.StatusConflict, "path_taken")
	code, body = e.json(t, ada, http.MethodPost, path+"/files", `{"path":"my intro.tex","kind":"text"}`)
	if wantError(t, "space", code, body, http.StatusBadRequest, "invalid_path"); body["field"] != "path" ||
		!strings.Contains(body["message"].(string), "no spaces") {
		t.Errorf("space: %v", body)
	}
	code, body = e.json(t, ada, http.MethodPost, path+"/files", `{"path":"../escape.tex","kind":"text"}`)
	wantError(t, "dot dot", code, body, http.StatusBadRequest, "invalid_path")
	code, body = e.json(t, ada, http.MethodPost, path+"/files", `{"path":"main.tex/x.tex","kind":"text"}`)
	wantError(t, "through a file", code, body, http.StatusConflict, "parent_not_folder")
	code, body = e.json(t, ada, http.MethodPost, path+"/files", `{"path":"fake.pdf","kind":"text"}`)
	wantError(t, "text named like a figure", code, body, http.StatusBadRequest, "invalid_path")
	code, body = e.json(t, ada, http.MethodPost, path+"/files", fmt.Sprintf(`{"path":"big.tex","kind":"text","content":%q}`, strings.Repeat("x", projectusecase.MaxTextBytes+1)))
	wantError(t, "over 1 MiB", code, body, http.StatusRequestEntityTooLarge, "file_too_large")

	// The main file moves into src; then src moves, everything in it with it.
	mainID := files["main.tex"]["id"].(string)
	if code, body := e.json(t, bob, http.MethodPatch, path+"/files/"+mainID, `{"path":"src/main.tex"}`); code != http.StatusOK || body["path"] != "src/main.tex" {
		t.Fatalf("move main: %d %v", code, body)
	}
	srcID := files["src"]["id"].(string)
	code, body = e.json(t, bob, http.MethodPatch, path+"/files/"+srcID, `{"path":"src/sections/src"}`)
	wantError(t, "into itself", code, body, http.StatusBadRequest, "invalid_move")
	if code, body := e.json(t, bob, http.MethodPatch, path+"/files/"+srcID, `{"path":"paper/latex"}`); code != http.StatusOK {
		t.Fatalf("move folder: %d %v", code, body)
	}
	paths, files, main := e.tree(t, ada, path)
	want := []string{"paper", "paper/latex", "paper/latex/main.tex", "paper/latex/sections", "paper/latex/sections/intro.tex"}
	if !slices.Equal(paths, want) || main != "paper/latex/main.tex" {
		t.Errorf("after the move: %v, main %q", paths, main)
	}
	if files["paper/latex/sections/intro.tex"]["updatedBy"].(map[string]any)["userId"] != bob {
		t.Errorf("moved file's updatedBy = %v", files["paper/latex/sections/intro.tex"]["updatedBy"])
	}

	// Two tabs: the second save from the old version is refused with the current text.
	file := path + "/files/" + files["paper/latex/sections/intro.tex"]["id"].(string)
	if code, body := e.json(t, ada, http.MethodPut, file+"/content", `{"content":"From tab A","version":1}`); code != http.StatusOK || body["version"] != 2.0 {
		t.Fatalf("save A: %d %v", code, body)
	}
	code, body = e.json(t, bob, http.MethodPut, file+"/content", `{"content":"From tab B","version":1}`)
	if wantError(t, "stale save", code, body, http.StatusConflict, "version_conflict"); body["current"] == nil ||
		body["current"].(map[string]any)["content"] != "From tab A" || body["current"].(map[string]any)["version"] != 2.0 {
		t.Errorf("conflict body = %v", body)
	}
	if code, body := e.json(t, ada, http.MethodGet, file, ""); body["content"] != "From tab A" {
		t.Errorf("after the conflict: %d %v", code, body)
	}
	code, body = e.json(t, ada, http.MethodPut, path+"/files/"+files["paper"]["id"].(string)+"/content", `{"content":"x","version":1}`)
	wantError(t, "save a folder", code, body, http.StatusConflict, "not_a_text_file")
	code, body = e.json(t, ada, http.MethodPut, file+"/content", `{"content":"bell\u0007","version":2}`)
	if wantError(t, "control character", code, body, http.StatusBadRequest, "bad_request"); body["field"] != "content" {
		t.Errorf("control character: %v", body)
	}
	// A REST save clears the Yjs state slice 22 keeps beside the text.
	if _, err := e.pool.Exec(t.Context(), `UPDATE project_files SET ydoc_state = '\x01' WHERE path = 'paper/latex/sections/intro.tex'`); err != nil {
		t.Fatal(err)
	}
	if code, body := e.json(t, ada, http.MethodPut, file+"/content", `{"content":"v3","version":2}`); code != http.StatusOK {
		t.Fatalf("save: %d %v", code, body)
	}
	if n := e.countRows(t, `SELECT count(*) FROM project_files WHERE path = 'paper/latex/sections/intro.tex' AND ydoc_state IS NULL`); n != 1 {
		t.Error("the Yjs state was not cleared")
	}

	// Deleting a folder deletes everything in it.
	if code, body := e.json(t, ada, http.MethodDelete, path+"/files/"+files["paper/latex"]["id"].(string), ""); code != http.StatusNoContent {
		t.Fatalf("delete folder: %d %v", code, body)
	}
	if paths, _, _ := e.tree(t, ada, path); !slices.Equal(paths, []string{"paper"}) {
		t.Errorf("after the delete: %v", paths)
	}
	code, body = e.json(t, ada, http.MethodGet, file, "")
	wantError(t, "deleted file", code, body, http.StatusNotFound, "not_found")
}

// FR-12.2: figures go straight to storage, are typed by content only with EXIF dropped
// losslessly, and are scanned before anyone downloads them; infected ones are removed;
// deleting the project deletes their objects.
func TestProjectFigures(t *testing.T) {
	e := newMediaEnv(t)
	ada, bob := e.projectUser(t), e.projectUser(t)
	id, path := e.ownProject(t, ada, testenv.Word(t)+" figures")
	e.member(t, id, bob, "viewer")

	jpg, err := os.ReadFile(filepath.Join("..", "..", "internal", "media", "adapter", "imaging", "testdata", "exif.jpg"))
	if err != nil {
		t.Fatal(err)
	}
	code, fig := e.uploadFigure(t, ada, path, "figures/photo.jpg", jpg)
	if code != http.StatusCreated || fig["kind"] != "binary" || fig["contentType"] != "image/jpeg" || fig["scanStatus"] != "pending" {
		t.Fatalf("upload: %d %v", code, fig)
	}
	figPath := path + "/files/" + fig["id"].(string)
	code, body := e.json(t, bob, http.MethodGet, figPath, "")
	wantError(t, "before the scan", code, body, http.StatusConflict, "scan_pending")
	// The object belongs to the project, with its uploader kept for audit.
	if n := e.countRows(t, `SELECT count(*) FROM stored_files sf JOIN project_files f ON f.stored_file_id = sf.id
		WHERE f.id = $1 AND sf.project_id = $2 AND sf.owner_user_id IS NULL AND sf.uploaded_by = $3 AND sf.purpose = 'figure'`,
		fig["id"], id, ada); n != 1 {
		t.Error("the figure's stored file is not the project's")
	}

	eicar := append([]byte("%PDF-1.4\n"), []byte(`X5O!P%@AP[4\PZX54(P^)7CC)7}$EICAR-STANDARD-ANTIVIRUS-TEST-FILE!$H+H*`)...)
	code, bad := e.uploadFigure(t, ada, path, "figures/bad.pdf", eicar)
	if code != http.StatusCreated {
		t.Fatalf("upload eicar: %d %v", code, bad)
	}
	badKey := e.figureKey(t, bad["id"].(string))
	e.scanAll(t, &fakeClamd{})

	code, body = e.json(t, bob, http.MethodGet, figPath, "")
	dl, _ := body["download"].(map[string]any)
	if code != http.StatusOK || dl == nil || body["content"] != nil {
		t.Fatalf("download: %d %v", code, body)
	}
	if exp, _ := time.Parse(time.RFC3339, dl["expiresAt"].(string)); time.Until(exp) > 5*time.Minute+time.Second {
		t.Errorf("expires at %v, want within 5 minutes", exp)
	}
	header, content := e.follow(t, dl["url"].(string))
	if bytes.Contains(content, []byte("SECRET-EXIF-MARKER")) || bytes.Contains(content, []byte("Exif\x00\x00")) {
		t.Errorf("EXIF kept in the stored figure (%d bytes)", len(content))
	}
	// Lossless: the image is not encoded again, so its pixels are exactly the upload's.
	before, err := jpeg.Decode(bytes.NewReader(jpg))
	if err != nil {
		t.Fatal(err)
	}
	after, err := jpeg.Decode(bytes.NewReader(content))
	if err != nil {
		t.Fatalf("stored figure does not decode: %v", err)
	}
	yb, ya := before.(*image.YCbCr), after.(*image.YCbCr)
	if !bytes.Equal(yb.Y, ya.Y) || !bytes.Equal(yb.Cb, ya.Cb) || !bytes.Equal(yb.Cr, ya.Cr) || int64(len(content)) != int64(fig["size"].(float64)) {
		t.Errorf("pixels changed, or size %v is not the stored %d bytes", fig["size"], len(content))
	}
	// The scan section of the file, from the start of scan to the end, is byte-identical.
	if sos := bytes.Index(jpg, []byte{0xFF, 0xDA}); sos < 0 || !bytes.HasSuffix(content, jpg[sos:]) {
		t.Error("the image data was changed")
	}
	if !strings.HasPrefix(header.Get("Content-Disposition"), "attachment") || header.Get("Content-Type") != "image/jpeg" {
		t.Errorf("download headers = %v", header)
	}

	// The scanner found malware in the second one: never served, object gone.
	code, body = e.json(t, bob, http.MethodGet, path+"/files/"+bad["id"].(string), "")
	wantError(t, "infected", code, body, http.StatusGone, "file_removed")
	if e.objectExists(t, badKey) {
		t.Error("the infected figure's object is still in the bucket")
	}
	if _, files, _ := e.tree(t, ada, path); files["figures/bad.pdf"]["scanStatus"] != "infected" {
		t.Errorf("tree shows %v", files["figures/bad.pdf"])
	}

	png, err := os.ReadFile(filepath.Join("..", "..", "internal", "media", "adapter", "imaging", "testdata", "alpha.png"))
	if err != nil {
		t.Fatal(err)
	}
	webp, err := os.ReadFile(filepath.Join("..", "..", "internal", "media", "adapter", "imaging", "testdata", "still.webp"))
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name, path string
		data       []byte
		status     int
		code       string
	}{
		{"webp", "figures/x.png", webp, http.StatusUnsupportedMediaType, "unsupported_type"},
		{"renamed program", "figures/x.pdf", append([]byte("MZ\x90\x00"), make([]byte, 64)...), http.StatusUnsupportedMediaType, "unsupported_type"},
		{"jpeg named .png", "figures/x.png", jpg, http.StatusBadRequest, "type_mismatch"},
		{"png named .pdf", "figures/x.pdf", png, http.StatusBadRequest, "type_mismatch"},
		{"no figure extension", "figures/x.tex", png, http.StatusBadRequest, "invalid_path"},
		{"path taken", "FIGURES/PHOTO.JPG", jpg, http.StatusConflict, "path_taken"},
	} {
		code, body := e.uploadFigure(t, ada, path, tt.path, tt.data)
		wantError(t, tt.name, code, body, tt.status, tt.code)
	}
	if code, body := e.uploadFigure(t, ada, path, "figures/plot.png", png); code != http.StatusCreated || body["contentType"] != "image/png" {
		t.Errorf("png: %d %v", code, body)
	}

	// Deleting the project deletes the figures' objects after the commit.
	keys := []string{e.figureKey(t, fig["id"].(string))}
	_, files, _ := e.tree(t, ada, path)
	keys = append(keys, e.figureKey(t, files["figures/plot.png"]["id"].(string)))
	if code, body := e.json(t, ada, http.MethodDelete, path+"?confirm="+strings.ReplaceAll(testenvTitle(t, e, id), " ", "+"), ""); code != http.StatusNoContent {
		t.Fatalf("delete project: %d %v", code, body)
	}
	for _, k := range keys {
		if e.objectExists(t, k) {
			t.Errorf("object %s outlived its project", k)
		}
	}
	if n := e.countRows(t, `SELECT count(*) FROM stored_files WHERE project_id = $1`, id); n != 0 {
		t.Errorf("%d stored files outlived the project", n)
	}
}

// FR-12.2: the figure and project limits come from the settings; the browser's POST is held
// to the size it announced; only the user who started an upload completes it; an abandoned
// upload expires and its object is queued for deletion.
func TestProjectFigureLimits(t *testing.T) {
	e := newMediaEnvOnPool(t, testenv.DB(t), "", "", envOptions{figures: &figureLimits{figure: 1500, storage: 2500}})
	ada, bob := e.projectUser(t), e.projectUser(t)
	id, path := e.ownProject(t, ada, testenv.Word(t)+" limits")
	e.member(t, id, bob, "editor")
	pdf := func(n int) []byte { return append([]byte("%PDF-1.4\n"), bytes.Repeat([]byte("x"), n-9)...) }

	code, body := e.startUpload(t, ada, path, "big.pdf", 1501)
	if code != http.StatusRequestEntityTooLarge || body["code"] != "file_too_large" || body["limitBytes"] != float64(1500) {
		t.Errorf("over the figure limit: %d %v", code, body)
	}
	if code, body := e.uploadFigure(t, ada, path, "a.pdf", pdf(1200)); code != http.StatusCreated {
		t.Fatalf("a.pdf: %d %v", code, body)
	}
	code, body = e.startUpload(t, bob, path, "b.pdf", 1301)
	if code != http.StatusRequestEntityTooLarge || body["code"] != "storage_limit_reached" ||
		body["usedBytes"] != float64(1200) || body["limitBytes"] != float64(2500) ||
		body["message"] != "This project uses 2 KB of 3 KB, so a figure of 2 KB does not fit. Delete figures it no longer needs to make room." {
		t.Errorf("over the project limit: %d %v", code, body)
	}
	code, tree := e.json(t, bob, http.MethodGet, path+"/files", "")
	if st, _ := tree["storage"].(map[string]any); code != http.StatusOK || st["usedBytes"] != float64(1200) ||
		st["limitBytes"] != float64(2500) || st["figureMaxBytes"] != float64(1500) {
		t.Errorf("tree storage: %d %v", code, tree["storage"])
	}

	// Storage holds the POST to the announced size; until a file arrives there is nothing to
	// complete.
	code, ticket := e.startUpload(t, bob, path, "b.pdf", 100)
	if code != http.StatusCreated || !strings.HasPrefix(ticket["fields"].(map[string]any)["key"].(string), "projects/"+id+"/uploads/") {
		t.Fatalf("start: %d %v", code, ticket)
	}
	if status := postToStorage(t, ticket, pdf(1000)); status != http.StatusBadRequest {
		t.Errorf("POST over the announced size: %d, want 400", status)
	}
	code, body = e.completeUpload(t, bob, path, ticket["id"].(string))
	wantError(t, "nothing arrived", code, body, http.StatusConflict, "upload_incomplete")
	if status := postToStorage(t, ticket, pdf(100)); status != http.StatusNoContent {
		t.Fatalf("POST: %d", status)
	}
	code, body = e.completeUpload(t, ada, path, ticket["id"].(string))
	wantError(t, "another member completes", code, body, http.StatusNotFound, "not_found")
	if code, body := e.completeUpload(t, bob, path, ticket["id"].(string)); code != http.StatusCreated || body["size"] != float64(100) {
		t.Fatalf("complete: %d %v", code, body)
	}
	code, body = e.completeUpload(t, bob, path, ticket["id"].(string))
	wantError(t, "completed twice", code, body, http.StatusNotFound, "not_found")
	uploadKey := ticket["fields"].(map[string]any)["key"].(string)
	if e.objectExists(t, uploadKey) {
		t.Error("the upload's own object outlived its completion")
	}

	// An abandoned upload: its hour passes, the sweep deletes it and queues its object.
	code, ticket = e.startUpload(t, bob, path, "c.pdf", 50)
	if code != http.StatusCreated || postToStorage(t, ticket, pdf(50)) != http.StatusNoContent {
		t.Fatalf("start c.pdf: %d", code)
	}
	if _, err := e.pool.Exec(t.Context(), `UPDATE project_figure_uploads
		SET created_at = now() - interval '2 hours', expires_at = now() - interval '1 hour' WHERE id = $1`, ticket["id"]); err != nil {
		t.Fatal(err)
	}
	if n, err := e.projects.ExpireUploads(t.Context()); err != nil || n != 1 {
		t.Fatalf("ExpireUploads = %d, %v", n, err)
	}
	key := ticket["fields"].(map[string]any)["key"].(string)
	if n := e.countRows(t, `SELECT count(*) FROM storage_deletions WHERE object_key = $1`, key); n != 1 {
		t.Error("the abandoned upload's object was not queued for deletion")
	}
	code, body = e.completeUpload(t, bob, path, ticket["id"].(string))
	wantError(t, "expired", code, body, http.StatusNotFound, "not_found")
}

func testenvTitle(t *testing.T, e mediaEnv, projectID string) string {
	t.Helper()
	var title string
	if err := e.pool.QueryRow(t.Context(), `SELECT title FROM projects WHERE id = $1`, projectID).Scan(&title); err != nil {
		t.Fatal(err)
	}
	return title
}

// FR-10.4 for projects made before slice 21: the backfill adds main.tex to projects without
// files, and running it again changes nothing.
func TestMainTexBackfill(t *testing.T) {
	e := newMediaEnv(t)
	ada := e.projectUser(t)
	e.rename(t, ada, "Ada Lovelace")
	old, oldPath := e.ownProject(t, ada, "Old project")
	kept, keptPath := e.ownProject(t, ada, "Kept project")
	// As before slice 21: no files at all. The other keeps a file of its own only.
	if _, err := e.pool.Exec(t.Context(), `DELETE FROM project_files WHERE project_id = $1`, old); err != nil {
		t.Fatal(err)
	}
	if _, err := e.pool.Exec(t.Context(), `UPDATE project_files SET path = 'paper.tex' WHERE project_id = $1`, kept); err != nil {
		t.Fatal(err)
	}
	n, err := e.projects.BackfillMainFiles(t.Context())
	if err != nil || n != 1 {
		t.Fatalf("backfill = %d, %v; want 1", n, err)
	}
	paths, files, _ := e.tree(t, ada, oldPath)
	if !slices.Equal(paths, []string{"main.tex"}) {
		t.Fatalf("old project: %v", paths)
	}
	_, body := e.json(t, ada, http.MethodGet, oldPath+"/files/"+files["main.tex"]["id"].(string), "")
	if c, _ := body["content"].(string); !strings.Contains(c, `\title{Old project}`) || !strings.Contains(c, `\author{Ada Lovelace}`) {
		t.Errorf("backfilled main.tex:\n%s", c)
	}
	if paths, _, _ := e.tree(t, ada, keptPath); !slices.Equal(paths, []string{"paper.tex"}) {
		t.Errorf("a project with files changed: %v", paths)
	}
	if n, err := e.projects.BackfillMainFiles(t.Context()); err != nil || n != 0 {
		t.Errorf("second run = %d, %v; want 0", n, err)
	}
}
