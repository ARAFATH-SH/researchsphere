package http

import (
	"context"
	"errors"
	"math"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/api/gen"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/messaging/domain"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/httpx"
)

var reportNotFound = gen.NotFoundJSONResponse{Code: httpx.CodeNotFound, Message: "This report does not exist."}

// badField is the bad_request of one body field; it never repeats the value.
func badField(field string) gen.Error {
	messages := map[string]string{
		"reason":  "Reason must be spam, harassment, inappropriate or other.",
		"details": "Details must have at most 1,000 characters.",
		"outcome": "Outcome must be dismissed or action_taken.",
		"note":    "The note must have at most 1,000 characters.",
	}
	return gen.Error{Code: httpx.CodeBadRequest, Message: messages[field], Field: &field}
}

// ReportConversation reports a conversation or one of its messages (FR-16.9).
func (h *Handler) ReportConversation(ctx context.Context, req gen.ReportConversationRequestObject) (gen.ReportConversationResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.ReportConversation401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	if req.Body == nil {
		return gen.ReportConversation400JSONResponse{Code: httpx.CodeBadRequest, Message: "The request is not valid."}, nil
	}
	in := domain.ReportInput{Reason: domain.ReportReason(req.Body.Reason), Details: req.Body.Details}
	if req.Body.MessageId != nil {
		m := req.Body.MessageId.String()
		in.MessageID = &m
	}
	r, err := h.svc.Report(ctx, id, req.ConversationId, in)
	var (
		field     *domain.InvalidFieldError
		limited   *domain.RateLimitedError
		suspended *domain.SuspendedError
	)
	switch {
	case errors.As(err, &suspended):
		return gen.ReportConversation403JSONResponse{AccountSuspendedOrForbiddenJSONResponse: gen.AccountSuspendedOrForbiddenJSONResponse(suspendedBody(suspended))}, nil
	case errors.Is(err, domain.ErrNotFound):
		return gen.ReportConversation404JSONResponse{NotFoundJSONResponse: notFound}, nil
	case errors.As(err, &field):
		return gen.ReportConversation400JSONResponse(badField(field.Field)), nil
	case errors.Is(err, domain.ErrOwnMessage):
		return gen.ReportConversation400JSONResponse{Code: CodeOwnMessage, Message: "You cannot report your own message."}, nil
	case errors.As(err, &limited):
		return gen.ReportConversation429JSONResponse{
			Body:    gen.Error{Code: CodeRateLimited, Message: "You have made 10 reports today. Try again later."},
			Headers: gen.ReportConversation429ResponseHeaders{RetryAfter: max(1, int(math.Ceil(limited.RetryAfter.Seconds())))},
		}, nil
	case err != nil:
		return nil, err
	}
	rid, err := uuid(r.ID)
	if err != nil {
		return nil, err
	}
	reported, err := uuid(r.ReportedID)
	if err != nil {
		return nil, err
	}
	return gen.ReportConversation201JSONResponse{Id: rid, CreatedAt: r.CreatedAt, ReportedUserId: reported, BlockedByMe: r.BlockedByMe}, nil
}

// ListMessageReports lists reports for an administrator; cmd/api has checked the role.
func (h *Handler) ListMessageReports(ctx context.Context, req gen.ListMessageReportsRequestObject) (gen.ListMessageReportsResponseObject, error) {
	if _, ok := httpx.UserID(ctx); !ok {
		return gen.ListMessageReports401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	page := 1
	if req.Params.Page != nil {
		page = *req.Params.Page
	}
	res, err := h.svc.ListReports(ctx, text(req.Params.Status), page)
	switch {
	case errors.Is(err, domain.ErrInvalidStatus):
		return gen.ListMessageReports400JSONResponse{Code: httpx.CodeBadRequest, Message: "Status must be open or resolved."}, nil
	case errors.Is(err, domain.ErrInvalidPage):
		return gen.ListMessageReports400JSONResponse{Code: httpx.CodeBadRequest, Message: "Page must be a whole number of at least 1."}, nil
	case err != nil:
		return nil, err
	}
	items := make([]gen.MessageReport, len(res.Items))
	for i, r := range res.Items {
		if items[i], err = reportBody(r); err != nil {
			return nil, err
		}
	}
	return gen.ListMessageReports200JSONResponse{Items: items, Page: res.Page, PageSize: res.PageSize, Total: res.Total}, nil
}

// GetMessageReport returns a report and its conversation up to the report (NFR-14).
func (h *Handler) GetMessageReport(ctx context.Context, req gen.GetMessageReportRequestObject) (gen.GetMessageReportResponseObject, error) {
	admin, ok := httpx.UserID(ctx)
	if !ok {
		return gen.GetMessageReport401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	d, err := h.svc.ReportDetail(ctx, admin, req.ReportId, text(req.Params.Before))
	switch {
	case errors.Is(err, domain.ErrNotFound):
		return gen.GetMessageReport404JSONResponse{NotFoundJSONResponse: reportNotFound}, nil
	case errors.Is(err, domain.ErrInvalidCursor):
		return gen.GetMessageReport400JSONResponse{Code: httpx.CodeBadRequest, Message: "Before must be a message ID."}, nil
	case err != nil:
		return nil, err
	}
	report, err := reportBody(d.Report)
	if err != nil {
		return nil, err
	}
	messages := make([]gen.Message, len(d.Messages))
	for i, m := range d.Messages {
		if messages[i], err = messageBody(m); err != nil {
			return nil, err
		}
	}
	return gen.GetMessageReport200JSONResponse{Report: report, Messages: messages, NextBefore: d.NextBefore}, nil
}

// ResolveMessageReport closes a report with an outcome (FR-16.9).
func (h *Handler) ResolveMessageReport(ctx context.Context, req gen.ResolveMessageReportRequestObject) (gen.ResolveMessageReportResponseObject, error) {
	admin, ok := httpx.UserID(ctx)
	if !ok {
		return gen.ResolveMessageReport401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	if req.Body == nil {
		return gen.ResolveMessageReport400JSONResponse{Code: httpx.CodeBadRequest, Message: "The request is not valid."}, nil
	}
	r, err := h.svc.ResolveReport(ctx, admin, req.ReportId, domain.ReportOutcome(req.Body.Outcome), req.Body.Note)
	var field *domain.InvalidFieldError
	switch {
	case errors.As(err, &field):
		return gen.ResolveMessageReport400JSONResponse(badField(field.Field)), nil
	case errors.Is(err, domain.ErrNotFound):
		return gen.ResolveMessageReport404JSONResponse{NotFoundJSONResponse: reportNotFound}, nil
	case errors.Is(err, domain.ErrAlreadyResolved):
		return gen.ResolveMessageReport409JSONResponse{Code: CodeAlreadyResolved, Message: "This report is already resolved."}, nil
	case err != nil:
		return nil, err
	}
	b, err := reportBody(r)
	if err != nil {
		return nil, err
	}
	return gen.ResolveMessageReport200JSONResponse(b), nil
}

func reportBody(r domain.Report) (gen.MessageReport, error) {
	id, err := uuid(r.ID)
	if err != nil {
		return gen.MessageReport{}, err
	}
	conversation, err := uuid(r.ConversationID)
	if err != nil {
		return gen.MessageReport{}, err
	}
	message, err := optionalUUID(r.MessageID)
	if err != nil {
		return gen.MessageReport{}, err
	}
	reporter, err := reportPerson(r.Reporter)
	if err != nil {
		return gen.MessageReport{}, err
	}
	reported, err := reportPerson(r.Reported)
	if err != nil {
		return gen.MessageReport{}, err
	}
	b := gen.MessageReport{
		Id: id, ConversationId: conversation, Status: gen.MessageReportStatus(domain.ReportOpen),
		Reason: gen.ReportReason(r.Reason), Details: r.Details, MessageId: message,
		Reporter: reporter, ReportedUser: reported, CreatedAt: r.CreatedAt, Note: r.Note, ResolvedAt: r.ResolvedAt,
	}
	if r.ResolvedAt != nil {
		b.Status = gen.MessageReportStatus(domain.ReportResolved)
	}
	if r.Outcome != nil {
		o := gen.MessageReportOutcome(*r.Outcome)
		b.Outcome = &o
	}
	return b, nil
}

func reportPerson(p domain.ReportPerson) (gen.MessageReportPerson, error) {
	id, err := uuid(p.UserID)
	if err != nil {
		return gen.MessageReportPerson{}, err
	}
	return gen.MessageReportPerson{UserId: id, FullName: p.FullName, Headline: p.Headline, PhotoUrl: mediaURL(p.PhotoFileID)}, nil
}
