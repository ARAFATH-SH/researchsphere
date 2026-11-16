package usecase

import (
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/embedding/domain"
)

func fullSource() domain.ProfileSource {
	return domain.ProfileSource{
		Eligible:  true,
		Headline:  "  Remote   sensing\tfor agriculture ",
		Bio:       "I study crops.\n\nAnd   rivers.",
		Interests: []string{"crop yield", "  graph\nneural networks "},
		Skills:    []string{"Python", "", "R"},
		Publications: []domain.PublicationText{
			{Title: "Newest paper", Abstract: "Its abstract."},
			{Title: "Older paper"},
		},
	}
}

// The text has a fixed order, one field per line, with whitespace collapsed (AI-1.1).
func TestProfileTextOrderAndWhitespace(t *testing.T) {
	got := ProfileText(fullSource())
	want := strings.Join([]string{
		"Remote sensing for agriculture",
		"crop yield, graph neural networks",
		"Python, R",
		"I study crops. And rivers.",
		"Newest paper",
		"Its abstract.",
		"Older paper",
	}, "\n")
	if got != want {
		t.Errorf("ProfileText =\n%s\nwant\n%s", got, want)
	}
}

// Headline, interests and skills come first, so they are always in the first chunk.
func TestProfileTextPutsHeadlineInterestsSkillsFirst(t *testing.T) {
	src := fullSource()
	src.Bio = strings.Repeat("long bio ", 2000)
	lines := strings.Split(ProfileText(src), "\n")
	if lines[0] != "Remote sensing for agriculture" || !strings.HasPrefix(lines[1], "crop yield") || lines[2] != "Python, R" {
		t.Errorf("first lines = %q", lines[:3])
	}
}

// Empty fields leave no blank lines, and an empty profile gives no text.
func TestProfileTextLeavesOutEmptyFields(t *testing.T) {
	if got := ProfileText(domain.ProfileSource{Eligible: true}); got != "" {
		t.Errorf("empty profile: %q, want empty", got)
	}
	if got := ProfileText(domain.ProfileSource{Headline: "  ", Skills: []string{" "}, Bio: "\n"}); got != "" {
		t.Errorf("blank fields: %q, want empty", got)
	}
	if got := ProfileText(domain.ProfileSource{Skills: []string{"Go"}}); got != "Go" {
		t.Errorf("skills only: %q, want Go", got)
	}
}

// Only the public fields of ProfileSource can reach the text: the source type has no contact
// field, so no email, phone, birthday or link can be added by mistake.
func TestProfileSourceHasNoContactField(t *testing.T) {
	allowed := map[string]bool{"Eligible": true, "Headline": true, "Bio": true, "Interests": true, "Skills": true, "Publications": true}
	typ := reflect.TypeFor[domain.ProfileSource]()
	for i := range typ.NumField() {
		if name := typ.Field(i).Name; !allowed[name] {
			t.Errorf("ProfileSource has field %s: decide whether it is public before it reaches the embedding", name)
		}
	}
	pub := reflect.TypeFor[domain.PublicationText]()
	if pub.NumField() != 2 {
		t.Errorf("PublicationText has %d fields, want Title and Abstract", pub.NumField())
	}
}

// The same profile always gives the same hash; any change gives another.
func TestContentHashIsStable(t *testing.T) {
	a := ContentHash(ProfileText(fullSource()))
	src := fullSource()
	src.Headline = "Remote sensing for agriculture" // same after normalising
	if b := ContentHash(ProfileText(src)); a != b {
		t.Errorf("hash changed with whitespace only: %s vs %s", a, b)
	}
	src.Skills = append(src.Skills, "Go")
	if c := ContentHash(ProfileText(src)); c == a {
		t.Error("hash did not change with the text")
	}
	if len(a) != 64 || strings.Trim(a, "0123456789abcdef") != "" {
		t.Errorf("hash %q is not 64 hex characters", a)
	}
}

func TestCutText(t *testing.T) {
	short := "গবেষণা"
	if cutText(short) != short {
		t.Error("short text changed")
	}
	long := strings.Repeat("ক", domain.MaxTextChars+10)
	got := cutText(long)
	if n := utf8.RuneCountInString(got); n != domain.MaxTextChars || !utf8.ValidString(got) {
		t.Errorf("cut text has %d characters (valid %v), want %d", n, utf8.ValidString(got), domain.MaxTextChars)
	}
}

// AI-1.2: a project's text is its title, skills, description and abstract, each on one line;
// empty fields are left out. Its hash differs from a profile's with the same text, so the
// two text formats can change apart.
func TestProjectText(t *testing.T) {
	tests := []struct {
		src  domain.ProjectSource
		want string
	}{
		{domain.ProjectSource{Title: " Graph\tlearning ", Skills: []string{"Go", " ", "Rust"}, Description: "Line one\nline two", Abstract: "Abs"},
			"Graph learning\nGo, Rust\nLine one line two\nAbs"},
		{domain.ProjectSource{Title: "Only a title"}, "Only a title"},
		{domain.ProjectSource{}, ""},
	}
	for _, tt := range tests {
		if got := ProjectText(tt.src); got != tt.want {
			t.Errorf("ProjectText(%+v) = %q, want %q", tt.src, got, tt.want)
		}
	}
	if ProjectContentHash("x") == ContentHash("x") {
		t.Error("project and profile texts hash the same")
	}
	if hashOf(domain.OwnerProject, "x") != ProjectContentHash("x") || hashOf(domain.OwnerProfile, "x") != ContentHash("x") {
		t.Error("hashOf picks the wrong hash")
	}
}
