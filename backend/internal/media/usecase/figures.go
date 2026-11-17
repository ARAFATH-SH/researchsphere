package usecase

import (
	"bytes"
	"context"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/media/domain"
)

// MaxFigureBytes is the largest project figure, as uploaded and as stored (FR-12.2).
const MaxFigureBytes = 5 << 20

// PrepareFigure checks a project figure by its content and returns what to store (FR-12.2,
// docs/decisions.md 2026-11-17): PNG or JPEG, encoded again at its own size so EXIF, GPS and
// anything appended are dropped (a PNG stays a PNG, a JPEG a JPEG), or a PDF stored as it
// is. WebP is refused, as pdfLaTeX cannot include it. The name and content type the client
// sent are never used. Errors: ErrFileTooLarge (over MaxFigureBytes, also after an image is
// encoded again), ErrUnsupportedType, ErrImageTooLarge and ErrInvalidImage.
func (a *Attachments) PrepareFigure(ctx context.Context, data []byte) (domain.PreparedFile, error) {
	if len(data) > MaxFigureBytes {
		return domain.PreparedFile{}, domain.ErrFileTooLarge
	}
	if format, ok := sniff(data); ok {
		if format != domain.FormatPNG && format != domain.FormatJPEG {
			return domain.PreparedFile{}, domain.ErrUnsupportedType
		}
		return a.reencodeImage(ctx, data, format, MaxFigureBytes)
	}
	if bytes.HasPrefix(data, []byte("%PDF-")) {
		return domain.PreparedFile{Data: data, ContentType: TypePDF, Extensions: []string{".pdf"}}, nil
	}
	return domain.PreparedFile{}, domain.ErrUnsupportedType
}
