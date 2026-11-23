package usecase

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"io"
	"slices"
	"testing"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/project/domain"
)

// SRS 2.3, "Compile, use the assistant, edit the abstract": editors and the owner; viewers
// read; a non-member, an administrator included, sees nothing; a suspended account cannot
// compile.
func TestAuthorizeCompile(t *testing.T) {
	e := newFileEnv()
	pid := e.project(t)
	tests := []struct {
		user   string
		action domain.Action
		want   error
	}{
		{fOwner, domain.ActCompile, nil},
		{fEditor, domain.ActCompile, nil},
		{fViewer, domain.ActCompile, domain.ErrForbidden},
		{fViewer, domain.ActReadFiles, nil},
		{fStrang, domain.ActReadFiles, domain.ErrNotFound},
	}
	for _, tt := range tests {
		if err := e.service.Authorize(t.Context(), tt.user, pid, tt.action); !errors.Is(err, tt.want) {
			t.Errorf("%s %s: err = %v, want %v", tt.user, tt.action, err, tt.want)
		}
	}
	e.actors.suspended[fEditor] = true
	var s *domain.SuspendedError
	if err := e.service.Authorize(t.Context(), fEditor, pid, domain.ActCompile); !errors.As(err, &s) {
		t.Errorf("suspended editor: err = %v", err)
	}
}

type memFlusher struct {
	calls int
	ok    bool
	err   error
	// store runs when a flush is confirmed, as the sync server's store would.
	store func()
}

func (f *memFlusher) FlushLive(context.Context, string, time.Duration) (bool, error) {
	f.calls++
	if f.ok && f.store != nil {
		f.store()
	}
	return f.ok, f.err
}

// A compile reads live edits: when a text file is open on the sync server it asks for a
// flush and reads the text again; when nobody confirms, it says so.
func TestCompileSourceFlushesLiveText(t *testing.T) {
	ce := newCollabEnv()
	pid := ce.project(t)
	main := ce.fileAt(t, pid, MainFileName)
	flusher := &memFlusher{ok: true}
	d := ce.service.d
	d.Flusher = flusher
	ce.service = New(d)

	src, err := ce.service.CompileSource(t.Context(), pid)
	if err != nil || !src.LiveConfirmed || flusher.calls != 0 || src.MainFilePath != MainFileName {
		t.Fatalf("nothing live: %+v %v, flushes %d", src, err, flusher.calls)
	}

	ce.live.live[main.ID] = true
	flusher.store = func() { _, _ = ce.files.SaveContent(t.Context(), main.ID, "live text", fEditor) }
	src, err = ce.service.CompileSource(t.Context(), pid)
	if err != nil || !src.LiveConfirmed || flusher.calls != 1 {
		t.Fatalf("live: %+v %v, flushes %d", src, err, flusher.calls)
	}
	if got := *src.Files[0].Content; got != "live text" {
		t.Errorf("compiled text = %q, want the flushed live text", got)
	}

	flusher.ok = false
	if src, _ = ce.service.CompileSource(t.Context(), pid); src.LiveConfirmed {
		t.Error("an unconfirmed flush reported as confirmed")
	}
	ce.live.err = errors.New("redis down")
	flusher.ok, flusher.err = false, errors.New("redis down")
	if src, err = ce.service.CompileSource(t.Context(), pid); err != nil || src.LiveConfirmed {
		t.Errorf("Redis down: %+v %v", src.LiveConfirmed, err)
	}
	if _, err := ce.service.CompileSource(t.Context(), "nope"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("unknown project: %v", err)
	}
}

// FR-13.7: members download the files; figures only when clean.
func TestArchive(t *testing.T) {
	e := newFileEnv()
	pid := e.project(t)
	if _, err := e.service.CreateFile(t.Context(), fOwner, pid, CreateFileInput{Path: "chapters/intro.tex", Kind: "text", Content: ptr("intro")}); err != nil {
		t.Fatal(err)
	}
	png := []byte("PNG data")
	clean, err := e.upload(t.Context(), fOwner, pid, "figs/clean.png", png)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.upload(t.Context(), fOwner, pid, "figs/pending.png", png); err != nil {
		t.Fatal(err)
	}
	st := domain.ScanClean
	e.files.mu.Lock()
	e.files.files[clean.ID].ScanStatus = &st
	e.files.mu.Unlock()

	if _, err := e.service.PrepareArchive(t.Context(), fStrang, pid); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("non-member: %v", err)
	}
	a, err := e.service.PrepareArchive(t.Context(), fViewer, pid)
	if err != nil {
		t.Fatal(err)
	}
	if a.Name != "Graph learning.zip" || a.Skipped != 1 {
		t.Errorf("archive %q, skipped %d", a.Name, a.Skipped)
	}
	var buf bytes.Buffer
	if err := e.service.WriteArchive(t.Context(), a, &buf); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	var names []string
	for _, f := range zr.File {
		r, _ := f.Open()
		b, _ := io.ReadAll(r)
		got[f.Name] = string(b)
		names = append(names, f.Name)
	}
	want := []string{"chapters/", "chapters/intro.tex", "figs/", "figs/clean.png", "main.tex"}
	if !slices.Equal(names, want) {
		t.Errorf("entries = %v, want %v", names, want)
	}
	if got["main.tex"] != "hello" || got["chapters/intro.tex"] != "intro" || got["figs/clean.png"] == "" {
		t.Errorf("contents = %v", got)
	}

	d := e.service.d
	d.ArchiveMaxBytes = 5
	e.service = New(d)
	if _, err := e.service.PrepareArchive(t.Context(), fOwner, pid); !errors.Is(err, domain.ErrArchiveTooLarge) {
		t.Errorf("over the limit: %v", err)
	}
}

func TestArchiveRateLimit(t *testing.T) {
	e := newFileEnv()
	pid := e.project(t)
	lim := &memLimiter{}
	d := e.service.d
	d.ArchiveLimiter = lim
	e.service = New(d)
	for range ArchivesPerWindow {
		if _, err := e.service.PrepareArchive(t.Context(), fOwner, pid); err != nil {
			t.Fatal(err)
		}
	}
	var limited *domain.RateLimitedError
	if _, err := e.service.PrepareArchive(t.Context(), fOwner, pid); !errors.As(err, &limited) {
		t.Errorf("11th archive: %v", err)
	}
}

func TestArchiveName(t *testing.T) {
	for in, want := range map[string]string{
		"Graph learning":         "Graph learning",
		"  a/b\\c:d  ":           "a_b_c_d",
		"বাংলা শিরোনাম":          "বাংলা শিরোনাম",
		"line\nbreak\tand  more": "line break and more",
		"...":                    "project",
	} {
		if got := ArchiveName(in); got != want {
			t.Errorf("ArchiveName(%q) = %q, want %q", in, got, want)
		}
	}
}

// FR-13.1: the owner chooses the main file, an existing .tex text file, stored with its own
// spelling.
func TestSetMainFile(t *testing.T) {
	e := newFileEnv()
	pid := e.project(t)
	if _, err := e.service.CreateFile(t.Context(), fOwner, pid, CreateFileInput{Path: "paper/Thesis.tex", Kind: "text", Content: ptr("x")}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.service.CreateFile(t.Context(), fOwner, pid, CreateFileInput{Path: "notes.txt", Kind: "text", Content: ptr("x")}); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"missing.tex", "notes.txt", "paper", "../x.tex"} {
		if _, err := e.service.Update(t.Context(), fOwner, pid, UpdateInput{MainFilePath: ptr(p)}); !errors.Is(err, domain.ErrMainFileInvalid) {
			t.Errorf("%q: err = %v", p, err)
		}
	}
	if _, err := e.service.Update(t.Context(), fEditor, pid, UpdateInput{MainFilePath: ptr("paper/thesis.tex")}); !errors.Is(err, domain.ErrForbidden) {
		t.Errorf("editor: err = %v", err)
	}
	if _, err := e.service.Update(t.Context(), fOwner, pid, UpdateInput{MainFilePath: ptr("paper/thesis.tex")}); err != nil {
		t.Fatal(err)
	}
	if got, _ := e.files.MainFile(t.Context(), pid); got != "paper/Thesis.tex" {
		t.Errorf("main file = %q", got)
	}
}
