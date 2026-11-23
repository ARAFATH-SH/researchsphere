//go:build sandbox

package sandbox

// NFR-4 against the real image: a compile reads and writes only its own files. TeX Live 2026
// made openin_any a no-op, so this is rs-landlock's work (docs/decisions.md), with the
// minimal /etc of the image as a second layer. Every case looks for the forbidden text in
// the log, BibTeX's log, the sandbox's stderr and the text of the PDF; a positive control
// proves that the same command and the same search do find a file of the project.

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"regexp"
	"strings"
	"testing"

	"github.com/ARAFATH-SH/researchsphere/compile/internal/docker"
)

// insideMarker is the text of the project's own file inside.txt.
const insideMarker = "RS-INSIDE-WORK"

// readCommands are every way a document reads a file by name. %s is the path.
var readCommands = []struct{ name, tex string }{
	{"input", `\input{%s}`},
	{"openin", `\newread\rsr\openin\rsr=%s \loop\unless\ifeof\rsr\read\rsr to\rsl\typeout{READ:\detokenize\expandafter{\rsl}}\repeat\closein\rsr`},
	{"includegraphics", `\includegraphics[type=png,ext=,read=,width=1cm,height=1cm]{%s}`},
	{"pdfximage", `\pdfximage{%s}\pdfrefximage\pdflastximage`},
	{"lstinputlisting", `\lstinputlisting{%s}`},
	{"verbatiminput", `\verbatiminput{%s}`},
	{"pdffiledump", `\typeout{DUMP:\pdffiledump length 4096{%s}}`},
	{"pdfmdfivesum", `\typeout{MD5:\pdfmdfivesum file{%s}}`},
	{"IfFileExists", `\IfFileExists{%s}{\typeout{RS-EXISTS-YES}}{\typeout{RS-EXISTS-NO}}`},
}

// outsideTargets are files outside the project, by absolute and by parent paths.
var outsideTargets = []string{
	"/etc/passwd",
	"/proc/self/environ",
	"../../etc/passwd",
	"../etc/passwd",
	"/usr/local/texlive/../../../etc/passwd",
}

func readDoc(cmd, target string) map[string]string {
	return map[string]string{
		"main.tex": "\\documentclass{article}\n\\usepackage{graphicx,listings,verbatim}\n\\begin{document}\nstart\n" +
			fmt.Sprintf(cmd, target) + "\nok\n\\end{document}\n",
		"inside.txt": insideMarker + "\n",
	}
}

// forbidden is the text of /etc/passwd (the image's or a host's) and of the environment, as
// typeset text, as letters only and as \pdffiledump's hex. Whether a file exists is not in
// it: Landlock governs opening, not stat, so \IfFileExists and \pdffilesize still see the
// image's own files (docs/compile.md); their content is what must never come out.
func forbidden(t *testing.T) []string {
	plain := []string{"root:x:0:0", "compile:x:10001", "RS_TIMEOUT=", "RS_MAX_PDF_BYTES=", "PATH=/usr", "TEXMFOUTPUT=/work"}
	plain = append(plain, hostMarkers(t)...)
	out := append([]string{}, plain...)
	for _, p := range plain {
		out = append(out, strings.ToUpper(hex.EncodeToString([]byte(p))), alnum(p))
	}
	return out
}

// alnum keeps letters and digits only: listings and verbatim typeset - _ = : with glyphs of
// their own codes, so the PDF's text is compared in this form too.
func alnum(s string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, s)
}

// md5Leak is an MD5 of a file's content, which \pdfmdfivesum prints only for a file it read.
var md5Leak = regexp.MustCompile(`MD5:[0-9A-F]{32}`)

// everything is all the text a compile shows: logs, stderr and the PDF's text.
func everything(t *testing.T, res Result) string {
	t.Helper()
	all := res.Log + "\n" + res.BibtexLog + "\n" + res.Stderr
	if res.PDF != nil {
		if _, err := res.PDF.Seek(0, io.SeekStart); err != nil {
			t.Fatal(err)
		}
		b, err := io.ReadAll(res.PDF)
		if err != nil {
			t.Fatal(err)
		}
		text := pdfText(b)
		all += "\nPDF TEXT: " + text + "\nPDF LETTERS: " + alnum(text)
	}
	return all
}

var (
	streamRe = regexp.MustCompile(`(?s)stream\r?\n(.*?)\r?\nendstream`)
	strRe    = regexp.MustCompile(`\((?:\\.|[^\\)])*\)`)
)

// pdfText is the text of a pdfTeX PDF: every string shown in an inflated content stream,
// joined. Joining strings that are apart only finds more, never less.
func pdfText(pdf []byte) string {
	var b strings.Builder
	for _, m := range streamRe.FindAllSubmatch(pdf, -1) {
		zr, err := zlib.NewReader(bytes.NewReader(m[1]))
		if err != nil {
			continue
		}
		raw, err := io.ReadAll(zr)
		_ = zr.Close()
		// Only page content: text is shown between BT and ET; fonts and images are skipped.
		if err != nil || !bytes.Contains(raw, []byte("BT")) || !bytes.Contains(raw, []byte(" Tf")) {
			continue
		}
		for _, s := range strRe.FindAll(raw, -1) {
			for _, c := range unescapePDF(s[1 : len(s)-1]) {
				if c >= ' ' && c <= '~' {
					b.WriteRune(c)
				} else {
					b.WriteByte('?')
				}
			}
		}
		b.WriteByte('\n')
	}
	return b.String()
}

func unescapePDF(s []byte) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' || i+1 == len(s) {
			b.WriteByte(s[i])
			continue
		}
		i++
		switch c := s[i]; {
		case c >= '0' && c <= '7':
			v, n := 0, 0
			for n < 3 && i < len(s) && s[i] >= '0' && s[i] <= '7' {
				v, i, n = v*8+int(s[i]-'0'), i+1, n+1
			}
			i--
			b.WriteByte(byte(v)) //nolint:gosec // an octal escape is at most 0o777; a byte is what the PDF meant
		case c == 'n':
			b.WriteByte('\n')
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// The oracle works: a typeset file of the project is found in the PDF's text, and each
// reading command does read a project file. Without this, the refusals below could pass
// because nothing was ever looked at.
func TestSandboxReadControl(t *testing.T) {
	r, _ := realRunner(t, Config{})
	hexMarker := strings.ToUpper(hex.EncodeToString([]byte(insideMarker)))
	want := map[string][]string{
		"input":           {"PDF TEXT: start" + insideMarker},
		"openin":          {"READ:" + insideMarker},
		"lstinputlisting": {alnum(insideMarker)},
		"verbatiminput":   {alnum(insideMarker)},
		"pdffiledump":     {"DUMP:" + hexMarker},
		"IfFileExists":    {"RS-EXISTS-YES"},
	}
	for _, c := range readCommands {
		t.Run(c.name, func(t *testing.T) {
			res := run(t, r, readDoc(c.tex, "inside.txt"))
			all := everything(t, res)
			for _, w := range want[c.name] {
				if !strings.Contains(all, w) {
					t.Errorf("%q not found: the control read nothing\n%s", w, tail(all))
				}
			}
			if c.name == "pdfmdfivesum" && !md5Leak.MatchString(all) {
				t.Errorf("no MD5 of the project's file\n%s", tail(all))
			}
			if c.name == "input" && res.PDF == nil {
				t.Errorf("no PDF to read the text of: %s", tail(all))
			}
		})
	}
}

// NFR-4: no command reads a file outside the project, by an absolute path or a parent one:
// no line of /etc/passwd, of the environment or of the host appears anywhere.
func TestSandboxReadsOutsideWorkFail(t *testing.T) {
	r, _ := realRunner(t, Config{})
	bad := forbidden(t)
	for _, c := range readCommands {
		for _, target := range outsideTargets {
			t.Run(c.name+" "+target, func(t *testing.T) {
				res := run(t, r, readDoc(c.tex, target))
				all := everything(t, res)
				for _, b := range bad {
					if strings.Contains(all, b) {
						t.Errorf("read outside the project: %q in the output\n%s", b, tail(all))
					}
				}
				if md5Leak.MatchString(all) {
					t.Errorf("an MD5 of %s was computed: the file was read\n%s", target, tail(all))
				}
				if strings.Contains(all, "READ:") && regexp.MustCompile(`READ:\S`).MatchString(all) {
					t.Errorf("\\openin read a line of %s\n%s", target, tail(all))
				}
				if res.TimedOut || res.Unavailable {
					t.Errorf("result %+v", res)
				}
			})
		}
	}
}

// NFR-4: nothing is written outside the project: \openout to an absolute path outside /work
// or through a parent folder is refused (openout_any = p), and Landlock refuses writes
// outside /work and /tmp below TeX too. A file written there is read back, which must find nothing.
// Writing inside the project works.
func TestSandboxWritesOutsideWorkFail(t *testing.T) {
	r, _ := realRunner(t, Config{})
	doc := func(target string) map[string]string {
		return map[string]string{
			// The marker is put together by a macro, so the log's echo of the source line
			// never shows it: only a file written and read back does.
			"main.tex": "\\documentclass{article}\n\\def\\rsw{RS-WROTE}\n\\begin{document}\nstart\n" +
				`\newwrite\rso\immediate\openout\rso=` + target + `\relax\immediate\write\rso{\rsw-IT}\immediate\closeout\rso` + "\n" +
				`\IfFileExists{` + target + `}{\input{` + target + `}}{\typeout{RS-NOT-WRITTEN}}` + "\nok\n\\end{document}\n",
			"sub/keep.txt": "keep\n",
		}
	}
	for _, target := range []string{"/tmp/rs-escape.tex", "/etc/rs-escape.tex", "../rs-escape.tex", "/work/../tmp/rs-escape.tex", "/proc/self/rs-escape.tex", "/usr/local/texlive/rs-escape.tex",
		// biber's unpacked copy, and its lock, a link into /tmp (compile/image/Dockerfile).
		"/usr/local/share/rs-biber/rs-escape.tex", "/usr/local/share/rs-biber/inc.lock",
		// TeX Live 2026 refuses every absolute path under openout_any = p, even under
		// TEXMFOUTPUT ("no extended check"): projects write by relative paths.
		"/work/rs-absolute.tex"} {
		t.Run(target, func(t *testing.T) {
			res := run(t, r, doc(target))
			all := everything(t, res)
			if strings.Contains(all, "RS-WROTE-IT") {
				t.Errorf("wrote %s and read it back\n%s", target, tail(all))
			}
			if !strings.Contains(all, "openout_any = p") && !strings.Contains(all, "I can't write on file") {
				t.Errorf("no refusal for %s in the log\n%s", target, tail(all))
			}
			if res.TimedOut || res.Unavailable {
				t.Errorf("result %+v", res)
			}
		})
	}
	for _, target := range []string{"written.tex", "sub/written.tex"} {
		t.Run("control "+target, func(t *testing.T) {
			res := run(t, r, doc(target))
			if all := everything(t, res); !strings.Contains(all, "RS-WROTE-IT") {
				t.Errorf("a file of the project was not written and read back\n%s", tail(all))
			}
		})
	}
}

// inContainer runs a command in a compile container (the compile's settings, another
// entrypoint) and returns its output.
func inContainer(t *testing.T, r *Runner, dc *docker.Client, name string, entrypoint ...string) (string, int) {
	t.Helper()
	spec := r.Spec(Job{ID: name, MainFile: "main.tex"})
	spec.Entrypoint, spec.Cmd = entrypoint, nil
	spec.OpenStdin, spec.StdinOnce, spec.AttachStdin = false, false, false
	ctx := t.Context()
	id, err := dc.Create(ctx, "rs-compile-"+name, spec)
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
	var out bytes.Buffer
	done := make(chan error, 1)
	go func() { done <- a.Demux(&out, &out) }()
	code, err := dc.Wait(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	<-done
	return out.String(), code
}

// The settings are really in force inside the sandbox, not only on a command line: TeX's
// own (kpsewhich), Landlock's (the probe, and a shell under rs-landlock), and the minimal
// /etc of the image without Landlock (the second layer on its own).
func TestSandboxSettings(t *testing.T) {
	r, dc := realRunner(t, Config{})
	t.Run("TeX settings", func(t *testing.T) {
		out, code := inContainer(t, r, dc, "settings-tex", LandlockPath, "--", "/bin/sh", "-c",
			`for v in openin_any openout_any shell_escape TEXMFOUTPUT; do echo "$v=$(kpsewhich -var-value $v)"; done`)
		for _, want := range []string{"openin_any=p", "openout_any=p", "shell_escape=f", "TEXMFOUTPUT=/work"} {
			if !strings.Contains(out, want+"\n") {
				t.Errorf("%s not set (exit %d):\n%s", want, code, out)
			}
		}
	})
	t.Run("Landlock probe", func(t *testing.T) {
		got, err := r.Probe(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if !regexp.MustCompile(`^landlock abi=([89]|[1-9][0-9]+) ok$`).MatchString(got) {
			t.Errorf("probe: %q", got)
		}
	})
	t.Run("Landlock refuses a shell too", func(t *testing.T) {
		out, _ := inContainer(t, r, dc, "settings-shell", LandlockPath, "--", "/bin/sh", "-c",
			`cat /etc/passwd; cat /proc/self/environ; ls /proc; echo x > /tmp/ok && echo TMP-WRITABLE; echo x > /work/ok && echo WORK-WRITABLE`)
		for _, bad := range []string{"compile:x:", "RS_TIMEOUT"} {
			if strings.Contains(out, bad) {
				t.Errorf("%q readable under Landlock:\n%s", bad, out)
			}
		}
		for _, want := range []string{"/etc/passwd: Permission denied", "/proc/self/environ: Permission denied", "'/proc': Permission denied", "TMP-WRITABLE", "WORK-WRITABLE"} {
			if !strings.Contains(out, want) {
				t.Errorf("%q missing:\n%s", want, out)
			}
		}
	})
	t.Run("minimal /etc without Landlock", func(t *testing.T) {
		out, _ := inContainer(t, r, dc, "settings-etc", "/bin/sh", "-c", `cat /etc/passwd /etc/group; ls -A /home /root /var 2>&1`)
		if strings.Contains(out, "root:") || !strings.Contains(out, "compile:x:10001:10001:") {
			t.Errorf("/etc of the image:\n%s", out)
		}
		for _, d := range []string{"/home", "/root", "/var"} {
			if !strings.Contains(out, "cannot access '"+d+"'") {
				t.Errorf("%s exists in the image:\n%s", d, out)
			}
		}
	})
}

// The document that leaked the container's /etc/passwd before Landlock (2026-11-23 19:51,
// docs/decisions.md): with nothing else to typeset it now makes no PDF, so the API records the
// compile as failed (no_pdf) and the project keeps its last good PDF; and nothing of the file,
// the old image's or this one's, is in the log or anywhere else the compile returns.
func TestSandboxPasswdDocumentFails(t *testing.T) {
	r, _ := realRunner(t, Config{})
	res := run(t, r, map[string]string{"main.tex": "\\documentclass{article}\\begin{document}\\input{/etc/passwd}\\end{document}\n"})
	if res.TimedOut || res.Unavailable || res.Failure != "" {
		t.Fatalf("result %+v", res)
	}
	if res.PDF != nil {
		t.Errorf("a PDF was made (%d bytes): the compile would succeed", res.PDFSize)
	}
	all := everything(t, res)
	bad := append(forbidden(t), "daemon:x:1:1", "/usr/sbin/nologin", "www-data:x:", "10001:10001")
	for _, b := range bad {
		if strings.Contains(all, b) {
			t.Errorf("file content in the output: %q\n%s", b, tail(all))
		}
	}
	// Landlock refuses the open, and pdfTeX stops at once ("pdflatex: /etc/passwd: Permission
	// denied" on its stderr); main.log just ends, without a TeX error.
	if !strings.Contains(res.Stderr, "/etc/passwd: Permission denied") {
		t.Errorf("the open was not refused:\n%s", tail(res.Stderr))
	}
}
