package usecase

import (
	"errors"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/project/domain"
)

const (
	fOwner  = "u-owner"
	fEditor = "u-editor"
	fViewer = "u-viewer"
	fStrang = "u-stranger"
)

// project makes an open project owned by fOwner with an editor and a viewer, and a
// main.tex.
func (e *fileEnv) project(t *testing.T) string {
	t.Helper()
	pid := e.store.add(fOwner, domain.StatusOpen)
	e.withMember(pid, fEditor, domain.RoleEditor)
	e.withMember(pid, fViewer, domain.RoleViewer)
	if _, err := e.files.InsertText(t.Context(), pid, MainFileName, "hello", fOwner); err != nil {
		t.Fatal(err)
	}
	return pid
}

func (e *fileEnv) fileAt(t *testing.T, pid, path string) domain.File {
	t.Helper()
	f, ok, err := e.files.FileAt(t.Context(), pid, path)
	if err != nil || !ok {
		t.Fatalf("no file at %q (err %v)", path, err)
	}
	return f
}

func (e *fileEnv) paths(t *testing.T, pid string) []string {
	t.Helper()
	tree, err := e.files.Tree(t.Context(), pid)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, f := range tree.Files {
		out = append(out, f.Path)
	}
	return out
}

func TestCleanPath(t *testing.T) {
	long := strings.Repeat("a", MaxSegmentLength)
	tests := []struct {
		in, want string
		ok       bool
	}{
		{"main.tex", "main.tex", true},
		{"  sections/intro.tex ", "sections/intro.tex", true},
		{"figures/plot_2024-v2.png", "figures/plot_2024-v2.png", true},
		{"a/b/c/d/e/f/g/h.tex", "a/b/c/d/e/f/g/h.tex", true}, // 8 segments
		{long, long, true},
		{"", "", false},
		{"a/b/c/d/e/f/g/h/i.tex", "", false}, // 9 segments
		{long + "a", "", false},
		{strings.Repeat("ab/", 85) + "x", "", false}, // over 255
		{"/main.tex", "", false},
		{"main.tex/", "", false},
		{"a//b.tex", "", false},
		{"../etc/passwd", "", false},
		{"a/../b.tex", "", false},
		{"a/./b.tex", "", false},
		{".hidden", "", false},
		{"-flag.tex", "", false},
		{"my intro.tex", "", false}, // no spaces (LaTeX)
		{`a\b.tex`, "", false},
		{"ré.tex", "", false},
		{"a:b.tex", "", false},
		{"a\x00b.tex", "", false},
	}
	for _, tt := range tests {
		got, err := CleanPath(tt.in)
		if (err == nil) != tt.ok || got != tt.want {
			t.Errorf("CleanPath(%q) = %q, %v; want %q, ok %v", tt.in, got, err, tt.want, tt.ok)
		}
		if err != nil && !errors.Is(err, domain.ErrInvalidPath) {
			t.Errorf("CleanPath(%q) error = %v, want ErrInvalidPath", tt.in, err)
		}
	}
}

func TestLatexEscape(t *testing.T) {
	tests := map[string]string{
		"Graph learning":       "Graph learning",
		"R&D at 100% {fast}":   `R\&D at 100\% \{fast\}`,
		`a\b $x$ #1 ^2 _3 ~4`:  `a\textbackslash{}b \$x\$ \#1 \textasciicircum{}2 \_3 \textasciitilde{}4`,
		"  two\n lines\t ":     "two lines",
		"bidi\u202eoverride":   "bidi override",
		"Nafisa Hasan":         "Nafisa Hasan",
		`\end{document}\input`: `\textbackslash{}end\{document\}\textbackslash{}input`,
	}
	for in, want := range tests {
		if got := latexEscape(in); got != want {
			t.Errorf("latexEscape(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMainTex(t *testing.T) {
	doc := MainTex("Graphs & 50% more", "Ada Lovelace")
	for _, want := range []string{
		`\documentclass{article}`, `\title{Graphs \& 50\% more}`, `\author{Ada Lovelace}`,
		`\begin{document}`, `\maketitle`, `\section{Introduction}`, `\includegraphics[width=0.8\linewidth]{figures/example.png}`, `\end{document}`,
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("main.tex lacks %q:\n%s", want, doc)
		}
	}
	// The figure example is commented out, so the document compiles without a figure.
	for line := range strings.SplitSeq(doc, "\n") {
		if strings.Contains(line, "includegraphics") && !strings.HasPrefix(line, "%") {
			t.Errorf("figure line not commented out: %q", line)
		}
	}
}

// Members read; editors and the owner write; viewers read only; non-members, even of an
// open project they can see, get not found (SRS 2.3, FR-12.7's rule applied to REST).
func TestFileRoles(t *testing.T) {
	e := newFileEnv()
	pid := e.project(t)
	main := e.fileAt(t, pid, MainFileName)
	ctx := t.Context()
	for _, tt := range []struct {
		user      string
		read      error
		write     error
		writeDesc string
	}{
		{fOwner, nil, nil, "owner"},
		{fEditor, nil, nil, "editor"},
		{fViewer, nil, domain.ErrForbidden, "viewer"},
		{fStrang, domain.ErrNotFound, domain.ErrNotFound, "non-member"},
	} {
		t.Run(tt.writeDesc, func(t *testing.T) {
			if _, err := e.service.Files(ctx, tt.user, pid); !errors.Is(err, tt.read) {
				t.Errorf("Files: err = %v, want %v", err, tt.read)
			}
			if _, err := e.service.File(ctx, tt.user, pid, main.ID); !errors.Is(err, tt.read) {
				t.Errorf("File: err = %v, want %v", err, tt.read)
			}
			if _, err := e.service.CreateFile(ctx, tt.user, pid, CreateFileInput{Path: tt.user + ".tex", Kind: domain.KindText}); !errors.Is(err, tt.write) {
				t.Errorf("CreateFile: err = %v, want %v", err, tt.write)
			}
			f, _, _ := e.files.FileAt(ctx, pid, MainFileName)
			if _, err := e.service.SaveContent(ctx, tt.user, pid, main.ID, "x", f.Version); !errors.Is(err, tt.write) {
				t.Errorf("SaveContent: err = %v, want %v", err, tt.write)
			}
			if _, err := e.service.MoveFile(ctx, tt.user, pid, main.ID, MainFileName); !errors.Is(err, tt.write) {
				t.Errorf("MoveFile: err = %v, want %v", err, tt.write)
			}
			if _, err := e.service.UploadFigure(ctx, tt.user, pid, tt.user+".png", []byte("PNG")); !errors.Is(err, tt.write) {
				t.Errorf("UploadFigure: err = %v, want %v", err, tt.write)
			}
		})
	}
	// The viewer's refused writes changed nothing.
	if got := e.paths(t, pid); slices.Contains(got, fViewer+".tex") || slices.Contains(got, fViewer+".png") {
		t.Errorf("tree after refused writes = %v", got)
	}
	// Removing a member takes their access at once: nothing is cached.
	e.withMember(pid, fEditor, domain.RoleNone)
	if _, err := e.service.Files(ctx, fEditor, pid); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("removed member: err = %v, want not found", err)
	}
	if err := e.service.DeleteFile(ctx, fEditor, pid, main.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("removed member delete: err = %v, want not found", err)
	}
}

// A suspended member still reads but cannot write.
func TestFileWritesRefuseSuspended(t *testing.T) {
	e := newFileEnv()
	pid := e.project(t)
	e.actors.suspended[fEditor] = true
	if _, err := e.service.Files(t.Context(), fEditor, pid); err != nil {
		t.Errorf("read: %v", err)
	}
	var susp *domain.SuspendedError
	if _, err := e.service.CreateFile(t.Context(), fEditor, pid, CreateFileInput{Path: "a.tex", Kind: domain.KindText}); !errors.As(err, &susp) {
		t.Errorf("write: err = %v, want suspended", err)
	}
}

func TestCreateFile(t *testing.T) {
	e := newFileEnv()
	pid := e.project(t)
	ctx := t.Context()
	f, err := e.service.CreateFile(ctx, fEditor, pid, CreateFileInput{Path: "sections/part/intro.tex", Kind: domain.KindText, Content: ptr("x")})
	if err != nil {
		t.Fatal(err)
	}
	if f.Path != "sections/part/intro.tex" || f.Kind != domain.KindText || f.UpdatedBy == nil || f.UpdatedBy.UserID != fEditor {
		t.Errorf("created %+v", f)
	}
	// The folders on the way were made.
	if got, want := e.paths(t, pid), []string{"main.tex", "sections", "sections/part", "sections/part/intro.tex"}; !slices.Equal(got, want) {
		t.Errorf("tree = %v, want %v", got, want)
	}

	tests := []struct {
		name string
		in   CreateFileInput
		want error
	}{
		{"same path, other case", CreateFileInput{Path: "MAIN.TEX", Kind: domain.KindText}, domain.ErrPathTaken},
		{"a folder over a file", CreateFileInput{Path: "Sections", Kind: domain.KindFolder}, domain.ErrPathTaken},
		{"through a file", CreateFileInput{Path: "main.tex/x.tex", Kind: domain.KindText}, domain.ErrParentNotFolder},
		{"text with a figure's extension", CreateFileInput{Path: "plot.PNG", Kind: domain.KindText}, domain.ErrInvalidPath},
		{"space", CreateFileInput{Path: "my file.tex", Kind: domain.KindText}, domain.ErrInvalidPath},
		{"folder with content", CreateFileInput{Path: "f", Kind: domain.KindFolder, Content: ptr("")}, &domain.FieldError{Field: "content"}},
		{"binary kind", CreateFileInput{Path: "b.tex", Kind: domain.KindBinary}, &domain.FieldError{Field: "kind"}},
		{"control character", CreateFileInput{Path: "c.tex", Kind: domain.KindText, Content: ptr("a\x07b")}, &domain.FieldError{Field: "content"}},
		{"over 1 MiB", CreateFileInput{Path: "big.tex", Kind: domain.KindText, Content: ptr(strings.Repeat("x", MaxTextBytes+1))}, domain.ErrFileTooLarge},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := e.service.CreateFile(ctx, fEditor, pid, tt.in)
			var fe, want *domain.FieldError
			if errors.As(tt.want, &want) {
				if !errors.As(err, &fe) || fe.Field != want.Field {
					t.Errorf("err = %v, want field %q", err, want.Field)
				}
				return
			}
			if !errors.Is(err, tt.want) {
				t.Errorf("err = %v, want %v", err, tt.want)
			}
		})
	}
	// Exactly 1 MiB, tabs and line breaks are fine.
	if _, err := e.service.CreateFile(ctx, fEditor, pid, CreateFileInput{Path: "max.tex", Kind: domain.KindText,
		Content: ptr(strings.Repeat("x", MaxTextBytes-3) + "\t\r\n")}); err != nil {
		t.Errorf("1 MiB: %v", err)
	}
}

func TestCreateFileLimit(t *testing.T) {
	e := newFileEnv()
	pid := e.project(t) // main.tex is entry 1
	ctx := t.Context()
	for i := 1; i < MaxFiles; i++ {
		if _, err := e.service.CreateFile(ctx, fOwner, pid, CreateFileInput{Path: "f" + strconv.Itoa(i) + ".tex", Kind: domain.KindText}); err != nil {
			t.Fatalf("file %d: %v", i, err)
		}
	}
	if _, err := e.service.CreateFile(ctx, fOwner, pid, CreateFileInput{Path: "last.tex", Kind: domain.KindText}); !errors.Is(err, domain.ErrFileLimit) {
		t.Errorf("201st: err = %v, want file limit", err)
	}
	// A folder counts too.
	if _, err := e.service.CreateFile(ctx, fOwner, pid, CreateFileInput{Path: "dir", Kind: domain.KindFolder}); !errors.Is(err, domain.ErrFileLimit) {
		t.Errorf("201st folder: err = %v, want file limit", err)
	}
}

func TestMoveFolderMovesEverythingInIt(t *testing.T) {
	e := newFileEnv()
	pid := e.project(t)
	ctx := t.Context()
	for _, p := range []string{"src/main2.tex", "src/sub/a.tex"} {
		if _, err := e.service.CreateFile(ctx, fOwner, pid, CreateFileInput{Path: p, Kind: domain.KindText}); err != nil {
			t.Fatal(err)
		}
	}
	// The main file lives in src now, so it follows the folder.
	main := e.fileAt(t, pid, MainFileName)
	if _, err := e.service.MoveFile(ctx, fOwner, pid, main.ID, "src/main.tex"); err != nil {
		t.Fatal(err)
	}
	src := e.fileAt(t, pid, "src")
	moved, err := e.service.MoveFile(ctx, fEditor, pid, src.ID, "paper/latex")
	if err != nil {
		t.Fatal(err)
	}
	if moved.Path != "paper/latex" {
		t.Errorf("moved to %q", moved.Path)
	}
	want := []string{"paper", "paper/latex", "paper/latex/main.tex", "paper/latex/main2.tex", "paper/latex/sub", "paper/latex/sub/a.tex"}
	if got := e.paths(t, pid); !slices.Equal(got, want) {
		t.Errorf("tree = %v, want %v", got, want)
	}
	if tree, _ := e.service.Files(ctx, fOwner, pid); tree.MainFilePath != "paper/latex/main.tex" {
		t.Errorf("main file = %q, want it to follow", tree.MainFilePath)
	}

	latex := e.fileAt(t, pid, "paper/latex")
	for name, tt := range map[string]struct {
		id, path string
		want     error
	}{
		"into itself":          {latex.ID, "paper/latex/sub/latex", domain.ErrInvalidMove},
		"onto a sibling":       {latex.ID, "paper", domain.ErrPathTaken},
		"onto one, other case": {e.fileAt(t, pid, "paper/latex/sub/a.tex").ID, "PAPER/LATEX/MAIN.TEX", domain.ErrPathTaken},
		"text to a figure":     {e.fileAt(t, pid, "paper/latex/main2.tex").ID, "main2.pdf", domain.ErrInvalidPath},
		"unknown file":         {"f999", "x.tex", domain.ErrFileNotFound},
		"bad path":             {latex.ID, "a b", domain.ErrInvalidPath},
	} {
		if _, err := e.service.MoveFile(ctx, fOwner, pid, tt.id, tt.path); !errors.Is(err, tt.want) {
			t.Errorf("%s: err = %v, want %v", name, err, tt.want)
		}
	}
	// A rename that changes only letter case is allowed.
	if f, err := e.service.MoveFile(ctx, fOwner, pid, latex.ID, "paper/LaTeX"); err != nil || f.Path != "paper/LaTeX" {
		t.Errorf("case rename = %+v, %v", f, err)
	}
}

func TestMoveFigureKeepsItsType(t *testing.T) {
	e := newFileEnv()
	pid := e.project(t)
	ctx := t.Context()
	png, err := e.service.UploadFigure(ctx, fEditor, pid, "fig/plot.png", []byte("PNG data"))
	if err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]error{
		"fig/plot.jpg": domain.ErrInvalidPath, "fig/plot.tex": domain.ErrInvalidPath, "fig/plot2.PNG": nil,
	} {
		if _, err := e.service.MoveFile(ctx, fEditor, pid, png.ID, path); !errors.Is(err, want) {
			t.Errorf("move to %s: err = %v, want %v", path, err, want)
		}
	}
}

func TestDeleteFolderDeletesEverythingInIt(t *testing.T) {
	e := newFileEnv()
	pid := e.project(t)
	ctx := t.Context()
	if _, err := e.service.CreateFile(ctx, fOwner, pid, CreateFileInput{Path: "figs/old/a.tex", Kind: domain.KindText}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.service.UploadFigure(ctx, fOwner, pid, "figs/old/p.png", []byte("PNG")); err != nil {
		t.Fatal(err)
	}
	if _, err := e.service.UploadFigure(ctx, fOwner, pid, "keep.pdf", []byte("PDF")); err != nil {
		t.Fatal(err)
	}
	figs := e.fileAt(t, pid, "figs")
	if err := e.service.DeleteFile(ctx, fEditor, pid, figs.ID); err != nil {
		t.Fatal(err)
	}
	if got, want := e.paths(t, pid), []string{"keep.pdf", "main.tex"}; !slices.Equal(got, want) {
		t.Errorf("tree = %v, want %v", got, want)
	}
	// Only the deleted figure's object goes, after the commit.
	if len(e.remover.keys) != 1 || !strings.HasPrefix(e.remover.keys[0], "projects/"+pid+"/files/") {
		t.Errorf("removed objects = %v", e.remover.keys)
	}
	if err := e.service.DeleteFile(ctx, fEditor, pid, figs.ID); !errors.Is(err, domain.ErrFileNotFound) {
		t.Errorf("second delete: err = %v, want not found", err)
	}
}

func TestSaveContentVersions(t *testing.T) {
	e := newFileEnv()
	pid := e.project(t)
	ctx := t.Context()
	main := e.fileAt(t, pid, MainFileName)

	// Tab A saves from version 1.
	f, err := e.service.SaveContent(ctx, fEditor, pid, main.ID, "from A", 1)
	if err != nil || f.Version != 2 {
		t.Fatalf("save A = %+v, %v", f, err)
	}
	// Tab B still has version 1: refused, with what A saved.
	_, err = e.service.SaveContent(ctx, fOwner, pid, main.ID, "from B", 1)
	var conflict *domain.VersionConflictError
	if !errors.As(err, &conflict) || conflict.Version != 2 || conflict.Content != "from A" {
		t.Fatalf("save B: err = %v, want a conflict with A's text", err)
	}
	if got, _ := e.service.File(ctx, fOwner, pid, main.ID); *got.Content != "from A" {
		t.Errorf("content = %q; B overwrote A", *got.Content)
	}
	// B saves again from version 2.
	if f, err := e.service.SaveContent(ctx, fOwner, pid, main.ID, "from B", 2); err != nil || f.Version != 3 {
		t.Errorf("save B again = %+v, %v", f, err)
	}

	folder, _ := e.service.CreateFile(ctx, fOwner, pid, CreateFileInput{Path: "dir", Kind: domain.KindFolder})
	for name, tt := range map[string]struct {
		id, content string
		version     int
		want        error
	}{
		"a folder":          {folder.ID, "x", 1, domain.ErrNotTextFile},
		"over 1 MiB":        {main.ID, strings.Repeat("x", MaxTextBytes+1), 3, domain.ErrFileTooLarge},
		"unknown file":      {"f999", "x", 1, domain.ErrFileNotFound},
		"invalid UTF-8":     {main.ID, "\xff", 3, &domain.FieldError{Field: "content"}},
		"version 0":         {main.ID, "x", 0, &domain.FieldError{Field: "version"}},
		"control character": {main.ID, "\x1b[31m", 3, &domain.FieldError{Field: "content"}},
	} {
		_, err := e.service.SaveContent(ctx, fOwner, pid, tt.id, tt.content, tt.version)
		var fe, want *domain.FieldError
		if errors.As(tt.want, &want) {
			if !errors.As(err, &fe) || fe.Field != want.Field {
				t.Errorf("%s: err = %v, want field %s", name, err, want.Field)
			}
			continue
		}
		if !errors.Is(err, tt.want) {
			t.Errorf("%s: err = %v, want %v", name, err, tt.want)
		}
	}
	// A save holds the project shared, so it waits for a membership change.
	if !slices.Contains(e.files.shared, pid) {
		t.Error("a save did not lock the project shared")
	}
}

func TestSaveContentLimit(t *testing.T) {
	e := newFileEnv()
	pid := e.project(t)
	main := e.fileAt(t, pid, MainFileName)
	e.saves.used = map[string]int{fEditor: SavesPerWindow}
	var limited *domain.RateLimitedError
	if _, err := e.service.SaveContent(t.Context(), fEditor, pid, main.ID, "x", 1); !errors.As(err, &limited) {
		t.Errorf("err = %v, want rate limited", err)
	}
	// Other users are not limited; the limit failing lets the save through.
	e.saves.err = errBoom
	if _, err := e.service.SaveContent(t.Context(), fOwner, pid, main.ID, "x", 1); err != nil {
		t.Errorf("limit down: %v", err)
	}
}

func TestUploadFigure(t *testing.T) {
	e := newFileEnv()
	pid := e.project(t)
	ctx := t.Context()
	f, err := e.service.UploadFigure(ctx, fEditor, pid, "figures/plot.png", []byte("PNG pixels"))
	if err != nil {
		t.Fatal(err)
	}
	if f.Kind != domain.KindBinary || *f.ContentType != TypePNG || *f.ScanStatus != domain.ScanPending || f.Path != "figures/plot.png" {
		t.Errorf("uploaded %+v", f)
	}
	if len(e.objects.objects) != 1 || e.figures.used[fEditor] != 1 {
		t.Errorf("objects %d, counted %d; want 1 and 1", len(e.objects.objects), e.figures.used[fEditor])
	}
	// Not downloadable before the scan says clean.
	if _, err := e.service.File(ctx, fViewer, pid, f.ID); !errors.Is(err, domain.ErrScanPending) {
		t.Errorf("download pending: err = %v", err)
	}

	tests := []struct {
		name, path string
		data       []byte
		want       error
	}{
		{"no figure extension", "figures/plot.tex", []byte("PNG"), domain.ErrInvalidPath},
		{"bad path", "figures/my plot.png", []byte("PNG"), domain.ErrInvalidPath},
		{"over 5 MB", "big.png", append([]byte("PNG"), make([]byte, MaxFigureBytes)...), domain.ErrFileTooLarge},
		{"empty", "empty.png", nil, domain.ErrUnsupportedType},
		{"not a figure", "virus.png", []byte("MZ program"), domain.ErrUnsupportedType},
		{"JPEG named .png", "photo.png", []byte("JPG data"), domain.ErrTypeMismatch},
		{"PDF named .jpg", "doc.jpg", []byte("PDF data"), domain.ErrTypeMismatch},
		{"path taken", "FIGURES/PLOT.PNG", []byte("PNG"), domain.ErrPathTaken},
		{"through a file", "main.tex/p.png", []byte("PNG"), domain.ErrParentNotFolder},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := e.service.UploadFigure(ctx, fEditor, pid, tt.path, tt.data); !errors.Is(err, tt.want) {
				t.Errorf("err = %v, want %v", err, tt.want)
			}
		})
	}
	// Refused files are not counted, and a figure refused after it was stored leaves no
	// object behind.
	if e.figures.used[fEditor] != 1 {
		t.Errorf("counted %d uploads, want 1", e.figures.used[fEditor])
	}
	if len(e.objects.objects) != 1 {
		t.Errorf("%d objects left, want 1", len(e.objects.objects))
	}
	// .jpeg and .jpg both name a JPEG; a PDF is kept as uploaded.
	for _, p := range []string{"a.jpeg", "b.JPG"} {
		if _, err := e.service.UploadFigure(ctx, fEditor, pid, p, []byte("JPG")); err != nil {
			t.Errorf("%s: %v", p, err)
		}
	}
	if _, err := e.service.UploadFigure(ctx, fEditor, pid, "c.pdf", []byte("PDF")); err != nil {
		t.Errorf("pdf: %v", err)
	}
}

func TestUploadFigureLimits(t *testing.T) {
	e := newFileEnv()
	pid := e.project(t)
	e.figures.used = map[string]int{fEditor: FiguresPerWindow}
	var limited *domain.RateLimitedError
	if _, err := e.service.UploadFigure(t.Context(), fEditor, pid, "a.png", []byte("PNG")); !errors.As(err, &limited) {
		t.Errorf("err = %v, want rate limited", err)
	}
	// An image that grows past 5 MB when encoded again is refused.
	e.service.d.Figures = memIntake{grow: MaxFigureBytes}
	if _, err := e.service.UploadFigure(t.Context(), fOwner, pid, "b.png", []byte("PNG")); !errors.Is(err, domain.ErrFileTooLarge) {
		t.Errorf("grown: err = %v, want too large", err)
	}
}

func TestFigureDownload(t *testing.T) {
	e := newFileEnv()
	pid := e.project(t)
	ctx := t.Context()
	f, err := e.service.UploadFigure(ctx, fEditor, pid, "figures/plot.png", []byte("PNG"))
	if err != nil {
		t.Fatal(err)
	}
	set := func(st domain.ScanStatus) { e.files.files[f.ID].ScanStatus = &st }
	for st, want := range map[domain.ScanStatus]error{
		domain.ScanPending: domain.ErrScanPending, domain.ScanFailed: domain.ErrScanFailed,
		domain.ScanInfected: domain.ErrFileRemoved, domain.ScanClean: nil,
	} {
		set(st)
		got, err := e.service.File(ctx, fViewer, pid, f.ID)
		if !errors.Is(err, want) {
			t.Errorf("%s: err = %v, want %v", st, err, want)
		}
		if err == nil {
			q := got.URL.Query()
			if q.Get("name") != "plot.png" || q.Get("type") != TypePNG || q.Get("expiry") != FigureDownloadExpiry.String() || got.Content != nil {
				t.Errorf("download = %v", got.URL)
			}
		}
	}
}

// FR-10.4: a new project starts with main.tex, written in the project's transaction, with
// its title and its owner's name.
func TestCreateProjectAddsMainTex(t *testing.T) {
	e := newFileEnv()
	e.files.names[fOwner] = "Ada & Co"
	p, err := e.service.Create(t.Context(), fOwner, CreateInput{Title: "Graphs_100%", Description: "A study"})
	if err != nil {
		t.Fatal(err)
	}
	main, ok, _ := e.files.FileAt(t.Context(), p.ID, MainFileName)
	if !ok {
		t.Fatal("no main.tex")
	}
	got, _ := e.files.File(t.Context(), p.ID, main.ID)
	if !strings.Contains(*got.Content, `\title{Graphs\_100\%}`) || !strings.Contains(*got.Content, `\author{Ada \& Co}`) {
		t.Errorf("main.tex:\n%s", *got.Content)
	}
	// A failed transaction leaves neither.
	e.store.failTx = errBoom
	if _, err := e.service.Create(t.Context(), fOwner, CreateInput{Title: "Second", Description: "x"}); !errors.Is(err, errBoom) {
		t.Errorf("err = %v", err)
	}
}

func TestDeleteProjectRemovesFigureObjects(t *testing.T) {
	e := newFileEnv()
	pid := e.project(t)
	if _, err := e.service.UploadFigure(t.Context(), fOwner, pid, "p.png", []byte("PNG")); err != nil {
		t.Fatal(err)
	}
	if err := e.service.Delete(t.Context(), fOwner, pid, ptr("Graph learning")); err != nil {
		t.Fatal(err)
	}
	if len(e.remover.keys) != 1 {
		t.Errorf("objects removed after the delete = %v, want the figure's", e.remover.keys)
	}
}

// The backfill gives main.tex to projects without files, once.
func TestBackfillMainFiles(t *testing.T) {
	e := newFileEnv()
	ctx := t.Context()
	var empty []string
	for range BackfillBatch + 5 { // more than one batch
		empty = append(empty, e.store.add(fOwner, domain.StatusOpen))
	}
	withFiles := e.project(t) // has main.tex already
	if _, err := e.files.InsertFolder(ctx, withFiles, "extra", fOwner); err != nil {
		t.Fatal(err)
	}
	e.files.names[fOwner] = "Ada"

	n, err := e.service.BackfillMainFiles(ctx)
	if err != nil || n != len(empty) {
		t.Fatalf("backfill = %d, %v; want %d", n, err, len(empty))
	}
	for _, id := range empty {
		if c, _ := e.files.FileCount(ctx, id); c != 1 {
			t.Fatalf("project %s has %d files", id, c)
		}
	}
	if got := e.paths(t, withFiles); !slices.Equal(got, []string{"extra", "main.tex"}) {
		t.Errorf("a project with files changed: %v", got)
	}
	// Idempotent.
	if n, err := e.service.BackfillMainFiles(ctx); err != nil || n != 0 {
		t.Errorf("second run = %d, %v; want 0", n, err)
	}
}
