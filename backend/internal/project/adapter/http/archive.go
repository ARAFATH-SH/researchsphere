package http

import (
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/api/gen"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/httpx"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/project/domain"
)

// archiveDeadline is how long writing an archive may take: 768 MiB at about 1 MB/s.
const archiveDeadline = 15 * time.Minute

// DownloadProjectArchive streams the project's files as a ZIP archive (FR-13.7).
func (h *Handler) DownloadProjectArchive(ctx context.Context, req gen.DownloadProjectArchiveRequestObject) (gen.DownloadProjectArchiveResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.DownloadProjectArchive401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	a, err := h.files.PrepareArchive(ctx, id, req.ProjectId)
	switch e := classifyFile(err); {
	case errors.Is(err, domain.ErrArchiveTooLarge):
		return gen.DownloadProjectArchive413JSONResponse{Code: CodeArchiveTooLarge,
			Message: "This project is too large to download as one archive."}, nil
	case e.status == 404, e.status == 403:
		// Reading files has no 403: a member may always read them.
		return gen.DownloadProjectArchive404JSONResponse{NotFoundJSONResponse: notFound}, nil
	case e.status == 429:
		return gen.DownloadProjectArchive429JSONResponse{
			ProjectFileRateLimitedJSONResponse: e.rateLimited("You have downloaded 10 archives this hour. Try again later."),
		}, nil
	case err != nil:
		return nil, err
	}
	return archiveResponse{ctx: ctx, archive: a, files: h.files}, nil
}

// archiveResponse writes the ZIP as it reads the files, so no archive is held in memory.
type archiveResponse struct {
	ctx     context.Context //nolint:containedctx // the request's, used while the body is written
	archive domain.Archive
	files   FileService
}

func (r archiveResponse) VisitDownloadProjectArchiveResponse(w http.ResponseWriter) error {
	h := http.Header{}
	h.Set("Content-Type", "application/zip")
	h.Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": r.archive.Name}))
	h.Set("Cache-Control", "private, no-store")
	h.Set("X-Content-Type-Options", "nosniff")
	return httpx.Stream(w, archiveDeadline, h, func(out io.Writer) error {
		return r.files.WriteArchive(r.ctx, r.archive, out)
	})
}
