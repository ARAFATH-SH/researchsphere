package usecase

import (
	"slices"
	"strings"
	"unicode"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/messaging/domain"
)

// Snippet sizes (FR-16.8), in Unicode code points.
const (
	// SnippetLength is the most text a snippet shows, not counting the "…" marks of a cut.
	SnippetLength = 160
	// snippetLead is how much text comes before the first highlight when the text is cut.
	snippetLead = 40
)

// ellipsis marks where a snippet cuts the text.
const ellipsis = "…"

// Snippet returns about SnippetLength characters of text around its first highlighted word,
// and the highlights inside it. Runs of white space become one space. Ranges count Unicode code
// points (the client indexes Array.from(snippet)), are sorted and never overlap. A word is
// highlighted where it contains a query term (a word or partial word) or is a small typo of it
// (see similar). When nothing is highlighted, the snippet is the start of the text.
func Snippet(text string, terms [][]rune) (string, []domain.Range) {
	t := []rune(strings.Join(strings.Fields(text), " "))
	rs := highlights(t, terms)
	if len(t) <= SnippetLength {
		return string(t), rs
	}
	anchor, anchorEnd := 0, 0
	if len(rs) > 0 {
		anchor, anchorEnd = rs[0].Start, rs[0].Start+rs[0].Length
	}
	start := max(0, anchor-snippetLead)
	end := min(len(t), start+SnippetLength)
	if end == len(t) {
		start = max(0, end-SnippetLength)
	}
	// Begin and end on word boundaries, never cutting off the first highlight.
	if start > 0 && t[start-1] != ' ' {
		if j := slices.Index(t[start:anchor], ' '); j >= 0 {
			start += j + 1
		}
	}
	if end < len(t) && t[end] != ' ' {
		from := min(max(anchorEnd, start), end)
		if j := lastIndex(t[from:end], ' '); j >= 0 {
			end = from + j
		}
	}
	for start < end && t[start] == ' ' {
		start++
	}
	for end > start && t[end-1] == ' ' {
		end--
	}
	var b strings.Builder
	shift := -start
	if start > 0 {
		b.WriteString(ellipsis)
		shift++
	}
	b.WriteString(string(t[start:end]))
	if end < len(t) {
		b.WriteString(ellipsis)
	}
	var out []domain.Range
	for _, r := range rs {
		s, e := max(r.Start, start), min(r.Start+r.Length, end)
		if e > s {
			out = append(out, domain.Range{Start: s + shift, Length: e - s})
		}
	}
	return b.String(), out
}

// queryTerms splits the search text into lower-case words, each once.
func queryTerms(q string) [][]rune {
	r := []rune(q)
	var out [][]rune
	for _, w := range words(r) {
		term := lower(r[w[0]:w[1]])
		if !slices.ContainsFunc(out, func(o []rune) bool { return slices.Equal(o, term) }) {
			out = append(out, term)
		}
	}
	return out
}

// isWordRune is a letter, a combining mark (Bangla vowel signs and the virama are marks) or a
// digit, or the zero-width joiners that Bangla writes inside words.
func isWordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsMark(r) || unicode.IsDigit(r) || r == '‌' || r == '‍'
}

// words returns the [start, end) code point indexes of each word of t.
func words(t []rune) [][2]int {
	var out [][2]int
	start := -1
	for i, r := range t {
		switch {
		case isWordRune(r) && start < 0:
			start = i
		case !isWordRune(r) && start >= 0:
			out = append(out, [2]int{start, i})
			start = -1
		}
	}
	if start >= 0 {
		out = append(out, [2]int{start, len(t)})
	}
	return out
}

// lower lowers each code point on its own, so indexes stay those of the original text.
func lower(t []rune) []rune {
	out := make([]rune, len(t))
	for i, r := range t {
		out[i] = unicode.ToLower(r)
	}
	return out
}

// highlights finds, in each word of t, the first place that contains a term (a whole word or a
// partial one), or marks the whole word when it is similar to a term. The ranges are merged.
func highlights(t []rune, terms [][]rune) []domain.Range {
	if len(terms) == 0 {
		return nil
	}
	low := lower(t)
	var out []domain.Range
	for _, w := range words(t) {
		tok := low[w[0]:w[1]]
		for _, term := range terms {
			if i := indexRunes(tok, term); i >= 0 && (len(term) >= 2 || len(tok) == 1) {
				out = append(out, domain.Range{Start: w[0] + i, Length: len(term)})
			} else if similar(term, tok) {
				out = append(out, domain.Range{Start: w[0], Length: w[1] - w[0]})
			}
		}
	}
	slices.SortFunc(out, func(a, b domain.Range) int { return a.Start - b.Start })
	var merged []domain.Range
	for _, r := range out {
		if n := len(merged); n > 0 && r.Start < merged[n-1].Start+merged[n-1].Length {
			last := &merged[n-1]
			last.Length = max(last.Start+last.Length, r.Start+r.Length) - last.Start
			continue
		}
		merged = append(merged, r)
	}
	return merged
}

// similar reports whether word, or the start of it, is within a small edit distance of term:
// one edit for terms of 4 to 7 characters, two from 8. Shorter terms must appear as they are.
// It mirrors what the trigram match finds (partial words and typos) closely enough to mark
// the word; the database decides what matches.
func similar(term, word []rune) bool {
	n := len(term)
	if n < 4 {
		return false
	}
	maxEdits := 1
	if n >= 8 {
		maxEdits = 2
	}
	for l := max(1, n-maxEdits); l <= min(len(word), n+maxEdits); l++ {
		if editDistance(term, word[:l]) <= maxEdits {
			return true
		}
	}
	return false
}

// editDistance is the optimal string alignment distance: insertions, deletions, substitutions
// and swaps of two neighbours each count one.
func editDistance(a, b []rune) int {
	prev2 := make([]int, len(b)+1)
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
			if i > 1 && j > 1 && a[i-1] == b[j-2] && a[i-2] == b[j-1] {
				cur[j] = min(cur[j], prev2[j-2]+1)
			}
		}
		prev2, prev, cur = prev, cur, prev2
	}
	return prev[len(b)]
}

func indexRunes(s, sub []rune) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if slices.Equal(s[i:i+len(sub)], sub) {
			return i
		}
	}
	return -1
}

func lastIndex(s []rune, r rune) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == r {
			return i
		}
	}
	return -1
}
