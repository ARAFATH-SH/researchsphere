package usecase

import (
	"context"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/messaging/domain"
)

// Limits on reports (FR-16.9, docs/decisions.md).
const (
	// ReportsPerDay is how many reports a user may make in a day.
	ReportsPerDay = 10
	// MaxReportText is the longest report details or resolution note, in characters.
	MaxReportText = 1000
	// ReportPageSize is the size of a page of reports.
	ReportPageSize = 20
	// ReportMessagePageSize is how many messages of a reported conversation a page holds.
	ReportMessagePageSize = 100
)

// Report stores a member's report of a conversation or one of its messages (FR-16.9). Checks
// run in this order: ErrNotFound when reporterID is not a member (NFR-14), *InvalidFieldError
// for the reason or details, ErrNotFound for a message outside the conversation,
// ErrOwnMessage, *SuspendedError, *RateLimitedError. Only stored reports count. It blocks
// nobody. A restricted user may still report.
func (s *Service) Report(ctx context.Context, reporterID, conversationID string, in domain.ReportInput) (domain.ReportReceipt, error) {
	c, err := s.store.FindConversation(ctx, conversationID, reporterID)
	if err != nil {
		return domain.ReportReceipt{}, err
	}
	switch in.Reason {
	case domain.ReasonSpam, domain.ReasonHarassment, domain.ReasonInappropriate, domain.ReasonOther:
	default:
		return domain.ReportReceipt{}, &domain.InvalidFieldError{Field: "reason"}
	}
	details, err := reportText(in.Details, "details")
	if err != nil {
		return domain.ReportReceipt{}, err
	}
	if in.MessageID != nil {
		sender, found, err := s.store.FindMessageSender(ctx, conversationID, *in.MessageID)
		switch {
		case err != nil:
			return domain.ReportReceipt{}, err
		case !found:
			return domain.ReportReceipt{}, domain.ErrNotFound
		case sender == reporterID:
			return domain.ReportReceipt{}, domain.ErrOwnMessage
		}
	}
	if err := s.refuseSuspended(ctx, reporterID); err != nil {
		return domain.ReportReceipt{}, err
	}
	if err := s.checkLimits(ctx, reporterID, s.reports); err != nil {
		return domain.ReportReceipt{}, err
	}
	var (
		id string
		at time.Time
	)
	err = s.tx.InTx(ctx, func(ctx context.Context) error {
		if err := s.refuseSuspended(ctx, reporterID); err != nil {
			return err
		}
		var err error
		id, at, err = s.store.InsertReport(ctx, domain.NewReport{
			ConversationID: conversationID, ReporterID: reporterID, ReportedID: c.OtherID,
			MessageID: in.MessageID, Reason: in.Reason, Details: details,
		})
		return err
	})
	if err != nil {
		return domain.ReportReceipt{}, err
	}
	s.record(ctx, reporterID, s.reports)
	state, err := s.blocks.Between(ctx, reporterID, c.OtherID)
	if err != nil {
		return domain.ReportReceipt{}, err
	}
	return domain.ReportReceipt{ID: id, CreatedAt: at, ReportedID: c.OtherID, BlockedByMe: state.ByMe}, nil
}

// ListReports returns one page of reports for an administrator, who has already passed the
// role check. An empty status means open; anything else but resolved is ErrInvalidStatus. A
// page below 1 is ErrInvalidPage. It holds no messages, so it records no view.
func (s *Service) ListReports(ctx context.Context, status string, page int) (domain.ReportPage, error) {
	open := true
	switch domain.ReportStatus(status) {
	case "", domain.ReportOpen:
	case domain.ReportResolved:
		open = false
	default:
		return domain.ReportPage{}, domain.ErrInvalidStatus
	}
	if page < 1 || page-1 > math.MaxInt32/ReportPageSize {
		return domain.ReportPage{}, domain.ErrInvalidPage
	}
	items, total, err := s.store.ListReports(ctx, open, ReportPageSize, (page-1)*ReportPageSize)
	if err != nil {
		return domain.ReportPage{}, err
	}
	return domain.ReportPage{Items: items, Page: page, PageSize: ReportPageSize, Total: total}, nil
}

// ReportDetail returns a report and one page of its conversation up to the report's moment
// (NFR-14). before is the previous page's NextBefore, or empty; anything but a UUID is
// ErrInvalidCursor. The view is recorded in the transaction of the read, so nothing is read
// without its audit row. ErrNotFound when there is no such report.
func (s *Service) ReportDetail(ctx context.Context, adminID, id, before string) (domain.ReportDetail, error) {
	if before != "" && !isUUID(before) {
		return domain.ReportDetail{}, domain.ErrInvalidCursor
	}
	var d domain.ReportDetail
	err := s.tx.InTx(ctx, func(ctx context.Context) error {
		r, err := s.store.FindReport(ctx, id)
		if err != nil {
			return err
		}
		if err := s.store.AddReportView(ctx, id, adminID); err != nil {
			return err
		}
		items, err := s.store.ListReportedMessages(ctx, id, before, ReportMessagePageSize+1)
		if err != nil {
			return err
		}
		d = domain.ReportDetail{Report: r, Messages: items}
		if len(items) > ReportMessagePageSize {
			d.Messages = items[:ReportMessagePageSize]
			last := d.Messages[ReportMessagePageSize-1].ID
			d.NextBefore = &last
		}
		return s.attachVersions(ctx, id, d.Messages)
	})
	if err != nil {
		return domain.ReportDetail{}, err
	}
	return d, nil
}

// attachVersions fills each message's Versions, oldest first, as the store returns them.
func (s *Service) attachVersions(ctx context.Context, reportID string, messages []domain.ReportedMessage) error {
	if len(messages) == 0 {
		return nil
	}
	ids := make([]string, len(messages))
	at := make(map[string]int, len(messages))
	for i, m := range messages {
		ids[i], at[m.ID] = m.ID, i
	}
	versions, err := s.store.ListReportedVersions(ctx, reportID, ids)
	if err != nil {
		return err
	}
	for _, v := range versions {
		if i, ok := at[v.MessageID]; ok {
			messages[i].Versions = append(messages[i].Versions, v)
		}
	}
	return nil
}

// ResolveReport closes an open report (FR-16.9) and returns it. *InvalidFieldError for the
// outcome or note, ErrNotFound for no such report, ErrAlreadyResolved when it is closed.
func (s *Service) ResolveReport(ctx context.Context, adminID, id string, outcome domain.ReportOutcome, note *string) (domain.Report, error) {
	switch outcome {
	case domain.OutcomeDismissed, domain.OutcomeActionTaken:
	default:
		return domain.Report{}, &domain.InvalidFieldError{Field: "outcome"}
	}
	text, err := reportText(note, "note")
	if err != nil {
		return domain.Report{}, err
	}
	resolved, err := s.store.ResolveReport(ctx, id, adminID, outcome, text)
	if err != nil {
		return domain.Report{}, err
	}
	r, err := s.store.FindReport(ctx, id)
	if err != nil {
		return domain.Report{}, err
	}
	if !resolved {
		return domain.Report{}, domain.ErrAlreadyResolved
	}
	return r, nil
}

// ResolveForAction resolves a report as action_taken because a moderation action on userID
// names it (FR-15.8), in the transaction the context carries. ErrNotFound for no such report,
// ErrReportAboutOther when it is about someone else, ErrAlreadyResolved when it is closed.
func (s *Service) ResolveForAction(ctx context.Context, adminID, id, userID string, note *string) error {
	r, err := s.store.FindReport(ctx, id)
	if err != nil {
		return err
	}
	if r.Reported.UserID != userID {
		return domain.ErrReportAboutOther
	}
	resolved, err := s.store.ResolveReport(ctx, id, adminID, domain.OutcomeActionTaken, note)
	if err != nil {
		return err
	}
	if !resolved {
		return domain.ErrAlreadyResolved
	}
	return nil
}

// reportText trims an optional text and checks it; empty means none. PostgreSQL cannot store
// a NUL.
func reportText(s *string, field string) (*string, error) {
	if s == nil {
		return nil, nil
	}
	if !utf8.ValidString(*s) || strings.ContainsRune(*s, 0) {
		return nil, &domain.InvalidFieldError{Field: field}
	}
	text := strings.TrimSpace(*s)
	switch n := utf8.RuneCountInString(text); {
	case n > MaxReportText:
		return nil, &domain.InvalidFieldError{Field: field}
	case n == 0:
		return nil, nil
	}
	return &text, nil
}
