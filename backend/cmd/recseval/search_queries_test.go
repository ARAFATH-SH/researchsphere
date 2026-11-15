package main

import (
	"encoding/json"
	"io/fs"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"
)

// searchStopwords are function words a query may share with the sample text: they carry no
// topic. Everything else counts.
var searchStopwords = map[string]bool{}

func init() {
	for _, w := range strings.Fields(`a an the of for and or in on to with that by from at as is are into how
		who what which their its it be can this these those our we they them whose so do does not no than when
		where while about over under between through after before up out off
		ও এবং এর যে যা করে কীভাবে কিভাবে জন্য থেকে দিয়ে নিয়ে কি কী না হয় আর এ এই সেই তার যারা যেখানে মধ্যে কোন কোনো`) {
		searchStopwords[w] = true
	}
}

var placeholderRE = regexp.MustCompile(`\{[^}]*\}`)

// textWords splits s into lower-case words of letters, marks and digits (so Bangla vowel
// signs stay in their word), without template placeholders such as {interest}.
func textWords(s string) []string {
	s = strings.ToLower(placeholderRE.ReplaceAllString(s, " "))
	return strings.FieldsFunc(s, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsMark(r) && !unicode.IsDigit(r)
	})
}

// sameWord reports whether a and b count as one word: equal, or one starts with the other
// (both at least 4 characters: "computer" and "computers", "image" and "imaging"), or they
// share their first 5 characters ("electricity" and "electrical").
func sameWord(a, b string) bool {
	if a == b {
		return true
	}
	ra, rb := []rune(a), []rune(b)
	if len(ra) >= 4 && len(rb) >= 4 && (strings.HasPrefix(a, b) || strings.HasPrefix(b, a)) {
		return true
	}
	return len(ra) >= 5 && len(rb) >= 5 && string(ra[:5]) == string(rb[:5])
}

// allStrings collects every string in a decoded JSON value.
func allStrings(v any, out *[]string) {
	switch x := v.(type) {
	case string:
		*out = append(*out, x)
	case []any:
		for _, y := range x {
			allStrings(y, out)
		}
	case map[string]any:
		for _, y := range x {
			allStrings(y, out)
		}
	}
}

// readJSON reads one of the sample content files of cmd/seedsample.
func readJSON(t *testing.T, name string) any {
	t.Helper()
	b, err := fs.ReadFile(os.DirFS("../seedsample/content"), name)
	if err != nil {
		t.Fatal(err)
	}
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

// AI-5.2: each evaluation query shares no word with its cluster's whole vocabulary (every
// field of clusters.json, English and Bangla, and the words common.json adds to every
// profile: roles, institutions, names, noise tags), so a match can only be one of meaning.
// There is one English and one Bangla query per cluster.
func TestSearchQueriesShareNoWords(t *testing.T) {
	qs, err := loadSearchQueries()
	if err != nil {
		t.Fatal(err)
	}
	var common []string
	allStrings(readJSON(t, "common.json"), &common)
	vocab := map[string][]string{}
	for _, c := range readJSON(t, "clusters.json").([]any) {
		var strs []string
		allStrings(c, &strs)
		strs = append(strs, common...)
		var words []string
		for _, s := range strs {
			words = append(words, textWords(s)...)
		}
		slices.Sort(words)
		vocab[c.(map[string]any)["id"].(string)] = slices.Compact(words)
	}
	if len(vocab) != 20 {
		t.Fatalf("%d clusters, want 20", len(vocab))
	}
	langs := map[string][]string{}
	for _, q := range qs {
		words, ok := vocab[q.Cluster]
		if !ok {
			t.Errorf("%q: unknown cluster %q", q.Text, q.Cluster)
			continue
		}
		langs[q.Cluster] = append(langs[q.Cluster], q.Lang)
		if n := utf8.RuneCountInString(q.Text); n < 2 || n > 300 {
			t.Errorf("%q: %d characters", q.Text, n)
		}
		for _, w := range textWords(q.Text) {
			if searchStopwords[w] {
				continue
			}
			for _, v := range words {
				if sameWord(w, v) {
					t.Errorf("%s (%s) %q: %q is in the cluster's vocabulary as %q", q.Cluster, q.Lang, q.Text, w, v)
				}
			}
		}
	}
	for c := range vocab {
		got := langs[c]
		slices.Sort(got)
		if !slices.Equal(got, []string{"bn", "en"}) {
			t.Errorf("cluster %s has queries %v, want one bn and one en", c, got)
		}
	}
}

// The check itself: it catches a shared word, a plural, a stem, a Bangla word with vowel
// signs, and a placeholder never counts.
func TestSameWordAndTextWords(t *testing.T) {
	for _, tt := range []struct {
		a, b string
		want bool
	}{
		{"chest", "chest", true},
		{"rays", "ray", false}, // too short to call a stem
		{"computers", "computer", true},
		{"imaging", "images", false},
		{"electricity", "electrical", true},
		{"রোগ", "রোগ", true},
		{"heat", "hearts", false},
	} {
		if got := sameWord(tt.a, tt.b); got != tt.want {
			t.Errorf("sameWord(%q, %q) = %v", tt.a, tt.b, got)
		}
	}
	if got := textWords("{role} in Chest X-ray, চিকিৎসা চিত্র"); !slices.Equal(got, []string{"in", "chest", "x", "ray", "চিকিৎসা", "চিত্র"}) {
		t.Errorf("textWords = %q", got)
	}
}
