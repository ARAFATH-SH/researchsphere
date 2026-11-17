package usecase

import (
	"bytes"
	"errors"
	"slices"
	"testing"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/media/domain"
)

// FR-12.2: a figure is PNG, JPEG or PDF by its content, up to 5 MB; images are encoded again
// (EXIF dropped), PDFs kept as uploaded, everything else refused, WebP included.
func TestPrepareFigure(t *testing.T) {
	pdf := []byte("%PDF-1.5\n1 0 obj <<>> endobj\n")
	for _, tc := range []struct {
		name  string
		data  []byte
		want  string
		image bool
	}{
		{"png", append(slices.Clone(pngHead), 1, 2, 3), TypePNG, true},
		{"jpeg", append(slices.Clone(jpegHead), 1, 2, 3), TypeJPEG, true},
		{"pdf", pdf, TypePDF, false},
		{"webp", append(slices.Clone(webpHead), 1, 2, 3), "", false},
		{"docx", officeFile(t, "word/document.xml", docxMain, "word/document.xml"), "", false},
		{"renamed exe", append([]byte("MZ\x90\x00"), make([]byte, 64)...), "", false},
		{"svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`), "", false},
		{"eps", []byte("%!PS-Adobe-3.0 EPSF-3.0\n"), "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			re := &fakeReencoder{}
			a := newAttachments(&fakeCodec{size: domain.ImageSize{Width: 800, Height: 600}}, re)
			f, err := a.PrepareFigure(t.Context(), tc.data)
			if tc.want == "" {
				if !errors.Is(err, domain.ErrUnsupportedType) {
					t.Fatalf("err = %v, want ErrUnsupportedType", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if f.ContentType != tc.want {
				t.Errorf("type = %s, want %s", f.ContentType, tc.want)
			}
			if tc.image {
				// Encoded again in its own format, so the metadata is gone.
				if len(re.calls) != 1 || bytes.Equal(f.Data, tc.data) || f.Width != 800 {
					t.Errorf("image not encoded again: %v", re.calls)
				}
			} else if len(re.calls) != 0 || !bytes.Equal(f.Data, tc.data) {
				t.Errorf("a PDF must be stored as uploaded: %v", re.calls)
			}
		})
	}
}

func TestPrepareFigureLimits(t *testing.T) {
	re := &fakeReencoder{}
	a := newAttachments(&fakeCodec{size: domain.ImageSize{Width: 10, Height: 10}}, re)
	big := append([]byte("%PDF-"), make([]byte, MaxFigureBytes)...)
	if _, err := a.PrepareFigure(t.Context(), big); !errors.Is(err, domain.ErrFileTooLarge) {
		t.Errorf("over 5 MB: err = %v", err)
	}
	if _, err := a.PrepareFigure(t.Context(), big[:MaxFigureBytes]); err != nil {
		t.Errorf("exactly 5 MB: err = %v", err)
	}
	// An image that grows past 5 MB when encoded again.
	a = newAttachments(&fakeCodec{size: domain.ImageSize{Width: 10, Height: 10}}, &fakeReencoder{size: MaxFigureBytes + 1})
	if _, err := a.PrepareFigure(t.Context(), append(slices.Clone(pngHead), 1)); !errors.Is(err, domain.ErrFileTooLarge) {
		t.Errorf("grown: err = %v", err)
	}
	// The image header rules of attachments hold.
	a = newAttachments(&fakeCodec{size: domain.ImageSize{Width: 8001, Height: 10}}, re)
	if _, err := a.PrepareFigure(t.Context(), append(slices.Clone(jpegHead), 1)); !errors.Is(err, domain.ErrImageTooLarge) {
		t.Errorf("8001 px: err = %v", err)
	}
}
