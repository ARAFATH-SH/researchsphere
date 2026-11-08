package postgres

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/messaging/adapter/postgres/sqlcgen"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/messaging/domain"
)

// FindMessageSender finds nothing for a value that is not a UUID.
func (s *Store) FindMessageSender(ctx context.Context, conversationID, messageID string) (string, bool, error) {
	cid, ok1 := parseUUID(conversationID)
	mid, ok2 := parseUUID(messageID)
	if !ok1 || !ok2 {
		return "", false, nil
	}
	sender, err := s.q(ctx).FindMessageSender(ctx, sqlcgen.FindMessageSenderParams{ConversationID: cid, ID: mid})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("find message: %w", err)
	}
	return sender.String(), true, nil
}

// InsertReport stores the report. The usecase has read every ID from the database.
func (s *Store) InsertReport(ctx context.Context, r domain.NewReport) (string, time.Time, error) {
	cid, ok1 := parseUUID(r.ConversationID)
	reporter, ok2 := parseUUID(r.ReporterID)
	reported, ok3 := parseUUID(r.ReportedID)
	if !ok1 || !ok2 || !ok3 {
		return "", time.Time{}, fmt.Errorf("insert report: %w", domain.ErrNotFound)
	}
	arg := sqlcgen.InsertReportParams{ConversationID: cid, ReporterID: reporter, ReportedID: reported, Reason: string(r.Reason)}
	if r.MessageID != nil {
		mid, ok := parseUUID(*r.MessageID)
		if !ok {
			return "", time.Time{}, fmt.Errorf("insert report: %w", domain.ErrNotFound)
		}
		arg.MessageID = mid
	}
	if r.Details != nil {
		arg.Details = pgtype.Text{String: *r.Details, Valid: true}
	}
	row, err := s.q(ctx).InsertReport(ctx, arg)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("insert report: %w", err)
	}
	return row.ID.String(), row.CreatedAt.Time, nil
}

// ListReports reads the page and the total in one read-only snapshot, so they agree.
func (s *Store) ListReports(ctx context.Context, open bool, limit, offset int) ([]domain.Report, int, error) {
	items := []domain.Report{}
	var total int64
	err := pgx.BeginTxFunc(ctx, s.pool, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		q := sqlcgen.New(tx)
		rows, err := q.ListReports(ctx, sqlcgen.ListReportsParams{Open: open, Lim: clamp(limit), Off: clamp(offset)})
		if err != nil {
			return err
		}
		for _, r := range rows {
			items = append(items, report(sqlcgen.FindReportRow(r)))
		}
		total, err = q.CountReports(ctx, open)
		return err
	})
	if err != nil {
		return nil, 0, fmt.Errorf("list reports: %w", err)
	}
	return items, int(total), nil
}

// FindReport returns domain.ErrNotFound for a bad ID.
func (s *Store) FindReport(ctx context.Context, id string) (domain.Report, error) {
	rid, ok := parseUUID(id)
	if !ok {
		return domain.Report{}, domain.ErrNotFound
	}
	r, err := s.q(ctx).FindReport(ctx, rid)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Report{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Report{}, fmt.Errorf("find report: %w", err)
	}
	return report(r), nil
}

// ListReportedMessages finds nothing for a report that is not a UUID.
func (s *Store) ListReportedMessages(ctx context.Context, reportID, before string, limit int) ([]domain.ReportedMessage, error) {
	rid, ok := parseUUID(reportID)
	if !ok {
		return nil, nil
	}
	arg := sqlcgen.ListReportedMessagesParams{ReportID: rid, Lim: clamp(limit)}
	if before != "" {
		id, ok := parseUUID(before)
		if !ok {
			return nil, domain.ErrInvalidCursor
		}
		arg.HasBefore, arg.BeforeID = true, id
	}
	rows, err := s.q(ctx).ListReportedMessages(ctx, arg)
	if err != nil {
		return nil, fmt.Errorf("list reported messages: %w", err)
	}
	items := make([]domain.ReportedMessage, len(rows))
	for i, r := range rows {
		items[i] = domain.ReportedMessage{
			ID: r.ID.String(), ConversationID: r.ConversationID.String(), SenderID: r.SenderID.String(), CreatedAt: r.CreatedAt.Time,
			EditedAt: timePtr(r.EditedAt), DeletedAt: timePtr(r.DeletedAt), ChangedAfterReport: r.ChangedAfterReport,
		}
		if r.HasBodyAtReport {
			body := r.BodyAtReport
			items[i].Body = &body
		}
	}
	return items, nil
}

// ListReportedVersions skips IDs that are not UUIDs.
func (s *Store) ListReportedVersions(ctx context.Context, reportID string, messageIDs []string) ([]domain.MessageVersion, error) {
	rid, ok := parseUUID(reportID)
	if !ok {
		return nil, nil
	}
	ids := make([]pgtype.UUID, 0, len(messageIDs))
	for _, m := range messageIDs {
		if id, ok := parseUUID(m); ok {
			ids = append(ids, id)
		}
	}
	rows, err := s.q(ctx).ListReportedVersions(ctx, sqlcgen.ListReportedVersionsParams{ReportID: rid, MessageIds: ids})
	if err != nil {
		return nil, fmt.Errorf("list reported versions: %w", err)
	}
	out := make([]domain.MessageVersion, len(rows))
	for i, r := range rows {
		out[i] = domain.MessageVersion{
			MessageID: r.MessageID.String(), Body: r.Body, ShownFrom: r.ShownFrom.Time, ShownUntil: r.ShownUntil.Time,
			EndedBy: domain.VersionEnd(r.EndedBy),
		}
	}
	return out, nil
}

// AddReportView records the view. The caller found the report first.
func (s *Store) AddReportView(ctx context.Context, reportID, adminID string) error {
	rid, ok1 := parseUUID(reportID)
	admin, ok2 := parseUUID(adminID)
	if !ok1 || !ok2 {
		return fmt.Errorf("record report view: %w", domain.ErrNotFound)
	}
	if err := s.q(ctx).InsertReportView(ctx, sqlcgen.InsertReportViewParams{ReportID: rid, AdminID: admin}); err != nil {
		return fmt.Errorf("record report view: %w", err)
	}
	return nil
}

// ResolveReport resolves nothing for a report that is not a UUID.
func (s *Store) ResolveReport(ctx context.Context, id, adminID string, outcome domain.ReportOutcome, note *string) (bool, error) {
	rid, ok1 := parseUUID(id)
	admin, ok2 := parseUUID(adminID)
	if !ok1 || !ok2 {
		return false, nil
	}
	arg := sqlcgen.ResolveReportParams{ID: rid, AdminID: admin, Outcome: pgtype.Text{String: string(outcome), Valid: true}}
	if note != nil {
		arg.Note = pgtype.Text{String: *note, Valid: true}
	}
	n, err := s.q(ctx).ResolveReport(ctx, arg)
	if err != nil {
		return false, fmt.Errorf("resolve report: %w", err)
	}
	return n > 0, nil
}

func report(r sqlcgen.FindReportRow) domain.Report {
	out := domain.Report{
		ID: r.ID.String(), ConversationID: r.ConversationID.String(), Reason: domain.ReportReason(r.Reason),
		Details: textPtr(r.Details), MessageID: uuidPtr(r.MessageID), CreatedAt: r.CreatedAt.Time, Note: textPtr(r.ResolutionNote),
		Reporter: domain.ReportPerson{UserID: r.ReporterID.String(), FullName: r.ReporterName,
			Headline: textPtr(r.ReporterHeadline), PhotoFileID: uuidPtr(r.ReporterPhoto), Role: r.ReporterRole},
		Reported: domain.ReportPerson{UserID: r.ReportedID.String(), FullName: r.ReportedName,
			Headline: textPtr(r.ReportedHeadline), PhotoFileID: uuidPtr(r.ReportedPhoto), Role: r.ReportedRole},
	}
	if r.Outcome.Valid {
		o := domain.ReportOutcome(r.Outcome.String)
		out.Outcome = &o
	}
	if r.ResolvedAt.Valid {
		out.ResolvedAt = &r.ResolvedAt.Time
	}
	return out
}

// clamp keeps a page size or offset inside int32; the usecase bounds both.
func clamp(n int) int32 {
	return int32(min(n, math.MaxInt32)) //nolint:gosec // bounded by the min
}
