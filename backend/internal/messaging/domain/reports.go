package domain

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// ReportReason is why a user reports a conversation or message (FR-16.9).
type ReportReason string

// Reasons.
const (
	ReasonSpam          ReportReason = "spam"
	ReasonHarassment    ReportReason = "harassment"
	ReasonInappropriate ReportReason = "inappropriate"
	ReasonOther         ReportReason = "other"
)

// ReportOutcome is how an administrator closed a report.
type ReportOutcome string

// Outcomes.
const (
	OutcomeDismissed   ReportOutcome = "dismissed"
	OutcomeActionTaken ReportOutcome = "action_taken"
)

// ReportStatus is a list of reports an administrator can ask for.
type ReportStatus string

// Statuses.
const (
	ReportOpen     ReportStatus = "open"
	ReportResolved ReportStatus = "resolved"
)

// ReportInput is what a member sends to report a conversation. A nil MessageID reports the
// conversation as a whole.
type ReportInput struct {
	Reason    ReportReason
	Details   *string
	MessageID *string
}

// NewReport is a validated report, ready to store.
type NewReport struct {
	ConversationID string
	ReporterID     string
	ReportedID     string
	MessageID      *string
	Reason         ReportReason
	Details        *string
}

// ReportReceipt is what the reporter gets back. BlockedByMe says whether the reporter has
// blocked ReportedID: reporting never does it, and the client can offer it next.
type ReportReceipt struct {
	ID          string
	CreatedAt   time.Time
	ReportedID  string
	BlockedByMe bool
}

// ReportPerson is the reporter or the reported user as an administrator sees them.
type ReportPerson struct {
	UserID      string
	FullName    string
	Headline    *string
	PhotoFileID *string
	// Role is the platform role, so the client can leave out actions an administrator cannot
	// be given. The moderation module still refuses them (FR-15.10).
	Role string
}

// Report is a stored report. CreatedAt is its moment: an administrator sees the
// conversation's messages only up to it (NFR-14). ResolvedAt is nil while it is open.
type Report struct {
	ID             string
	ConversationID string
	Reason         ReportReason
	Details        *string
	MessageID      *string
	Reporter       ReportPerson
	Reported       ReportPerson
	CreatedAt      time.Time
	Outcome        *ReportOutcome
	Note           *string
	ResolvedAt     *time.Time
}

// ReportPage is one page of reports.
type ReportPage struct {
	Items    []Report
	Page     int
	PageSize int
	Total    int // reports with this status on all pages
}

// ReportDetail is a report and one page of its conversation, newest first. NextBefore is nil
// on the last page.
type ReportDetail struct {
	Report     Report
	Messages   []Message
	NextBefore *string
}

// ReportStore reads and writes reports. There is deliberately no way to read messages by
// conversation here: ListReportedMessages starts from a report (NFR-14).
type ReportStore interface {
	// FindMessageSender returns who sent the message; found is false when it is not in the
	// conversation, also for a value that is not a UUID.
	FindMessageSender(ctx context.Context, conversationID, messageID string) (senderID string, found bool, err error)
	// InsertReport stores r and returns its ID and moment.
	InsertReport(ctx context.Context, r NewReport) (id string, createdAt time.Time, err error)
	// ListReports returns one page of open or resolved reports, newest first, and the total
	// of that status, read in one snapshot.
	ListReports(ctx context.Context, open bool, limit, offset int) ([]Report, int, error)
	// FindReport returns ErrNotFound when there is none, also for a value that is not a UUID.
	FindReport(ctx context.Context, id string) (Report, error)
	// ListReportedMessages returns the report's conversation newest first, none later than
	// the report, before the message ID before when it is not empty.
	ListReportedMessages(ctx context.Context, reportID, before string, limit int) ([]Message, error)
	// AddReportView records that adminID read the report's conversation.
	AddReportView(ctx context.Context, reportID, adminID string) error
	// ResolveReport closes an open report. resolved is false when no open report has the ID.
	ResolveReport(ctx context.Context, id, adminID string, outcome ReportOutcome, note *string) (resolved bool, err error)
}

// Errors of reports.
var (
	ErrOwnMessage      = errors.New("messaging: the message is the reporter's own")
	ErrAlreadyResolved = errors.New("messaging: the report is already resolved")
	// ErrReportAboutOther refuses a moderation action on one user that names a report about
	// another (FR-15.8).
	ErrReportAboutOther = errors.New("messaging: the report is about another user")
	ErrInvalidStatus    = errors.New("messaging: unknown report status")
	ErrInvalidPage      = errors.New("messaging: page is not a whole number of at least 1")
)

// InvalidFieldError says a body field of a report or a resolution is not valid. Field is its
// name in the contract.
type InvalidFieldError struct {
	Field string
}

func (e *InvalidFieldError) Error() string {
	return fmt.Sprintf("messaging: %s is not valid", e.Field)
}
