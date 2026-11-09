package http

import (
	"context"
	"io"
	"net/url"
	"strings"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/messaging/domain"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/messaging/usecase"
)

// SendWithAttachments records the arguments as Send did, the attachment IDs joined last when
// there are any; a missing body is recorded as "<nil>".
func (f *fakeService) SendWithAttachments(_ context.Context, senderID, id string, body *string, cid string, ids []string) (domain.Sent, error) {
	b := "<nil>"
	if body != nil {
		b = *body
	}
	f.args = []any{senderID, id, b, cid}
	if len(ids) > 0 {
		f.args = append(f.args, strings.Join(ids, ","))
	}
	return f.sent, f.err
}

func (f *fakeService) UploadAttachment(_ context.Context, uploaderID, id, name string, r io.Reader) (domain.Upload, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return domain.Upload{}, err
	}
	f.args = []any{uploaderID, id, name, string(data)}
	return f.upload, f.err
}

func (f *fakeService) DownloadAttachment(_ context.Context, userID, id string) (usecase.Download, error) {
	f.args = []any{userID, id}
	return usecase.Download{URL: &url.URL{Scheme: "https", Host: "bucket.example", Path: "/k", RawQuery: "X-Amz-Signature=abc"}}, f.err
}

func (f *fakeService) DownloadReportedAttachment(_ context.Context, adminID, reportID, id string) (usecase.Download, error) {
	f.args = []any{adminID, reportID, id}
	return usecase.Download{URL: &url.URL{Scheme: "https", Host: "bucket.example", Path: "/r"}}, f.err
}
