package sandbox

import (
	"archive/tar"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
)

// Limits of the tar the API sends, beside the input size: the API's own rules (at most 200
// entries per project, paths of at most 255 characters) with room to spare.
const (
	maxEntries = 400
	maxName    = 255
)

// ErrBadInput refuses a tar entry the API would never send: not a plain file or folder, a
// path that is absolute, climbs with "..", or starts a name with ".".
var ErrBadInput = errors.New("sandbox: unexpected entry in the project's files")

// ErrInputTooLarge refuses files over the input limit.
var ErrInputTooLarge = errors.New("sandbox: the project's files are too large")

// Sanitize copies the tar from in to out, entry by entry, refusing anything but folders and
// regular files at clean relative paths, and more than maxBytes of file content (0: no
// limit). Only the fields it checks are copied: no owners, modes, links or extended headers.
// The API builds this tar from paths the database checked; this is defense in depth, since
// the container's tar extracts whatever it is given.
func Sanitize(in io.Reader, out io.Writer, maxBytes int64) error {
	tr := tar.NewReader(in)
	tw := tar.NewWriter(out)
	var total int64
	for n := 0; ; n++ {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return tw.Close()
		}
		if err != nil {
			return fmt.Errorf("read the project's files: %w", err)
		}
		if n >= maxEntries {
			return fmt.Errorf("%w: more than %d entries", ErrBadInput, maxEntries)
		}
		name := strings.TrimSuffix(h.Name, "/")
		if !safeName(name) {
			return fmt.Errorf("%w: %q", ErrBadInput, h.Name)
		}
		switch h.Typeflag {
		case tar.TypeDir:
			if err := tw.WriteHeader(&tar.Header{Typeflag: tar.TypeDir, Name: name + "/", Mode: 0o755, ModTime: h.ModTime}); err != nil {
				return err
			}
		case tar.TypeReg:
			total += h.Size
			if maxBytes > 0 && total > maxBytes {
				return ErrInputTooLarge
			}
			if err := tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: name, Mode: 0o644, Size: h.Size, ModTime: h.ModTime}); err != nil {
				return err
			}
			if _, err := io.CopyN(tw, tr, h.Size); err != nil {
				return err
			}
		default:
			return fmt.Errorf("%w: %q is not a file or a folder", ErrBadInput, h.Name)
		}
	}
}

func safeName(name string) bool {
	if name == "" || len(name) > maxName || strings.HasPrefix(name, "/") || path.Clean(name) != name {
		return false
	}
	for _, seg := range strings.Split(name, "/") {
		if seg == "" || seg == ".." || strings.HasPrefix(seg, ".") || strings.ContainsAny(seg, "\\\x00") {
			return false
		}
	}
	return true
}
