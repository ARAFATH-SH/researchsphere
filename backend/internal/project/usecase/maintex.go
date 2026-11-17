package usecase

import (
	"strings"
	"unicode"
)

// MainTex is the starting document of a new project (FR-10.4): the project's title, its
// owner as the author, one section and a figure example commented out. Both texts are
// escaped for LaTeX, so a title like "R&D at 100%" compiles. pdfLaTeX (FR-13.1) cannot set
// scripts such as Bangla; such a title compiles only after the user edits it or changes the
// engine (docs/decisions.md 2026-11-17).
func MainTex(title, author string) string {
	return `\documentclass{article}
\usepackage[utf8]{inputenc}
\usepackage[T1]{fontenc}
\usepackage{graphicx}

\title{` + latexEscape(title) + `}
\author{` + latexEscape(author) + `}
\date{\today}

\begin{document}

\maketitle

\section{Introduction}

Start writing here.

% To add a figure, upload it (PNG, JPG or PDF), for example to figures/example.png, and
% remove the % signs below.
% \begin{figure}[ht]
%   \centering
%   \includegraphics[width=0.8\linewidth]{figures/example.png}
%   \caption{An example figure.}
%   \label{fig:example}
% \end{figure}

\end{document}
`
}

// latexSpecial is how each of LaTeX's special characters is written as text.
var latexSpecial = map[rune]string{
	'\\': `\textbackslash{}`,
	'{':  `\{`,
	'}':  `\}`,
	'$':  `\$`,
	'&':  `\&`,
	'#':  `\#`,
	'^':  `\textasciicircum{}`,
	'_':  `\_`,
	'%':  `\%`,
	'~':  `\textasciitilde{}`,
}

// latexEscape writes s as LaTeX text on one line: special characters escaped, control and
// format characters and runs of white space made one space, trimmed.
func latexEscape(s string) string {
	var b strings.Builder
	space := false
	for _, r := range s {
		if unicode.IsSpace(r) || unicode.Is(unicode.Cc, r) || unicode.Is(unicode.Cf, r) {
			space = true
			continue
		}
		if space && b.Len() > 0 {
			b.WriteByte(' ')
		}
		space = false
		if e, ok := latexSpecial[r]; ok {
			b.WriteString(e)
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
