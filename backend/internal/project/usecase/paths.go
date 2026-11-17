package usecase

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/project/domain"
)

// Rules of project files (FR-12.1, FR-12.2, docs/decisions.md 2026-11-17). The database holds
// the same path, size and count rules (migration 000035); these give the API its answers.
const (
	MaxPathLength    = 255
	MaxPathSegments  = 8
	MaxSegmentLength = 100
	// MaxFiles is the most entries a project holds, folders included.
	MaxFiles = 200
	// MaxTextBytes is the largest text file, in bytes of UTF-8.
	MaxTextBytes = 1 << 20
	// DefaultFigureMaxBytes and DefaultStorageMaxBytes are the figure limits when the
	// settings give none (FR-12.2; FIGURE_MAX_BYTES, PROJECT_STORAGE_MAX_BYTES).
	DefaultFigureMaxBytes  = 50 << 20
	DefaultStorageMaxBytes = 500 << 20
	// MainFileName is the file every new project starts with (FR-10.4).
	MainFileName = "main.tex"
)

// Content types of figures.
const (
	TypePNG  = "image/png"
	TypeJPEG = "image/jpeg"
	TypePDF  = "application/pdf"
)

// figureExtensions maps a figure's extension, in lower case, to its content type.
var figureExtensions = map[string]string{".png": TypePNG, ".jpg": TypeJPEG, ".jpeg": TypeJPEG, ".pdf": TypePDF}

// CleanPath trims the path and checks it against the path rules: segments of letters,
// digits, '_', '.' and '-' that do not start with '.', so no '.' or '..' and no spaces, 1 to
// MaxSegmentLength characters each, at most MaxPathSegments of them and MaxPathLength in all,
// with no leading, trailing or double '/'. Otherwise domain.ErrInvalidPath.
func CleanPath(p string) (string, error) {
	p = strings.TrimSpace(p)
	if p == "" || len(p) > MaxPathLength {
		return "", domain.ErrInvalidPath
	}
	segments := strings.Split(p, "/")
	if len(segments) > MaxPathSegments {
		return "", domain.ErrInvalidPath
	}
	for _, s := range segments {
		if !validSegment(s) {
			return "", domain.ErrInvalidPath
		}
	}
	return p, nil
}

func validSegment(s string) bool {
	if s == "" || len(s) > MaxSegmentLength || s[0] == '.' || s[0] == '-' {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '.', r == '-':
		default:
			return false
		}
	}
	return true
}

// figureType is the content type a path's extension names, false when it names none.
func figureType(p string) (string, bool) {
	i := strings.LastIndexByte(p, '.')
	if i < 0 || strings.Contains(p[i:], "/") {
		return "", false
	}
	ct, ok := figureExtensions[strings.ToLower(p[i:])]
	return ct, ok
}

// parentPaths returns the folders on the way to p, outermost first: "a", "a/b" for "a/b/c".
func parentPaths(p string) []string {
	var out []string
	for i := 0; i < len(p); i++ {
		if p[i] == '/' {
			out = append(out, p[:i])
		}
	}
	return out
}

// under reports whether p is base or below it, ignoring letter case.
func under(p, base string) bool {
	lp, lb := strings.ToLower(p), strings.ToLower(base)
	return lp == lb || strings.HasPrefix(lp, lb+"/")
}

// baseName is the last segment of p.
func baseName(p string) string {
	return p[strings.LastIndexByte(p, '/')+1:]
}

// checkText refuses text that is not valid UTF-8 or holds control characters other than
// tab, line feed and carriage return (a *domain.FieldError on "content"), then text over
// MaxTextBytes (domain.ErrFileTooLarge).
func checkText(content string) error {
	if !utf8.ValidString(content) {
		return &domain.FieldError{Field: "content"}
	}
	for _, r := range content {
		if unicode.Is(unicode.Cc, r) && r != '\t' && r != '\n' && r != '\r' {
			return &domain.FieldError{Field: "content"}
		}
	}
	if len(content) > MaxTextBytes {
		return domain.ErrFileTooLarge
	}
	return nil
}
