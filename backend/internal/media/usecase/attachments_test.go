package usecase

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"slices"
	"testing"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/media/domain"
)

// fakeReencoder returns a fixed image in the format it is told to answer with, and records
// what it was asked.
type fakeReencoder struct {
	out   map[domain.Format]domain.Format // the output format for each input; the input itself when missing
	size  int                             // bytes of the output, 16 when 0
	err   error
	calls []string
}

func (r *fakeReencoder) Reencode(_ io.Reader, f domain.Format, quality int) ([]byte, domain.Format, error) {
	r.calls = append(r.calls, fmt.Sprintf("%s q%d", f, quality))
	if r.err != nil {
		return nil, "", r.err
	}
	out := f
	if o, ok := r.out[f]; ok {
		out = o
	}
	n := r.size
	if n == 0 {
		n = 16
	}
	return make([]byte, n), out, nil
}

func newAttachments(codec *fakeCodec, re *fakeReencoder) *Attachments {
	f := newImagesFixture()
	f.svc.codec = codec
	return NewAttachments(f.svc, re)
}

// officeFile builds an Office Open XML package: [Content_Types].xml names mainType for
// mainPart, and every part in parts is stored.
func officeFile(t *testing.T, mainPart, mainType string, parts ...string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	ct := `<?xml version="1.0" encoding="UTF-8"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
<Default Extension="xml" ContentType="application/xml"/>
<Override PartName="/` + mainPart + `" ContentType="` + mainType + `"/>
<Override PartName="/docProps/core.xml" ContentType="application/vnd.openxmlformats-package.core-properties+xml"/>
</Types>`
	if mainType != "" {
		add(t, w, "[Content_Types].xml", ct)
	}
	for _, p := range parts {
		add(t, w, p, "<x/>")
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func add(t *testing.T, w *zip.Writer, name, body string) {
	t.Helper()
	f, err := w.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte(body)); err != nil {
		t.Fatal(err)
	}
}

const (
	docxMain = "application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"
	xlsxMain = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"
	pptxMain = "application/vnd.openxmlformats-officedocument.presentationml.presentation.main+xml"
)

// FR-16.7: the type comes from the content only. Every accepted type, and the refused ones the
// SRS names: a renamed program, macro-enabled and legacy Office files.
func TestPrepareAttachmentTypes(t *testing.T) {
	pdf := []byte("%PDF-1.7\n%âãÏÓ\n1 0 obj <<>> endobj\n")
	for _, tc := range []struct {
		name  string
		data  []byte
		want  string // content type; empty when refused
		exts  []string
		image bool
	}{
		{"pdf", pdf, TypePDF, []string{".pdf"}, false},
		{"docx", officeFile(t, "word/document.xml", docxMain, "word/document.xml"), TypeDOCX, []string{".docx"}, false},
		{"xlsx", officeFile(t, "xl/workbook.xml", xlsxMain, "xl/workbook.xml"), TypeXLSX, []string{".xlsx"}, false},
		{"pptx", officeFile(t, "ppt/presentation.xml", pptxMain, "ppt/presentation.xml"), TypePPTX, []string{".pptx"}, false},
		{"jpeg", append(slices.Clone(jpegHead), 1, 2, 3), TypeJPEG, []string{".jpg", ".jpeg"}, true},
		{"png", append(slices.Clone(pngHead), 1, 2, 3), TypePNG, []string{".png"}, true},
		{"still webp", append(slices.Clone(webpHead), 1, 2, 3), TypeJPEG, []string{".jpg", ".jpeg"}, true},
		{"webp with alpha", append(slices.Clone(stillVP8X), 1, 2, 3), TypePNG, []string{".png"}, true},

		{"renamed exe", append([]byte("MZ\x90\x00\x03\x00\x00\x00"), make([]byte, 64)...), "", nil, false},
		{"elf", []byte("\x7fELF\x02\x01\x01"), "", nil, false},
		{"docm", officeFile(t, "word/document.xml", "application/vnd.ms-word.document.macroEnabled.main+xml", "word/document.xml"), "", nil, false},
		{"xlsm", officeFile(t, "xl/workbook.xml", "application/vnd.ms-excel.sheet.macroEnabled.main+xml", "xl/workbook.xml"), "", nil, false},
		{"pptm", officeFile(t, "ppt/presentation.xml", "application/vnd.ms-powerpoint.presentation.macroEnabled.main+xml", "ppt/presentation.xml"), "", nil, false},
		{"docx with vbaProject.bin", officeFile(t, "word/document.xml", docxMain, "word/document.xml", "word/vbaProject.bin"), "", nil, false},
		{"dotx template", officeFile(t, "word/document.xml", "application/vnd.openxmlformats-officedocument.wordprocessingml.template.main+xml", "word/document.xml"), "", nil, false},
		{"docx without its main part", officeFile(t, "word/document.xml", docxMain), "", nil, false},
		{"zip without content types", officeFile(t, "", "", "a.txt"), "", nil, false},
		{"broken zip", []byte("PK\x03\x04 not really"), "", nil, false},
		{"legacy doc or xls (OLE)", append([]byte{0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1}, make([]byte, 512)...), "", nil, false},
		{"animated webp", append(slices.Clone(animatedWebP), 1, 2, 3), "", nil, false},
		{"plain text named .pdf", []byte("just text"), "", nil, false},
		{"html", []byte("<html><script>alert(1)</script>"), "", nil, false},
		{"pdf not at the start", append([]byte("junk"), pdf...), "", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			re := &fakeReencoder{out: map[domain.Format]domain.Format{domain.FormatWebP: domain.FormatJPEG}}
			if tc.name == "webp with alpha" {
				re.out[domain.FormatWebP] = domain.FormatPNG
			}
			a := newAttachments(&fakeCodec{size: domain.ImageSize{Width: 640, Height: 480}}, re)
			f, err := a.Prepare(t.Context(), tc.data)
			if tc.want == "" {
				if !errors.Is(err, domain.ErrUnsupportedType) {
					t.Fatalf("err = %v, want ErrUnsupportedType", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if f.ContentType != tc.want || !slices.Equal(f.Extensions, tc.exts) {
				t.Errorf("got %s %v, want %s %v", f.ContentType, f.Extensions, tc.want, tc.exts)
			}
			if tc.image {
				if len(re.calls) != 1 || bytes.Equal(f.Data, tc.data) || f.Width != 640 || f.Height != 480 {
					t.Errorf("image not encoded again: %v, %dx%d", re.calls, f.Width, f.Height)
				}
			} else if len(re.calls) != 0 || !bytes.Equal(f.Data, tc.data) {
				t.Errorf("a document must be stored as uploaded: %v", re.calls)
			}
		})
	}
}

// FR-16.7: 25 MB, and the image rules of 10A.2 (header checked first, 8000 pixels a side).
func TestPrepareAttachmentLimits(t *testing.T) {
	re := &fakeReencoder{}
	a := newAttachments(&fakeCodec{size: domain.ImageSize{Width: 8001, Height: 10}}, re)
	big := append([]byte("%PDF-"), make([]byte, MaxAttachmentBytes)...)
	if _, err := a.Prepare(t.Context(), big); !errors.Is(err, domain.ErrFileTooLarge) {
		t.Errorf("over 25 MB: %v", err)
	}
	if _, err := a.Prepare(t.Context(), big[:MaxAttachmentBytes]); err != nil {
		t.Errorf("exactly 25 MB: %v", err)
	}
	if _, err := a.Prepare(t.Context(), append(slices.Clone(pngHead), 0)); !errors.Is(err, domain.ErrImageTooLarge) || len(re.calls) != 0 {
		t.Errorf("8001 px: %v, decoded %v", err, re.calls)
	}
	a = newAttachments(&fakeCodec{sizeErr: domain.ErrInvalidImage}, re)
	if _, err := a.Prepare(t.Context(), append(slices.Clone(pngHead), 0)); !errors.Is(err, domain.ErrInvalidImage) {
		t.Errorf("bad header: %v", err)
	}
	// A re-encoded image that grows past the limit is refused, not stored.
	a = newAttachments(&fakeCodec{size: domain.ImageSize{Width: 10, Height: 10}}, &fakeReencoder{size: MaxAttachmentBytes + 1})
	if _, err := a.Prepare(t.Context(), append(slices.Clone(pngHead), 0)); !errors.Is(err, domain.ErrFileTooLarge) {
		t.Errorf("grown image: %v", err)
	}
	a = newAttachments(&fakeCodec{size: domain.ImageSize{Width: 10, Height: 10}}, &fakeReencoder{err: domain.ErrInvalidImage})
	if _, err := a.Prepare(t.Context(), append(slices.Clone(jpegHead), 0)); !errors.Is(err, domain.ErrInvalidImage) {
		t.Errorf("undecodable: %v", err)
	}
}
