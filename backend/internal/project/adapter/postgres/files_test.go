//go:build integration

package postgres

import (
	"errors"
	"slices"
	"testing"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/project/domain"
)

func (d db) paths(projectID string) []string {
	d.t.Helper()
	tree, err := d.store.Tree(d.t.Context(), projectID)
	if err != nil {
		d.t.Fatal(err)
	}
	var out []string
	for _, f := range tree.Files {
		out = append(out, f.Path)
	}
	return out
}

func (d db) folder(projectID, path string) {
	d.t.Helper()
	if _, err := d.store.InsertFolder(d.t.Context(), projectID, path, ""); err != nil {
		d.t.Fatalf("folder %s: %v", path, err)
	}
}

func (d db) text(projectID, path string) string {
	d.t.Helper()
	id, err := d.store.InsertText(d.t.Context(), projectID, path, "x", "")
	if err != nil {
		d.t.Fatalf("text %s: %v", path, err)
	}
	return id
}

// '_' is a path character and a LIKE wildcard: moving or deleting folder a_b must not touch
// axb. Moves keep letter case from the new path, and the main file follows its folder.
func TestFileTreeQueries(t *testing.T) {
	d := newDB(t)
	ctx := t.Context()
	owner := d.user("Ada")
	pid := d.project(owner, "Graph learning", "A study")
	d.folder(pid, "a_b")
	d.text(pid, "a_b/x.tex")
	d.folder(pid, "axb")
	d.text(pid, "axb/y.tex")
	d.text(pid, "a_b.tex")
	d.exec("UPDATE projects SET main_file_path = 'a_b/x.tex' WHERE id = $1", pid)

	if err := d.store.Move(ctx, pid, "a_b", "Moved", owner); err != nil {
		t.Fatal(err)
	}
	want := []string{"a_b.tex", "axb", "axb/y.tex", "Moved", "Moved/x.tex"}
	if got := d.paths(pid); !slices.Equal(got, want) {
		t.Errorf("after move = %v, want %v", got, want)
	}
	if tree, _ := d.store.Tree(ctx, pid); tree.MainFilePath != "Moved/x.tex" {
		t.Errorf("main file = %q", tree.MainFilePath)
	}
	keys, err := d.store.ObjectKeysUnder(ctx, pid, "a_b")
	if err != nil || len(keys) != 0 {
		t.Errorf("keys = %v, %v", keys, err)
	}
	// FileAt ignores letter case.
	if f, ok, err := d.store.FileAt(ctx, pid, "moved/X.TEX"); err != nil || !ok || f.Kind != domain.KindText {
		t.Errorf("FileAt = %+v %v %v", f, ok, err)
	}
	// Delete the moved folder only: a_b.tex and axb stay.
	d.folder(pid, "a_b")
	if err := d.store.DeleteTree(ctx, pid, "a_b"); err != nil {
		t.Fatal(err)
	}
	if err := d.store.DeleteTree(ctx, pid, "MOVED"); err != nil {
		t.Fatal(err)
	}
	if got, want := d.paths(pid), []string{"a_b.tex", "axb", "axb/y.tex"}; !slices.Equal(got, want) {
		t.Errorf("after delete = %v, want %v", got, want)
	}
	// The database's rules come back as domain errors.
	if _, err := d.store.InsertText(ctx, pid, "A_B.TEX", "", ""); !errors.Is(err, domain.ErrPathTaken) {
		t.Errorf("taken: %v", err)
	}
	if _, err := d.store.InsertText(ctx, pid, "a_b.tex/z.tex", "", ""); !errors.Is(err, domain.ErrParentNotFolder) {
		t.Errorf("parent: %v", err)
	}
	if _, err := d.store.InsertText(ctx, pid, "nowhere/z.tex", "", ""); !errors.Is(err, domain.ErrParentNotFolder) {
		t.Errorf("no parent: %v", err)
	}
	// A save raises the version and clears the Yjs state.
	id := d.text(pid, "axb/z.tex")
	d.exec("UPDATE project_files SET ydoc_state = '\\x01', ydoc_epoch = gen_random_uuid() WHERE id = $1", id)
	if v, err := d.store.SaveContent(ctx, id, "new", owner); err != nil || v != 2 {
		t.Errorf("save = %d, %v", v, err)
	}
	if n := d.count("SELECT count(*) FROM project_files WHERE id = $1 AND ydoc_state IS NULL AND ydoc_epoch IS NULL AND size_bytes = 3", id); n != 1 {
		t.Error("save did not clear the Yjs state or set the size")
	}
	d.exec(`INSERT INTO project_files (project_id, path, content)
		SELECT $1, 'axb/f' || i || '.tex', '' FROM generate_series(1, 196) AS i`, pid)
	if _, err := d.store.InsertFolder(ctx, pid, "one-more", ""); !errors.Is(err, domain.ErrFileLimit) {
		t.Errorf("limit: %v", err)
	}
	// A shared lock reads the role; a project that does not exist is not found.
	if r, err := d.store.LockShared(ctx, pid, owner); err != nil || r != domain.RoleOwner {
		t.Errorf("LockShared = %q, %v", r, err)
	}
	if _, err := d.store.LockShared(ctx, "00000000-0000-4000-8000-000000000000", owner); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("LockShared missing = %v", err)
	}
}
