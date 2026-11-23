package usecase

import (
	"path"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/compile/domain"
)

// The log parser (FR-13.2). The sandbox runs pdfLaTeX with -file-line-error and
// max_print_line=10000 (docs/compile.md), so an error reads "./dir/file.tex:12: message" on one
// line and file names are never broken across lines. Warnings name no file: the parser
// follows the files TeX opens ("(./file.tex" ... ")") to know which file is being read.
//
// The log is written by the user's document (\typeout can print anything), so nothing in it
// is trusted: a file is reported only when it is one of the project's text files, messages
// are cut to MaxMessage characters and there are at most MaxProblems problems. Go's regexp
// runs in linear time.

// Bounds of the parsed problems.
const (
	MaxProblems = 100
	MaxMessage  = 500
)

var (
	// "./sections/intro.tex:12: Undefined control sequence."
	fileLineError = regexp.MustCompile(`^(\.{0,2}/?[^:\s][^:]*?\.[A-Za-z]+):(\d+): (.+)$`)
	// "! Emergency stop." and other errors printed without -file-line-error's form.
	bangError = regexp.MustCompile(`^! (.+)$`)
	// "l.12 \foo": the line an error happened on.
	errorContext = regexp.MustCompile(`^l\.(\d+)\b(.*)$`)
	// "LaTeX Warning: Reference `x' on page 1 undefined on input line 5."
	latexWarning = regexp.MustCompile(`^LaTeX Warning: (.+)$`)
	// "Package hyperref Warning: ..." or "Class article Warning: ...", continued on lines
	// that start with "(hyperref)".
	packageWarning = regexp.MustCompile(`^(?:Package|Class) (\S+) Warning: (.+)$`)
	// "Overfull \hbox (12.0pt too wide) in paragraph at lines 3--4"
	overfull  = regexp.MustCompile(`^(Overfull \\[hv]box .*?) (?:in paragraph |in alignment |detected )?at lines? (\d+)`)
	inputLine = regexp.MustCompile(`on input line (\d+)\.?`)
	// "Output written on main.pdf (10 pages, 123456 bytes)."
	outputWritten = regexp.MustCompile(`^Output written on .+ \((\d+) pages?`)
	// BibTeX: "I was expecting a `,' or a `}'---line 5 of file refs.bib"
	bibLineError = regexp.MustCompile(`^(.+?)---line (\d+) of file (.+)$`)
	// BibTeX: "--line 5 of file refs.bib", the place of the warning before it.
	bibLinePlace = regexp.MustCompile(`^--line (\d+) of file (.+)$`)
	// BibTeX: "Warning--I didn't find a database entry for "x""
	bibWarning = regexp.MustCompile(`^Warning--(.+)$`)
	// BibTeX: errors without a place.
	bibError = regexp.MustCompile(`^(I couldn't open .+|I found no .+|Illegal, another \\bib.+)$`)
	// A name TeX prints when it opens a file.
	fileOpen = regexp.MustCompile(`^\(((?:\.{1,2}/|/)[^\s()]+|[A-Za-z0-9_\-./]+\.(?:tex|sty|cls|cfg|def|clo|fd|aux|bbl|toc|lof|lot|out|ldf|bib|ltx))`)
)

// ParseLogs reads the problems of a compile from the pdfLaTeX log and the BibTeX log:
// errors first, then warnings, each in the order they appear, without repeats, at most
// MaxProblems. textPaths are the project's text files; pages is the PDF's page count when
// the log says it.
func ParseLogs(latexLog, bibtexLog string, textPaths []string) ([]domain.Problem, *int) {
	known := make(map[string]bool, len(textPaths))
	for _, p := range textPaths {
		known[p] = true
	}
	p := parser{known: known, seen: map[string]bool{}}
	pages := p.latex(latexLog)
	p.bibtex(bibtexLog)
	out := append(p.errors, p.warnings...)
	if len(out) > MaxProblems {
		out = out[:MaxProblems]
	}
	if out == nil {
		out = []domain.Problem{}
	}
	return out, pages
}

type parser struct {
	known    map[string]bool
	seen     map[string]bool
	errors   []domain.Problem
	warnings []domain.Problem
	stack    []string // files being read; "" for a parenthesis that opened no file
}

// projectFile maps a name in the log to a project path, or nil for any other file.
func (p *parser) projectFile(name string) *string {
	name = strings.TrimPrefix(strings.TrimSpace(name), "/work/")
	name = strings.TrimPrefix(path.Clean(name), "./")
	if p.known[name] {
		return &name
	}
	return nil
}

func (p *parser) current() *string {
	for i := len(p.stack) - 1; i >= 0; i-- {
		if p.stack[i] != "" {
			return p.projectFile(p.stack[i])
		}
	}
	return nil
}

func (p *parser) add(file *string, line *int, msg string, sev domain.Severity) {
	msg = clip(strings.TrimSpace(msg))
	if msg == "" {
		return
	}
	key := string(sev) + "\x00" + msg
	if file != nil {
		key += "\x00" + *file
	}
	if line != nil {
		key += "\x00" + strconv.Itoa(*line)
	}
	if p.seen[key] || len(p.errors)+len(p.warnings) >= 4*MaxProblems {
		return
	}
	p.seen[key] = true
	pr := domain.Problem{File: file, Line: line, Message: msg, Severity: sev}
	if sev == domain.SeverityError {
		p.errors = append(p.errors, pr)
	} else {
		p.warnings = append(p.warnings, pr)
	}
}

// track follows the files TeX opens and closes on one line.
func (p *parser) track(line string) {
	for i := 0; i < len(line); i++ {
		switch line[i] {
		case '(':
			if m := fileOpen.FindStringSubmatch(line[i:]); m != nil {
				p.stack = append(p.stack, m[1])
				i += len(m[0]) - 1
			} else {
				p.stack = append(p.stack, "")
			}
		case ')':
			if len(p.stack) > 0 {
				p.stack = p.stack[:len(p.stack)-1]
			}
		}
	}
}

func (p *parser) latex(log string) *int {
	var pages *int
	lines := strings.Split(strings.ReplaceAll(log, "\r\n", "\n"), "\n")
	for i := 0; i < len(lines); i++ {
		l := lines[i]
		switch {
		case fileLineError.MatchString(l):
			m := fileLineError.FindStringSubmatch(l)
			n, _ := strconv.Atoi(m[2])
			_, source := errorAt(lines, i)
			p.add(p.projectFile(m[1]), &n, withHint(m[3], source), domain.SeverityError)
		case bangError.MatchString(l):
			m := bangError.FindStringSubmatch(l)
			at, source := errorAt(lines, i)
			p.add(p.current(), at, withHint(m[1], source), domain.SeverityError)
		case latexWarning.MatchString(l):
			msg := latexWarning.FindStringSubmatch(l)[1]
			for i+1 < len(lines) && strings.TrimSpace(lines[i+1]) != "" && !strings.HasPrefix(lines[i+1], "(") &&
				!strings.Contains(lines[i+1], "Warning") && len(lines[i+1]) > 0 && lines[i+1][0] != '!' &&
				!strings.HasSuffix(msg, ".") {
				i++
				msg += " " + strings.TrimSpace(lines[i])
			}
			p.warning(msg)
		case packageWarning.MatchString(l):
			m := packageWarning.FindStringSubmatch(l)
			msg := m[1] + ": " + m[2]
			cont := "(" + m[1] + ")"
			for i+1 < len(lines) && strings.HasPrefix(lines[i+1], cont) {
				i++
				msg += " " + strings.TrimSpace(strings.TrimPrefix(lines[i], cont))
			}
			p.warning(msg)
		case overfull.MatchString(l):
			m := overfull.FindStringSubmatch(l)
			n, _ := strconv.Atoi(m[2])
			p.add(p.current(), &n, m[1], domain.SeverityWarning)
		case outputWritten.MatchString(l):
			n, _ := strconv.Atoi(outputWritten.FindStringSubmatch(l)[1])
			pages = &n
		default:
			p.track(l)
		}
	}
	return pages
}

// errorAt reads the "l.N <source>" line TeX prints within 10 lines after an error: the line
// number and the source text up to where TeX stopped.
func errorAt(lines []string, i int) (*int, string) {
	for j := i + 1; j < len(lines) && j <= i+10; j++ {
		if c := errorContext.FindStringSubmatch(lines[j]); c != nil {
			n, _ := strconv.Atoi(c[1])
			return &n, strings.TrimSpace(c[2])
		}
	}
	return nil, ""
}

// RestartHint explains "Can be used only in preamble" at a second \documentclass or
// \begin{document}: the usual cause is a whole document pasted below another one.
const RestartHint = "This file has more than one \\documentclass. Make sure the document starts only once (Ctrl+A before pasting replaces everything)."

// withHint adds a hint to an error whose cause is known from where TeX stopped.
func withHint(msg, source string) string {
	if strings.Contains(msg, "Can be used only in preamble") &&
		(strings.HasSuffix(source, "\\documentclass") || strings.HasSuffix(source, "\\begin{document}")) {
		return strings.TrimSpace(msg) + " " + RestartHint
	}
	return msg
}

// warning adds a LaTeX or package warning, at its "on input line N" in the current file.
func (p *parser) warning(msg string) {
	var at *int
	if m := inputLine.FindStringSubmatch(msg); m != nil {
		n, _ := strconv.Atoi(m[1])
		at = &n
	}
	p.add(p.current(), at, msg, domain.SeverityWarning)
}

func (p *parser) bibtex(log string) {
	lines := strings.Split(strings.ReplaceAll(log, "\r\n", "\n"), "\n")
	for i, l := range lines {
		l = strings.TrimSpace(l)
		switch {
		case bibLineError.MatchString(l):
			m := bibLineError.FindStringSubmatch(l)
			n, _ := strconv.Atoi(m[2])
			p.add(p.projectFile(m[3]), &n, "BibTeX: "+m[1], domain.SeverityError)
		case bibWarning.MatchString(l):
			msg := "BibTeX: " + bibWarning.FindStringSubmatch(l)[1]
			var file *string
			var at *int
			if i+1 < len(lines) {
				if m := bibLinePlace.FindStringSubmatch(strings.TrimSpace(lines[i+1])); m != nil {
					n, _ := strconv.Atoi(m[1])
					file, at = p.projectFile(m[2]), &n
				}
			}
			p.add(file, at, msg, domain.SeverityWarning)
		case bibError.MatchString(l):
			p.add(nil, nil, "BibTeX: "+l, domain.SeverityError)
		}
	}
}

// clip cuts s to MaxMessage characters.
func clip(s string) string {
	s = strings.ToValidUTF8(s, "�")
	if utf8.RuneCountInString(s) <= MaxMessage {
		return s
	}
	r := []rune(s)
	return string(r[:MaxMessage-1]) + "…"
}
