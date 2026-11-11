package storage

import (
	"net/url"
	"strings"
	"unicode"
)

// ContentDisposition returns an "attachment" Content-Disposition for filename (RFC 6266): a
// plain ASCII filename for old clients, with anything else replaced by "_", and the exact
// name in filename* (RFC 8187, UTF-8 percent-encoded). Quotes, backslashes and control
// characters never reach the header.
func ContentDisposition(filename string) string {
	if filename == "" {
		filename = "download"
	}
	var ascii strings.Builder
	for _, r := range filename {
		switch {
		case r > unicode.MaxASCII || r < 0x20 || r == 0x7f || r == '"' || r == '\\' || r == '%' || r == ';':
			ascii.WriteByte('_')
		default:
			ascii.WriteRune(r)
		}
	}
	clean := strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, filename)
	return `attachment; filename="` + ascii.String() + `"; filename*=UTF-8''` + strings.ReplaceAll(url.QueryEscape(clean), "+", "%20")
}
