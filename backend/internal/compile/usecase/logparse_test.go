package usecase

import (
	"os"
	"strings"
	"testing"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/compile/domain"
)

func describe(p domain.Problem) string {
	f, l := "-", "-"
	if p.File != nil {
		f = *p.File
	}
	if p.Line != nil {
		l = itoaTest(*p.Line)
	}
	return string(p.Severity) + " " + f + ":" + l + " " + p.Message
}

func itoaTest(n int) string {
	if n == 0 {
		return "0"
	}
	s := ""
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	return s
}

// FR-13.2: each problem with its project file and line, errors first; files outside the
// project (packages, the class) have no file.
func TestParseLogsMultiFile(t *testing.T) {
	log := strings.Join([]string{
		"This is pdfTeX, Version 3.141592653-2.6-1.40.27 (TeX Live 2026) (preloaded format=pdflatex)",
		"(./main.tex",
		"LaTeX2e <2025-11-01>",
		"(/usr/local/texlive/2026/texmf-dist/tex/latex/base/article.cls",
		"Document Class: article 2024/06/29 v1.4n Standard LaTeX document class",
		"(/usr/local/texlive/2026/texmf-dist/tex/latex/base/size10.clo))",
		"(./main.aux) (./sections/intro.tex",
		"./sections/intro.tex:7: Undefined control sequence.",
		"l.7 \\foo",
		"",
		"LaTeX Warning: Reference `fig:x' on page 1 undefined on input line 9.",
		"",
		"Overfull \\hbox (12.3pt too wide) in paragraph at lines 11--12",
		"[]\\OT1/cmr/m/n/10 text",
		")",
		"Package natbib Warning: Citation `knuth' on page 1 undefined on input line 14.",
		"",
		"/usr/local/texlive/2026/texmf-dist/tex/latex/graphics/graphicx.sty:99: LaTeX Error: Something in a package.",
		"./main.tex:20: LaTeX Error: File `missing.sty' not found.",
		"(./main.bbl)",
		"Package hyperref Warning: Token not allowed in a PDF string (Unicode):",
		"(hyperref)                removing `math shift' on input line 3.",
		"",
		"[1{/usr/local/texlive/2026/texmf-var/fonts/map/pdftex/updmap/pdftex.map}] (./main.aux) )",
		"Output written on main.pdf (12 pages, 54321 bytes).",
	}, "\n")
	blg := strings.Join([]string{
		"This is BibTeX, Version 0.99d (TeX Live 2026)",
		"The top-level auxiliary file: main.aux",
		"I was expecting a `,' or a `}'---line 5 of file refs.bib",
		" :   title = \"A",
		"Warning--empty journal in knuth",
		"--line 12 of file refs.bib",
		"Warning--I didn't find a database entry for \"lamport\"",
		"(There was 1 error message)",
	}, "\n")
	problems, pages := ParseLogs(log, blg, []string{"main.tex", "sections/intro.tex", "refs.bib"})
	var got []string
	for _, p := range problems {
		got = append(got, describe(p))
	}
	want := []string{
		"error sections/intro.tex:7 Undefined control sequence.",
		"error -:99 LaTeX Error: Something in a package.",
		"error main.tex:20 LaTeX Error: File `missing.sty' not found.",
		"error refs.bib:5 BibTeX: I was expecting a `,' or a `}'",
		"warning sections/intro.tex:9 Reference `fig:x' on page 1 undefined on input line 9.",
		"warning sections/intro.tex:11 Overfull \\hbox (12.3pt too wide)",
		"warning main.tex:14 natbib: Citation `knuth' on page 1 undefined on input line 14.",
		"warning main.tex:3 hyperref: Token not allowed in a PDF string (Unicode): removing `math shift' on input line 3.",
		"warning refs.bib:12 BibTeX: empty journal in knuth",
		"warning -:- BibTeX: I didn't find a database entry for \"lamport\"",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("problems:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if pages == nil || *pages != 12 {
		t.Errorf("pages = %v, want 12", pages)
	}
}

// The log is the document's to write: a forged error never names a file outside the project,
// messages are cut, problems are capped, and repeats are dropped.
func TestParseLogsUntrustedInput(t *testing.T) {
	var b strings.Builder
	b.WriteString("../../etc/passwd.tex:1: forged\n/work/main.tex:2: absolute in the sandbox\n")
	b.WriteString("./main.tex:3: " + strings.Repeat("é", 900) + "\n")
	for i := range 300 {
		b.WriteString("./main.tex:" + itoaTest(i+10) + ": Undefined control sequence.\n")
		b.WriteString("./main.tex:5: same again\n")
	}
	problems, pages := ParseLogs(b.String(), "", []string{"main.tex"})
	if pages != nil {
		t.Errorf("pages = %v", *pages)
	}
	if len(problems) != MaxProblems {
		t.Fatalf("%d problems, want %d", len(problems), MaxProblems)
	}
	if problems[0].File != nil {
		t.Errorf("a path outside the project was named: %s", *problems[0].File)
	}
	if problems[1].File == nil || *problems[1].File != "main.tex" {
		t.Errorf("sandbox path = %v", problems[1].File)
	}
	if n := len([]rune(problems[2].Message)); n != MaxMessage {
		t.Errorf("message of %d characters", n)
	}
	repeats := 0
	for _, p := range problems {
		if p.Message == "same again" {
			repeats++
		}
	}
	if repeats != 1 {
		t.Errorf("a repeated problem appears %d times", repeats)
	}
	if empty, _ := ParseLogs("", "", nil); empty == nil || len(empty) != 0 {
		t.Errorf("empty log: %v", empty)
	}
}

// A real log from the sandbox (compile/testdata, written by make test-compile-sandbox): the
// multi-file sample project with its BibTeX run.
func TestParseLogsSandboxFixture(t *testing.T) {
	log, err1 := os.ReadFile("testdata/multifile.log")
	blg, err2 := os.ReadFile("testdata/multifile.blg")
	if err1 != nil || err2 != nil {
		t.Skip("no sandbox fixture")
	}
	problems, pages := ParseLogs(string(log), string(blg), []string{"main.tex", "chapters/intro.tex", "chapters/method.tex", "refs.bib"})
	if pages == nil || *pages < 1 {
		t.Errorf("pages = %v", pages)
	}
	var sawIntro, sawBib bool
	for _, p := range problems {
		if p.Severity == domain.SeverityError && p.File != nil && *p.File == "chapters/intro.tex" && p.Line != nil && *p.Line == 4 {
			sawIntro = true
		}
		if p.File != nil && *p.File == "refs.bib" {
			sawBib = true
		}
	}
	if !sawIntro || !sawBib {
		var got []string
		for _, p := range problems {
			got = append(got, describe(p))
		}
		t.Errorf("want an error at chapters/intro.tex:4 and a BibTeX problem in refs.bib; got:\n%s", strings.Join(got, "\n"))
	}
}

// A document pasted below another starts twice: "Can be used only in preamble" at the second
// \documentclass and \begin{document} gets the hint; the same error elsewhere does not.
func TestParseLogsRestartHint(t *testing.T) {
	log := `(./main.tex
./main.tex:14: LaTeX Error: Can be used only in preamble.

See the LaTeX manual or LaTeX Companion for explanation.
Type  H <return>  for immediate help.
 ...                                              
                                                  
l.14 \documentclass
                   [11pt]{article}
./main.tex:15: LaTeX Error: Can be used only in preamble.

See the LaTeX manual or LaTeX Companion for explanation.
Type  H <return>  for immediate help.
 ...                                              
                                                  
l.15 \begin{document}
                     
./main.tex:20: LaTeX Error: Can be used only in preamble.

See the LaTeX manual or LaTeX Companion for explanation.
Type  H <return>  for immediate help.
 ...                                              
                                                  
l.20 \usepackage
                {tikz}
! LaTeX Error: Can be used only in preamble.

See the LaTeX manual or LaTeX Companion for explanation.
Type  H <return>  for immediate help.
 ...                                              
                                                  
l.30 \documentclass
                   {article}
)
`
	problems, _ := ParseLogs(log, "", []string{"main.tex"})
	want := []string{
		"error main.tex:14 LaTeX Error: Can be used only in preamble. " + RestartHint,
		"error main.tex:15 LaTeX Error: Can be used only in preamble. " + RestartHint,
		"error main.tex:20 LaTeX Error: Can be used only in preamble.",
		"error main.tex:30 LaTeX Error: Can be used only in preamble. " + RestartHint,
	}
	if len(problems) != len(want) {
		t.Fatalf("%d problems: %v", len(problems), problems)
	}
	for i, p := range problems {
		if got := describe(p); got != want[i] {
			t.Errorf("problem %d = %q\nwant %q", i, got, want[i])
		}
	}
	if !strings.HasPrefix(RestartHint, `This file has more than one \documentclass.`) {
		t.Errorf("hint = %q", RestartHint)
	}
}
