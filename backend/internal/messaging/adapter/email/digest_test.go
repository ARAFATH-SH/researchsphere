package email

import (
	"regexp"
	"strings"
	"testing"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/messaging/domain"
)

const base = "https://rs.example"

func digest() domain.Digest {
	return domain.Digest{
		To: "ada@example.org", FullName: "Ada Lovelace", Unread: 12,
		Top: []domain.DigestConversation{
			{Name: "Nafisa Hasan", Unread: 3},
			{Name: "CVS Paper Team", Group: true, Unread: 5},
		},
		More: 2, Requests: 2, UnsubscribeToken: "abc.def",
	}
}

func TestRenderContent(t *testing.T) {
	r, err := Render(digest(), base+"/")
	if err != nil {
		t.Fatal(err)
	}
	if want := "You have 12 unread messages on ResearchSphere"; r.Subject != want {
		t.Errorf("Subject = %q, want %q", r.Subject, want)
	}
	for _, part := range []struct{ name, body string }{{"text", r.Text}, {"html", r.HTML}} {
		for _, want := range []string{
			"Ada Lovelace", "12 unread messages", "Nafisa Hasan (3)", "CVS Paper Team (5)",
			"and 2 more conversations", "You have 2 message requests.",
			base + "/messaging", base + "/unsubscribe?token=abc.def",
		} {
			if !strings.Contains(part.body, want) {
				t.Errorf("%s part lacks %q:\n%s", part.name, want, part.body)
			}
		}
	}
	if want := base + "/api/v1/unsubscribe?token=abc.def"; r.ListUnsubscribeURL != want {
		t.Errorf("ListUnsubscribeURL = %q, want %q", r.ListUnsubscribeURL, want)
	}
}

func TestRenderSingularAndNothingExtra(t *testing.T) {
	d := domain.Digest{FullName: "Ada", Unread: 1, Top: []domain.DigestConversation{{Name: "Bo", Unread: 1}}, UnsubscribeToken: "t"}
	r, err := Render(d, base)
	if err != nil {
		t.Fatal(err)
	}
	if want := "You have 1 unread message on ResearchSphere"; r.Subject != want {
		t.Errorf("Subject = %q, want %q", r.Subject, want)
	}
	for _, body := range []string{r.Text, r.HTML} {
		if strings.Contains(body, "more conversation") || strings.Contains(body, "message request") {
			t.Errorf("mentions more conversations or requests that do not exist:\n%s", body)
		}
	}
	d.More, d.Requests = 1, 1
	r, _ = Render(d, base)
	for _, want := range []string{"and 1 more conversation", "You have 1 message request."} {
		if !strings.Contains(r.Text, want) {
			t.Errorf("text lacks %q", want)
		}
	}
}

func TestRenderEscapesNames(t *testing.T) {
	d := digest()
	d.FullName = "Ada\r\nBcc: x@evil.test"
	d.Top[0].Name = `<script>alert(1)</script><a href="https://evil.test">x</a>`
	r, err := Render(d, base)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(r.HTML, "<script>") || strings.Contains(r.HTML, `href="https://evil.test"`) {
		t.Errorf("HTML part has an unescaped name:\n%s", r.HTML)
	}
	if strings.Contains(r.Text, "\r\nBcc:") || strings.Contains(r.Text, "Ada\nBcc") {
		t.Errorf("a name added a line to the text:\n%s", r.Text)
	}
}

// The HTML loads nothing: no images, stylesheets, scripts or fonts, and links only to this site.
func TestRenderLoadsNothingRemote(t *testing.T) {
	r, err := Render(digest(), base)
	if err != nil {
		t.Fatal(err)
	}
	for _, banned := range []string{"<img", "<link", "<script", "@import", "url(", "<iframe", "background-image"} {
		if strings.Contains(strings.ToLower(r.HTML), banned) {
			t.Errorf("HTML contains %q", banned)
		}
	}
	for _, body := range []string{r.Text, r.HTML} {
		for _, u := range regexp.MustCompile(`https?://[^\s"'<>)]+`).FindAllString(body, -1) {
			if !strings.HasPrefix(u, base+"/") {
				t.Errorf("link outside the site: %q", u)
			}
		}
	}
}
