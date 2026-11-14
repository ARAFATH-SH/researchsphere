// Package email renders the unread message digest (FR-16.11) as a plain-text and an HTML
// email. It shows only what domain.Digest holds: counts, the names of people and groups, and
// links to this site. Never message text, attachment names or message IDs (NFR-13). The HTML
// has inline styles only: no images, remote stylesheets, fonts or tracking.
package email

import (
	"bytes"
	"embed"
	"fmt"
	htmltemplate "html/template"
	"net/url"
	"strings"
	texttemplate "text/template"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/messaging/domain"
)

//go:embed templates
var files embed.FS

var (
	textTmpl = texttemplate.Must(texttemplate.ParseFS(files, "templates/digest.txt"))
	htmlTmpl = htmltemplate.Must(htmltemplate.ParseFS(files, "templates/digest.html"))
)

// Rendered is a digest ready to send.
type Rendered struct {
	Subject string
	Text    string
	HTML    string
	// ListUnsubscribeURL is the one-click unsubscribe of RFC 8058: the API operation.
	ListUnsubscribeURL string
}

// line is one conversation the digest names.
type line struct {
	Name  string
	Count int
}

// view is what the templates see.
type view struct {
	FullName       string
	Headline       string
	Lines          []line
	More           string
	Requests       string
	MessagingURL   string
	UnsubscribeURL string
}

// Render renders d with links under appBaseURL (APP_BASE_URL, such as https://researchsphere.org).
func Render(d domain.Digest, appBaseURL string) (Rendered, error) {
	base := strings.TrimRight(appBaseURL, "/")
	q := url.Values{"token": {d.UnsubscribeToken}}.Encode()
	v := view{
		FullName:       oneLine(d.FullName),
		Headline:       headline(d.Unread),
		MessagingURL:   base + "/messaging",
		UnsubscribeURL: base + "/unsubscribe?" + q,
	}
	for _, c := range d.Top {
		name := oneLine(c.Name)
		if name == "" {
			name = "A conversation"
		}
		v.Lines = append(v.Lines, line{Name: name, Count: c.Unread})
	}
	switch {
	case d.More == 1:
		v.More = "and 1 more conversation"
	case d.More > 1:
		v.More = fmt.Sprintf("and %d more conversations", d.More)
	}
	switch {
	case d.Requests == 1:
		v.Requests = "You have 1 message request."
	case d.Requests > 1:
		v.Requests = fmt.Sprintf("You have %d message requests.", d.Requests)
	}
	var text, html bytes.Buffer
	if err := textTmpl.Execute(&text, v); err != nil {
		return Rendered{}, fmt.Errorf("render digest text: %w", err)
	}
	if err := htmlTmpl.Execute(&html, v); err != nil {
		return Rendered{}, fmt.Errorf("render digest html: %w", err)
	}
	return Rendered{
		Subject: "You have " + headline(d.Unread) + " on ResearchSphere",
		Text:    text.String(), HTML: html.String(),
		ListUnsubscribeURL: base + "/api/v1/unsubscribe?" + q,
	}, nil
}

// headline is "1 unread message" or "N unread messages".
func headline(n int) string {
	if n == 1 {
		return "1 unread message"
	}
	return fmt.Sprintf("%d unread messages", n)
}

// oneLine removes line breaks and repeated spaces, so a name cannot add lines to the text.
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
