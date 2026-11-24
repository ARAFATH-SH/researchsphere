// Package http implements the venue operations of docs/openapi.yaml (FR-14, FR-15.1). cmd/api
// checks the platform action before a request reaches these handlers ("View profiles,
// search, feed, events and recommendations" to read, "Add, edit and remove venues" to write).
package http

import (
	"context"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"time"

	"github.com/oapi-codegen/nullable"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/api/gen"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/event/domain"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/event/usecase"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/httpx"
)

// Error codes of the venue operations.
const (
	CodeInvalidImportFile = "invalid_import_file"
	CodeTooManyRows       = "too_many_rows"
	CodeFileTooLarge      = "file_too_large"
)

// formField is the multipart part that holds an import file.
const formField = "file"

// Service is what the handlers need from the usecase.
type Service interface {
	Fields(ctx context.Context) ([]domain.Field, error)
	List(ctx context.Context, in usecase.ListInput) (domain.Page, int, int, error)
	Get(ctx context.Context, id string) (domain.Venue, error)
	Create(ctx context.Context, adminID string, in usecase.Input) (domain.Venue, error)
	Update(ctx context.Context, adminID, id string, p usecase.Patch) (domain.Venue, error)
	Remove(ctx context.Context, adminID, id, reason string) error
	Import(ctx context.Context, adminID, filename string, r io.Reader, dryRun bool) (usecase.ImportResult, error)
}

// Handler serves the venue operations.
type Handler struct {
	svc Service
	now func() time.Time
}

// NewHandler returns a Handler. now is the clock that says whether a deadline has passed;
// nil means time.Now.
func NewHandler(svc Service, now func() time.Time) *Handler {
	if now == nil {
		now = time.Now
	}
	return &Handler{svc: svc, now: now}
}

var (
	unauthenticated = gen.UnauthorizedJSONResponse{Code: httpx.CodeUnauthenticated, Message: "Log in to continue."}
	notFound        = gen.NotFoundJSONResponse{Code: httpx.CodeNotFound, Message: "This venue does not exist."}
	fileTooLarge    = gen.FileTooLargeJSONResponse{Code: CodeFileTooLarge, Message: "An import file may be at most 1 MiB."}
)

// messages says, per problem code, what rule a field breaks. The value is never repeated.
var messages = map[string]string{
	domain.CodeRequired:         "This field is required.",
	domain.CodeTooLong:          "This field is too long.",
	domain.CodeTooShort:         "This field is too short.",
	domain.CodeInvalid:          "This field is not valid.",
	domain.CodeNotHTTPS:         "The link must start with https://.",
	domain.CodeUnknownField:     "This is not a research field of the list.",
	domain.CodeDuplicateField:   "A research field is given twice.",
	domain.CodeTooManyFields:    "A venue may have at most 5 research fields.",
	domain.CodeNotAllowed:       "This field does not apply to this type of venue or attendance.",
	domain.CodeInvalidTimeZone:  "The time zone must be AoE, UTC or an IANA name such as Asia/Dhaka.",
	domain.CodeInvalidDate:      "The date is not valid; use YYYY-MM-DD or YYYY-MM-DDTHH:MM.",
	domain.CodeEndsBeforeStarts: "The end date is before the start date.",
	domain.CodeAfterDeadline:    "The abstract deadline is after the submission deadline.",
	domain.CodeBeforeDeadline:   "The notification date is before the submission deadline.",
}

// invalid is the 400 body of the first problem.
func invalid(err *domain.InvalidError) gen.InvalidVenueJSONResponse {
	if len(err.Problems) == 0 {
		return gen.InvalidVenueJSONResponse{Code: httpx.CodeBadRequest, Message: "The request is not valid."}
	}
	p := err.Problems[0]
	msg, ok := messages[p.Code]
	if !ok {
		msg = "This field is not valid."
	}
	field := p.Field
	return gen.InvalidVenueJSONResponse{Code: httpx.CodeBadRequest, Message: msg, Field: &field}
}

// ListResearchFields returns the fixed list (FR-14.3).
func (h *Handler) ListResearchFields(ctx context.Context, _ gen.ListResearchFieldsRequestObject) (gen.ListResearchFieldsResponseObject, error) {
	if _, ok := httpx.UserID(ctx); !ok {
		return gen.ListResearchFields401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	fields, err := h.svc.Fields(ctx)
	if err != nil {
		return nil, err
	}
	body := gen.ListResearchFields200JSONResponse{Items: make([]gen.ResearchField, len(fields))}
	for i, f := range fields {
		body.Items[i] = gen.ResearchField{Key: f.Key, Label: f.Label}
	}
	return body, nil
}

// ListVenues returns one page of venues with the facets (FR-14.1 to FR-14.3).
func (h *Handler) ListVenues(ctx context.Context, req gen.ListVenuesRequestObject) (gen.ListVenuesResponseObject, error) {
	if _, ok := httpx.UserID(ctx); !ok {
		return gen.ListVenues401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	p := req.Params
	in := usecase.ListInput{
		IncludePast: deref(p.IncludePast), DeadlineFrom: deref(p.DeadlineFrom), DeadlineTo: deref(p.DeadlineTo),
		TZ: deref(p.Tz), Q: deref(p.Q),
	}
	if p.Page != nil {
		in.Page = *p.Page
		if in.Page == 0 {
			in.Page = -1 // 0 is refused, not the default
		}
	}
	if p.PageSize != nil {
		in.PageSize = *p.PageSize
		if in.PageSize == 0 {
			in.PageSize = -1
		}
	}
	if p.Type != nil {
		in.Types = *p.Type
	}
	if p.Field != nil {
		in.Fields = *p.Field
	}
	page, num, size, err := h.svc.List(ctx, in)
	var bad *domain.InvalidError
	switch {
	case errors.As(err, &bad):
		return gen.ListVenues400JSONResponse{InvalidVenueJSONResponse: invalid(bad)}, nil
	case err != nil:
		return nil, err
	}
	now := h.now()
	body := gen.ListVenues200JSONResponse{
		Items: make([]gen.VenueSummary, len(page.Items)), Page: num, PageSize: size, Total: page.Total,
		Facets: gen.VenueFacets{Types: make([]gen.VenueTypeCount, len(page.TypeCounts)), Fields: make([]gen.VenueFieldCount, len(page.FieldCounts))},
	}
	for i, v := range page.Items {
		if body.Items[i], err = summaryOf(v, now); err != nil {
			return nil, err
		}
	}
	for i, c := range page.TypeCounts {
		body.Facets.Types[i] = gen.VenueTypeCount{Type: gen.VenueType(c.Type), Count: c.Count}
	}
	for i, c := range page.FieldCounts {
		body.Facets.Fields[i] = gen.VenueFieldCount{Key: c.Field.Key, Label: c.Field.Label, Count: c.Count}
	}
	return body, nil
}

// GetVenue returns a live venue with its scope (FR-14.4).
func (h *Handler) GetVenue(ctx context.Context, req gen.GetVenueRequestObject) (gen.GetVenueResponseObject, error) {
	if _, ok := httpx.UserID(ctx); !ok {
		return gen.GetVenue401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	v, err := h.svc.Get(ctx, req.VenueId)
	switch {
	case errors.Is(err, domain.ErrNotFound):
		return gen.GetVenue404JSONResponse{NotFoundJSONResponse: notFound}, nil
	case err != nil:
		return nil, err
	}
	body, err := venueOf(v, h.now())
	if err != nil {
		return nil, err
	}
	return gen.GetVenue200JSONResponse(body), nil
}

// CreateVenue adds a venue (FR-15.1).
func (h *Handler) CreateVenue(ctx context.Context, req gen.CreateVenueRequestObject) (gen.CreateVenueResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.CreateVenue401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	if req.Body == nil {
		return gen.CreateVenue400JSONResponse{InvalidVenueJSONResponse: gen.InvalidVenueJSONResponse{
			Code: httpx.CodeBadRequest, Message: "The request body is required."}}, nil
	}
	b := req.Body
	v, err := h.svc.Create(ctx, id, usecase.Input{
		Name: b.Name, ShortName: deref(b.ShortName), Type: b.Type, Fields: b.Fields, Attendance: deref(b.Attendance),
		City: deref(b.City), Country: deref(b.Country), StartsOn: deref(b.StartsOn), EndsOn: deref(b.EndsOn),
		TimeZone: b.TimeZone, SubmissionDeadline: b.SubmissionDeadline, AbstractDeadline: deref(b.AbstractDeadline),
		NotificationAt: deref(b.NotificationAt), OfficialURL: b.OfficialUrl, Scope: b.Scope,
	})
	var bad *domain.InvalidError
	switch {
	case errors.As(err, &bad):
		return gen.CreateVenue400JSONResponse{InvalidVenueJSONResponse: invalid(bad)}, nil
	case err != nil:
		return nil, err
	}
	body, err := venueOf(v, h.now())
	if err != nil {
		return nil, err
	}
	return gen.CreateVenue201JSONResponse(body), nil
}

// UpdateVenue changes the fields the body names (FR-15.1).
func (h *Handler) UpdateVenue(ctx context.Context, req gen.UpdateVenueRequestObject) (gen.UpdateVenueResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.UpdateVenue401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	if req.Body == nil {
		return gen.UpdateVenue400JSONResponse{InvalidVenueJSONResponse: gen.InvalidVenueJSONResponse{
			Code: httpx.CodeBadRequest, Message: "The request body is required."}}, nil
	}
	b := req.Body
	p := usecase.Patch{
		Name: opt(b.Name), ShortName: opt(b.ShortName), Type: opt(b.Type), Attendance: opt(b.Attendance),
		City: opt(b.City), Country: opt(b.Country), StartsOn: opt(b.StartsOn), EndsOn: opt(b.EndsOn),
		TimeZone: opt(b.TimeZone), SubmissionDeadline: opt(b.SubmissionDeadline),
		AbstractDeadline: opt(b.AbstractDeadline), NotificationAt: opt(b.NotificationAt),
		OfficialURL: opt(b.OfficialUrl), Scope: opt(b.Scope),
	}
	if b.Fields.IsSpecified() {
		p.FieldsSet = true
		if b.Fields.IsNull() {
			p.FieldsNull = true
		} else {
			p.Fields = b.Fields.MustGet()
		}
	}
	v, err := h.svc.Update(ctx, id, req.VenueId, p)
	var bad *domain.InvalidError
	switch {
	case errors.As(err, &bad):
		return gen.UpdateVenue400JSONResponse{InvalidVenueJSONResponse: invalid(bad)}, nil
	case errors.Is(err, domain.ErrNotFound):
		return gen.UpdateVenue404JSONResponse{NotFoundJSONResponse: notFound}, nil
	case err != nil:
		return nil, err
	}
	body, err := venueOf(v, h.now())
	if err != nil {
		return nil, err
	}
	return gen.UpdateVenue200JSONResponse(body), nil
}

// RemoveVenue hides a venue for good, with a reason (FR-15.1).
func (h *Handler) RemoveVenue(ctx context.Context, req gen.RemoveVenueRequestObject) (gen.RemoveVenueResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.RemoveVenue401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	reason := ""
	if req.Body != nil {
		reason = req.Body.Reason
	}
	err := h.svc.Remove(ctx, id, req.VenueId, reason)
	var bad *domain.InvalidError
	switch {
	case errors.As(err, &bad):
		return gen.RemoveVenue400JSONResponse{InvalidVenueJSONResponse: invalid(bad)}, nil
	case errors.Is(err, domain.ErrNotFound):
		return gen.RemoveVenue404JSONResponse{NotFoundJSONResponse: notFound}, nil
	case err != nil:
		return nil, err
	}
	return gen.RemoveVenue204Response{}, nil
}

// ImportVenues adds and updates venues from a file (FR-14.5, interim).
func (h *Handler) ImportVenues(ctx context.Context, req gen.ImportVenuesRequestObject) (gen.ImportVenuesResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.ImportVenues401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	if req.Body == nil {
		return badMultipart(), nil
	}
	part, err := filePart(req.Body)
	if err != nil {
		if isTooLarge(err) {
			return gen.ImportVenues413JSONResponse{FileTooLargeJSONResponse: fileTooLarge}, nil
		}
		return badMultipart(), nil
	}
	res, err := h.svc.Import(ctx, id, part.FileName(), part, deref(req.Params.DryRun))
	var (
		fileErr *domain.ImportFileError
		rowsErr *domain.TooManyRowsError
	)
	switch {
	case errors.Is(err, domain.ErrFileTooLarge) || isTooLarge(err):
		return gen.ImportVenues413JSONResponse{FileTooLargeJSONResponse: fileTooLarge}, nil
	case errors.As(err, &fileErr):
		return gen.ImportVenues400JSONResponse{Code: CodeInvalidImportFile, Message: fileErr.Message}, nil
	case errors.As(err, &rowsErr):
		return gen.ImportVenues400JSONResponse{Code: CodeTooManyRows, Message: "An import file may hold at most 1,000 venues."}, nil
	case err != nil && isBodyError(err):
		return badMultipart(), nil
	case err != nil:
		return nil, err
	}
	body := gen.ImportVenues200JSONResponse{
		DryRun: res.DryRun, Created: res.Created, Updated: res.Updated, Unchanged: res.Unchanged, Rejected: res.Rejected,
		Rows: make([]gen.VenueImportRow, len(res.Rows)),
	}
	for i, r := range res.Rows {
		row := gen.VenueImportRow{Row: r.Row, Outcome: gen.VenueImportRowOutcome(r.Outcome), SourceKey: r.SourceKey,
			Reasons: make([]gen.VenueImportReason, len(r.Problems))}
		for j, p := range r.Problems {
			row.Reasons[j] = gen.VenueImportReason{Field: p.Field, Code: p.Code}
		}
		if r.VenueID != "" {
			u, err := uuidOf(r.VenueID)
			if err != nil {
				return nil, err
			}
			row.VenueId = &u
		}
		body.Rows[i] = row
	}
	return body, nil
}

func badMultipart() gen.ImportVenues400JSONResponse {
	return gen.ImportVenues400JSONResponse{Code: httpx.CodeBadRequest, Message: "Send the file as multipart form data in a part named file."}
}

// filePart returns the part named formField, skipping any other parts.
func filePart(body *multipart.Reader) (*multipart.Part, error) {
	for {
		p, err := body.NextPart()
		if err != nil {
			return nil, err
		}
		if p.FormName() == formField {
			return p, nil
		}
	}
}

// isTooLarge says whether reading the body went over the cap cmd/api sets for the route.
func isTooLarge(err error) bool {
	_, ok := errors.AsType[*http.MaxBytesError](err)
	return ok
}

// isBodyError says whether the multipart body itself could not be read.
func isBodyError(err error) bool {
	return errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, multipart.ErrMessageTooLarge)
}

// summaryOf is a venue as the list shows it.
func summaryOf(v domain.Venue, now time.Time) (gen.VenueSummary, error) {
	id, err := uuidOf(v.ID)
	if err != nil {
		return gen.VenueSummary{}, err
	}
	loc, err := usecase.LoadZone(v.TimeZone)
	if err != nil {
		loc = time.UTC
	}
	s := gen.VenueSummary{
		Id: id, Name: v.Name, ShortName: optString(v.ShortName), Type: gen.VenueType(v.Type),
		Fields: make([]gen.ResearchField, len(v.Fields)), City: optString(v.City), Country: optString(v.Country),
		StartsOn: optDate(v.StartsOn), EndsOn: optDate(v.EndsOn), TimeZone: v.TimeZone,
		SubmissionDeadline: venueTime(v.SubmissionDeadline, loc), AbstractDeadline: optVenueTime(v.AbstractDeadline, loc),
		NotificationAt: optVenueTime(v.NotificationAt, loc), OfficialUrl: v.OfficialURL, Past: v.Past(now),
	}
	for i, f := range v.Fields {
		s.Fields[i] = gen.ResearchField{Key: f.Key, Label: f.Label}
	}
	if v.Attendance != "" {
		a := gen.VenueAttendance(v.Attendance)
		s.Attendance = &a
	}
	return s, nil
}

// venueOf is a venue with its scope.
func venueOf(v domain.Venue, now time.Time) (gen.Venue, error) {
	s, err := summaryOf(v, now)
	if err != nil {
		return gen.Venue{}, err
	}
	return gen.Venue{
		Id: s.Id, Name: s.Name, ShortName: s.ShortName, Type: s.Type, Fields: s.Fields, Attendance: s.Attendance,
		City: s.City, Country: s.Country, StartsOn: s.StartsOn, EndsOn: s.EndsOn, TimeZone: s.TimeZone,
		SubmissionDeadline: s.SubmissionDeadline, AbstractDeadline: s.AbstractDeadline, NotificationAt: s.NotificationAt,
		OfficialUrl: s.OfficialUrl, Past: s.Past, Scope: v.Scope, Source: gen.VenueSource(v.Source),
		CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt,
	}, nil
}

func venueTime(t time.Time, loc *time.Location) gen.VenueTime {
	return gen.VenueTime{At: t.UTC(), Local: usecase.FormatLocal(t, loc)}
}

func optVenueTime(t time.Time, loc *time.Location) *gen.VenueTime {
	if t.IsZero() {
		return nil
	}
	vt := venueTime(t, loc)
	return &vt
}

func optDate(t time.Time) *openapi_types.Date {
	if t.IsZero() {
		return nil
	}
	return &openapi_types.Date{Time: t}
}

func optString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func opt(n nullable.Nullable[string]) usecase.Opt {
	switch {
	case !n.IsSpecified():
		return usecase.Opt{}
	case n.IsNull():
		return usecase.Opt{Set: true, Null: true}
	}
	return usecase.Opt{Set: true, Value: n.MustGet()}
}

func deref[T any](p *T) T {
	var zero T
	if p == nil {
		return zero
	}
	return *p
}

func uuidOf(s string) (openapi_types.UUID, error) {
	var u openapi_types.UUID
	err := u.UnmarshalText([]byte(s))
	return u, err
}
