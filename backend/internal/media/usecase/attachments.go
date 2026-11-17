package usecase

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"io"
	"path"
	"strings"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/media/domain"
)

// Rules of message attachments (FR-16.7, docs/decisions.md 2026-11-09).
const (
	// MaxAttachmentBytes is the largest attachment, as uploaded and as stored.
	MaxAttachmentBytes = 25 << 20
	// AttachmentJPEGQuality is the quality an attached JPEG (or opaque WebP) is encoded with:
	// higher than a profile photo's, as the file is the point of the message.
	AttachmentJPEGQuality = 90
	// maxZipEntries and maxContentTypesBytes bound what is read of an Office file.
	maxZipEntries        = 10000
	maxContentTypesBytes = 1 << 20
)

// Content types of the accepted attachments.
const (
	TypePDF  = "application/pdf"
	TypePNG  = "image/png"
	TypeJPEG = "image/jpeg"
	TypeDOCX = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	TypeXLSX = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	TypePPTX = "application/vnd.openxmlformats-officedocument.presentationml.presentation"
)

// officeMain maps the content type of an Office Open XML package's main part to the type of
// the file. Only these three are accepted: the macro-enabled main parts
// (application/vnd.ms-word.document.macroEnabled.main+xml and its kin), templates and slide
// shows have other content types and are refused.
var officeMain = map[string]struct {
	contentType string
	extensions  []string
}{
	"application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml":   {TypeDOCX, []string{".docx"}},
	"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml":         {TypeXLSX, []string{".xlsx"}},
	"application/vnd.openxmlformats-officedocument.presentationml.presentation.main+xml": {TypePPTX, []string{".pptx"}},
}

// Attachments checks and prepares message attachments. Its images share the decoding slots
// of the profile images, so at most maxRenders images are decoded at once in all.
type Attachments struct {
	images    *Images
	reencoder domain.ImageReencoder
}

// NewAttachments returns an Attachments that decodes images with the codec of images.
func NewAttachments(images *Images, reencoder domain.ImageReencoder) *Attachments {
	return &Attachments{images: images, reencoder: reencoder}
}

// Prepare checks a file by its content and returns what to store (FR-16.7). The name and the
// content type the client sent are never used. Accepted: PDF, PNG, JPEG, still WebP, DOCX,
// XLSX and PPTX. An image is encoded again at its own size, so EXIF, GPS and anything appended
// is dropped; a document is stored as it is. Errors: ErrFileTooLarge (over MaxAttachmentBytes,
// also after an image is encoded again), ErrUnsupportedType, ErrImageTooLarge and
// ErrInvalidImage.
func (a *Attachments) Prepare(ctx context.Context, data []byte) (domain.PreparedFile, error) {
	if len(data) > MaxAttachmentBytes {
		return domain.PreparedFile{}, domain.ErrFileTooLarge
	}
	if format, ok := sniff(data); ok {
		return a.prepareImage(ctx, data, format)
	}
	switch {
	case bytes.HasPrefix(data, []byte("%PDF-")):
		return domain.PreparedFile{Data: data, ContentType: TypePDF, Extensions: []string{".pdf"}}, nil
	case bytes.HasPrefix(data, []byte("PK\x03\x04")):
		ct, ext, err := officeType(data)
		if err != nil {
			return domain.PreparedFile{}, err
		}
		return domain.PreparedFile{Data: data, ContentType: ct, Extensions: ext}, nil
	}
	// Everything else, the legacy Office formats (OLE, D0 CF 11 E0) and programs among them.
	return domain.PreparedFile{}, domain.ErrUnsupportedType
}

func (a *Attachments) prepareImage(ctx context.Context, data []byte, format domain.Format) (domain.PreparedFile, error) {
	return a.reencodeImage(ctx, data, format, MaxAttachmentBytes)
}

// reencodeImage checks an image's header, then encodes it again at its own size, turned
// upright by its EXIF Orientation, so that nothing but its pixels is kept. The result may be
// at most limit bytes (domain.ErrFileTooLarge).
func (a *Attachments) reencodeImage(ctx context.Context, data []byte, format domain.Format, limit int) (domain.PreparedFile, error) {
	size, err := a.images.codec.Size(bytes.NewReader(data), format)
	if err != nil {
		return domain.PreparedFile{}, err
	}
	if size.Width <= 0 || size.Height <= 0 {
		return domain.PreparedFile{}, domain.ErrInvalidImage
	}
	if size.Width > MaxImageSide || size.Height > MaxImageSide {
		return domain.PreparedFile{}, domain.ErrImageTooLarge
	}
	select {
	case a.images.renders <- struct{}{}:
	case <-ctx.Done():
		return domain.PreparedFile{}, ctx.Err()
	}
	out, outFormat, err := a.reencoder.Reencode(bytes.NewReader(data), format, AttachmentJPEGQuality)
	<-a.images.renders
	if err != nil {
		return domain.PreparedFile{}, err
	}
	if len(out) > limit {
		return domain.PreparedFile{}, domain.ErrFileTooLarge
	}
	f := domain.PreparedFile{Data: out, Width: size.Width, Height: size.Height}
	if outFormat == domain.FormatPNG {
		f.ContentType, f.Extensions = TypePNG, []string{".png"}
	} else {
		f.ContentType, f.Extensions = TypeJPEG, []string{".jpg", ".jpeg"}
	}
	return f, nil
}

// contentTypes is the part of [Content_Types].xml that names each part's type.
type contentTypes struct {
	Overrides []struct {
		PartName    string `xml:"PartName,attr"`
		ContentType string `xml:"ContentType,attr"`
	} `xml:"Override"`
}

// officeType reads an Office Open XML package: [Content_Types].xml must name exactly one main
// part of an accepted type, that part must exist, and no part may hold macros
// (vbaProject.bin). Anything else is ErrUnsupportedType: a plain ZIP, a macro-enabled file, a
// template, a damaged package.
func officeType(data []byte) (string, []string, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil || len(zr.File) == 0 || len(zr.File) > maxZipEntries {
		return "", nil, domain.ErrUnsupportedType
	}
	var ctFile *zip.File
	names := make(map[string]bool, len(zr.File))
	for _, f := range zr.File {
		name := strings.ToLower(f.Name)
		names[name] = true
		if name == "[content_types].xml" {
			ctFile = f
		}
		if path.Base(name) == "vbaproject.bin" || strings.HasSuffix(name, ".bin") && strings.Contains(name, "vba") {
			return "", nil, domain.ErrUnsupportedType
		}
	}
	if ctFile == nil || ctFile.UncompressedSize64 > maxContentTypesBytes {
		return "", nil, domain.ErrUnsupportedType
	}
	rc, err := ctFile.Open()
	if err != nil {
		return "", nil, domain.ErrUnsupportedType
	}
	defer func() { _ = rc.Close() }()
	raw, err := io.ReadAll(io.LimitReader(rc, maxContentTypesBytes+1))
	if err != nil || len(raw) > maxContentTypesBytes {
		return "", nil, domain.ErrUnsupportedType
	}
	var types contentTypes
	if err := xml.Unmarshal(raw, &types); err != nil {
		return "", nil, domain.ErrUnsupportedType
	}

	var (
		found      string
		extensions []string
		mains      int
	)
	for _, o := range types.Overrides {
		ct := strings.ToLower(strings.TrimSpace(o.ContentType))
		if strings.Contains(ct, "macroenabled") || strings.Contains(ct, "vbaproject") {
			return "", nil, domain.ErrUnsupportedType
		}
		if !strings.HasSuffix(ct, ".main+xml") {
			continue
		}
		mains++
		kind, ok := officeMain[ct]
		if !ok {
			return "", nil, domain.ErrUnsupportedType
		}
		if !names[strings.ToLower(strings.TrimPrefix(o.PartName, "/"))] {
			return "", nil, domain.ErrUnsupportedType
		}
		found, extensions = kind.contentType, kind.extensions
	}
	if mains != 1 {
		return "", nil, domain.ErrUnsupportedType
	}
	return found, extensions, nil
}
