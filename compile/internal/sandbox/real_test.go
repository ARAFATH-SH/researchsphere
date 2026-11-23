//go:build sandbox

package sandbox

// Tests against the real Docker daemon and the real sandbox image (make
// test-compile-sandbox): NFR-4 and FR-13.5 as the container enforces them, not as the spec
// says. COMPILE_IMAGE names the image (default researchsphere-compile:dev).

import (
	"archive/tar"
	"bytes"
	"context"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ARAFATH-SH/researchsphere/compile/internal/docker"
)

func realRunner(t *testing.T, cfg Config) (*Runner, *docker.Client) {
	t.Helper()
	dc, err := docker.New(os.Getenv("COMPILE_DOCKER_HOST"))
	if err != nil {
		t.Fatal(err)
	}
	if err := dc.Ping(t.Context()); err != nil {
		t.Fatalf("Docker does not answer: %v", err)
	}
	if cfg.Image == "" {
		cfg.Image = os.Getenv("COMPILE_IMAGE")
	}
	if cfg.Image == "" {
		cfg.Image = "researchsphere-compile:dev"
	}
	if cfg.MaxPDFBytes == 0 {
		cfg.MaxPDFBytes = 100 << 20
	}
	if ok, err := dc.ImageExists(t.Context(), cfg.Image); err != nil || !ok {
		t.Fatalf("image %s missing (make compile-image): %v", cfg.Image, err)
	}
	t.Cleanup(func() { noContainersLeft(t, dc) })
	return New(FromClient(dc), cfg, nil), dc
}

// noContainersLeft: every compile container is removed after its run (NFR-4).
func noContainersLeft(t *testing.T, dc *docker.Client) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	list, err := dc.List(ctx, Label, "1")
	if err != nil {
		t.Errorf("list containers: %v", err)
		return
	}
	if len(list) > 0 {
		t.Errorf("%d compile containers left behind", len(list))
		for _, c := range list {
			_ = dc.Remove(ctx, c.ID)
		}
	}
}

func project(t *testing.T, files map[string]string) io.Reader {
	t.Helper()
	var b bytes.Buffer
	tw := tar.NewWriter(&b)
	dirs := map[string]bool{}
	for name, content := range files {
		if d := filepath.Dir(name); d != "." && !dirs[d] {
			dirs[d] = true
			_ = tw.WriteHeader(&tar.Header{Typeflag: tar.TypeDir, Name: d + "/", Mode: 0o755})
		}
		_ = tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: name, Size: int64(len(content)), Mode: 0o644})
		_, _ = io.WriteString(tw, content)
	}
	_ = tw.Close()
	return &b
}

func run(t *testing.T, r *Runner, files map[string]string) Result {
	t.Helper()
	res, err := r.Run(t.Context(), Job{ID: strings.ReplaceAll(strings.ToLower(t.Name()), "/", "-"), MainFile: "main.tex", Input: project(t, files)})
	if err != nil {
		t.Fatal(err)
	}
	if res.PDF != nil {
		t.Cleanup(func() {
			_ = res.PDF.Close()
			_ = os.Remove(res.PDF.Name())
		})
	}
	return res
}

// The multi-file sample with a BibTeX run (FR-13.1, FR-13.2); its logs become the backend's
// parser fixture when RS_WRITE_FIXTURES names the folder.
func TestSandboxMultiFileBibTeX(t *testing.T) {
	r, _ := realRunner(t, Config{})
	res := run(t, r, map[string]string{
		"main.tex": `\documentclass{article}
\begin{document}
\section{Start}
See \cite{knuth84} and \cite{lamport94}.
\input{chapters/intro}
\input{chapters/method}
\bibliographystyle{plain}
\bibliography{refs}
\end{document}
`,
		"chapters/intro.tex": `\section{Introduction}
Some text.

An \undefinedmacro here.
`,
		"chapters/method.tex": `\section{Method}
$x^2$ and a missing reference~\ref{nowhere}.
`,
		"refs.bib": `@book{knuth84,
  author = {Donald E. Knuth},
  title = {The {\TeX}book},
  publisher = {Addison-Wesley},
  year = 1984
}

@book{lamport94,
  author = {Leslie Lamport},
  title = {{\LaTeX}: A Document Preparation System},
  publisher = {Addison-Wesley},
  year = 1994,
  year = 1995
}
`,
	})
	if res.PDF == nil || res.TimedOut || res.Failure != "" {
		t.Fatalf("no PDF: %+v\nstderr: %s\nlog tail: %s", res, res.Stderr, tail(res.Log))
	}
	head := make([]byte, 5)
	_, _ = res.PDF.ReadAt(head, 0)
	if string(head) != "%PDF-" {
		t.Errorf("not a PDF: %q", head)
	}
	if !strings.Contains(res.Log, "chapters/intro.tex:4: Undefined control sequence") {
		t.Errorf("the error is not reported as file:line:\n%s", tail(res.Log))
	}
	if !strings.Contains(res.BibtexLog, "refs.bib") {
		t.Errorf("BibTeX did not run or reported nothing:\n%s", res.BibtexLog)
	}
	if strings.Contains(res.Log, "Citation `knuth84' on page 1 undefined") {
		t.Errorf("the citation is undefined after the BibTeX run:\n%s", tail(res.Log))
	}
	if dir := os.Getenv("RS_WRITE_FIXTURES"); dir != "" {
		_ = os.WriteFile(filepath.Join(dir, "multifile.log"), []byte(res.Log), 0o600)       //nolint:gosec // a folder the developer names for the fixture
		_ = os.WriteFile(filepath.Join(dir, "multifile.blg"), []byte(res.BibtexLog), 0o600) //nolint:gosec // a folder the developer names for the fixture
	}
}

// hostMarkers are strings only the host has: its name and its user's, which a read that left
// the container would show.
func hostMarkers(t *testing.T) []string {
	t.Helper()
	var out []string
	if h, err := os.Hostname(); err == nil && len(h) > 3 {
		out = append(out, h)
	}
	if u, err := user.Current(); err == nil && len(u.Username) > 3 && u.Username != "root" {
		out = append(out, u.Username+":x:")
	}
	for _, k := range []string{"COMPILE_SERVICE_TOKEN", "SYNC_SERVICE_TOKEN", "JWT_SECRET"} {
		if v := os.Getenv(k); len(v) > 8 {
			out = append(out, v)
		}
	}
	if len(out) == 0 {
		t.Fatal("no host marker to look for")
	}
	return out
}

func tail(s string) string {
	if len(s) > 3000 {
		return s[len(s)-3000:]
	}
	return s
}

// NFR-4: no shell escape, no network, no file outside the project read or written. Each
// attempt runs in a document of its own, so a refusal that stops pdfLaTeX does not hide the
// next attempt; none may succeed, and the refusal must be in the log.
func TestSandboxEscapesFail(t *testing.T) {
	r, _ := realRunner(t, Config{})
	// dump writes every line of a file into the log, so the test sees what a read got.
	dump := func(file string) string {
		return `\newread\r\openin\r=` + file + ` \loop\unless\ifeof\r\read\r to\l\typeout{CONTAINER-FILE: \detokenize\expandafter{\l}}\repeat\closein\r` + "\n"
	}
	doc := func(body string) map[string]string {
		return map[string]string{"main.tex": "\\documentclass{article}\n\\begin{document}\n" + body + "\nok\n\\end{document}\n"}
	}
	tests := []struct {
		name    string
		files   map[string]string
		refusal string // in the log or stderr
		bad     []string
	}{
		{"shell escape", doc(`\immediate\write18{curl -s http://example.com -o pwned.txt}\IfFileExists{pwned.txt}{\typeout{ESCAPED}}{}`),
			"runsystem(curl -s http://example.com -o pwned.txt)...disabled", []string{"ESCAPED"}},
		{"piped input", doc(`\input{|"curl -s http://example.com"}`), "", []string{"<html", "Example Domain"}},
		// pdfTeX reads absolute paths whatever openin_any says (docs/compile.md); what it can
		// read is the container's: the image's files, never the host's or another project's.
		{"absolute read stays in the container", doc(dump("/etc/passwd") + dump("/etc/hostname") + dump("../../etc/hosts")), "CONTAINER-FILE", hostMarkers(t)},
		{"absolute write", doc(`\newwrite\o\immediate\openout\o=/tmp/escape.tex\immediate\closeout\o`), "openout_any = p", nil},
		{"parent write", doc(`\newwrite\o\immediate\openout\o=../escape.tex\immediate\closeout\o`), "openout_any = p", nil},
		{"pdfTeX image of an absolute path", doc(`\pdfximage{/etc/passwd}`), "", []string{"root:x:0"}},
		// latexmk reads a file named latexmkrc in the working folder as Perl; -norc must keep
		// it unread.
		{"project latexmkrc", map[string]string{
			"main.tex":  "\\documentclass{article}\\begin{document}ok\\end{document}\n",
			"latexmkrc": `die "LATEXMKRC-WAS-READ";`,
		}, "", []string{"LATEXMKRC-WAS-READ"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := run(t, r, tt.files)
			all := res.Log + res.Stderr + res.BibtexLog
			for _, bad := range tt.bad {
				if strings.Contains(all, bad) {
					t.Errorf("escape succeeded: %q in the output", bad)
				}
			}
			if tt.refusal != "" && !strings.Contains(all, tt.refusal) {
				t.Errorf("the refusal %q is not in the log:\n%s", tt.refusal, tail(all))
			}
			if res.TimedOut {
				t.Error("timed out")
			}
			if tt.name == "project latexmkrc" && res.PDF == nil {
				t.Errorf("no PDF: %s", tail(all))
			}
		})
	}
}

// NFR-4: the container has no network at all, whatever runs in it.
func TestSandboxHasNoNetwork(t *testing.T) {
	r, dc := realRunner(t, Config{})
	spec := r.Spec(Job{ID: "net-probe", MainFile: "main.tex"})
	spec.Entrypoint = []string{"/bin/sh", "-c", "ls /sys/class/net; (exec 3<>/dev/tcp/1.1.1.1/80) 2>&1 || echo NO-ROUTE; getent hosts example.com || echo NO-DNS"}
	spec.Cmd = nil
	spec.OpenStdin, spec.StdinOnce, spec.AttachStdin = false, false, false
	ctx := t.Context()
	id, err := dc.Create(ctx, "rs-compile-net-probe", spec)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = dc.Remove(context.Background(), id) }()
	a, err := dc.Attach(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = a.Close() }()
	if err := dc.Start(ctx, id); err != nil {
		t.Fatal(err)
	}
	var out, errb bytes.Buffer
	done := make(chan error, 1)
	go func() { done <- a.Demux(&out, &errb) }()
	if _, err := dc.Wait(ctx, id); err != nil {
		t.Fatal(err)
	}
	<-done
	got := out.String() + errb.String()
	if strings.TrimSpace(strings.Split(got, "\n")[0]) != "lo" || !strings.Contains(got, "NO-DNS") {
		t.Errorf("network probe:\n%s", got)
	}
	if strings.Contains(got, "eth0") {
		t.Errorf("the container has a network interface:\n%s", got)
	}
}

// FR-13.5: a compile that never ends is stopped, by the entrypoint's limit (the log still
// comes out) and, failing that, by the hard kill; the container is removed either way.
func TestSandboxTimeouts(t *testing.T) {
	loop := map[string]string{"main.tex": "\\documentclass{article}\\begin{document}\\loop\\iftrue\\repeat\\end{document}\n"}
	t.Run("inner limit", func(t *testing.T) {
		r, _ := realRunner(t, Config{Hard: 30 * time.Second, Inner: 5 * time.Second})
		start := time.Now()
		res := run(t, r, loop)
		if !res.TimedOut || res.PDF != nil || time.Since(start) > 25*time.Second {
			t.Errorf("result %+v after %s", res, time.Since(start))
		}
	})
	t.Run("hard kill", func(t *testing.T) {
		r, _ := realRunner(t, Config{Hard: 5 * time.Second, Inner: 50 * time.Second})
		start := time.Now()
		res := run(t, r, loop)
		if !res.TimedOut || time.Since(start) > 20*time.Second {
			t.Errorf("result %+v after %s", res, time.Since(start))
		}
	})
}

// Output caps: a PDF over the limit is refused; a document that writes without end stops at
// the memory-backed /work and the container still goes.
func TestSandboxOutputCaps(t *testing.T) {
	t.Run("PDF over the limit", func(t *testing.T) {
		r, _ := realRunner(t, Config{MaxPDFBytes: 1000})
		res := run(t, r, map[string]string{"main.tex": "\\documentclass{article}\\begin{document}Hello\\end{document}\n"})
		if res.Failure != FailOutputTooLarge || res.PDF != nil {
			t.Errorf("result %+v", res)
		}
	})
	t.Run("write bomb", func(t *testing.T) {
		r, _ := realRunner(t, Config{Hard: 50 * time.Second, Inner: 40 * time.Second})
		res := run(t, r, map[string]string{"main.tex": `\documentclass{article}
\newwrite\big
\immediate\openout\big=big.txt
\def\line{xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx}
\begin{document}
\loop\immediate\write\big{\line\line\line\line\line\line\line\line\line\line}\iftrue\repeat
\end{document}
`})
		if res.PDF != nil {
			t.Error("a write bomb made a PDF")
		}
		// It either fills /work (pdfLaTeX stops: no room) or runs into the time limit; either
		// way it ends as a known outcome, never as an unexplained success.
		if !res.TimedOut && res.Failure == "" && !strings.Contains(res.Log, "I can't write on file") {
			t.Errorf("write bomb ended as %+v; log: %s", res, tail(res.Log))
		}
	})
}
