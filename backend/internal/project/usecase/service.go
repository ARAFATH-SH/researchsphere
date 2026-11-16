// Package usecase holds the project module's business logic: creating, reading, editing,
// listing, searching and deleting research projects (FR-10.1 to FR-10.3, FR-10.6), and
// reporting them (FR-15.11).
package usecase

import (
	"context"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/project/domain"
)

// Limits and page sizes.
const (
	// CreatesPerWindow projects a user may create in CreateWindow (decisions.md 2026-11-16).
	CreatesPerWindow = 10
	CreateWindow     = 24 * time.Hour
	// SearchesPerWindow project searches a user may run in SearchWindow, as people search.
	SearchesPerWindow = 60
	SearchWindow      = time.Minute

	MinQueryLength  = 2
	MaxQueryLength  = 100 // keyword mode (FR-10.3)
	MaxMeaningQuery = 300 // auto mode (AI-5.1)
	SearchPageSize  = 20  // FR-10.3
	MaxSearchPage   = 50
	DefaultPageSize = 20
	MaxPageSize     = 50 // the server's cap on every list
	ReportPageSize  = 20
)

// Deps is what the Service needs. The fields up to SearchLimiter are required. Without an
// Embedder every auto search falls back to keywords (AI-5.4); without a VectorCache every
// query is embedded; AITimeout 0 means DefaultSearchAITimeout; without a Watcher nobody is
// told about changes.
type Deps struct {
	Store         domain.Store
	Tx            domain.Transactor
	Permissions   domain.Permissions
	Actors        domain.Actors
	CreateLimiter domain.Limiter
	SearchLimiter domain.Limiter
	Embedder      domain.QueryEmbedder
	VectorCache   domain.QueryVectorCache
	AITimeout     time.Duration
	Watcher       domain.Watcher
	Logger        *slog.Logger
}

// Service runs the project operations.
type Service struct {
	d  Deps
	ai searchAI
}

// New returns a Service.
func New(d Deps) *Service {
	if d.Logger == nil {
		d.Logger = slog.New(slog.DiscardHandler)
	}
	if d.AITimeout <= 0 {
		d.AITimeout = DefaultSearchAITimeout
	}
	if d.Watcher == nil {
		d.Watcher = noWatcher{}
	}
	return &Service{d: d}
}

// noWatcher is the Watcher of a Service made without one.
type noWatcher struct{}

func (noWatcher) ProjectChanged(context.Context, string)            {}
func (noWatcher) MembersChanged(context.Context, string, ...string) {}

// CreateInput is a new project as the caller sends it.
type CreateInput struct {
	Title       string
	Description string
	Skills      []string
	Abstract    *string
}

// Create stores a project with the caller as its owner (FR-10.1). It returns a
// *domain.FieldError for a field that breaks a rule and *domain.RateLimitedError after
// CreatesPerWindow projects in CreateWindow; only created projects count. When the limit
// cannot be read, the project is created.
func (s *Service) Create(ctx context.Context, userID string, in CreateInput) (domain.Project, error) {
	np, err := validCreate(userID, in)
	if err != nil {
		return domain.Project{}, err
	}
	used, left, err := s.d.CreateLimiter.Used(ctx, userID)
	switch {
	case err != nil:
		s.d.Logger.WarnContext(ctx, "project create limit unavailable, allowing it", slog.Any("error", err))
	case used >= CreatesPerWindow:
		return domain.Project{}, &domain.RateLimitedError{RetryAfter: left}
	}
	var id string
	err = s.d.Tx.InTx(ctx, func(ctx context.Context) error {
		var err error
		if id, err = s.d.Store.Create(ctx, np); err != nil {
			return err
		}
		return s.d.Store.SetSkills(ctx, id, np.Skills)
	})
	if err != nil {
		return domain.Project{}, err
	}
	if err := s.d.CreateLimiter.Record(ctx, userID, CreateWindow); err != nil {
		s.d.Logger.WarnContext(ctx, "project create limit not recorded", slog.Any("error", err))
	}
	return s.d.Store.Get(ctx, id, userID)
}

func validCreate(userID string, in CreateInput) (domain.NewProject, error) {
	title, err := validTitle(in.Title)
	if err != nil {
		return domain.NewProject{}, err
	}
	desc, err := validDescription(in.Description)
	if err != nil {
		return domain.NewProject{}, err
	}
	skills, err := validSkills(in.Skills)
	if err != nil {
		return domain.NewProject{}, err
	}
	abstract, err := validAbstract(in.Abstract)
	if err != nil {
		return domain.NewProject{}, err
	}
	return domain.NewProject{OwnerID: userID, Title: title, Description: desc, Abstract: abstract, Skills: skills}, nil
}

// Get returns the project as viewerID sees it, or domain.ErrNotFound when it does not exist
// or the viewer may not see it (Access.Visible). The abstract is for members only.
func (s *Service) Get(ctx context.Context, viewerID, projectID string) (domain.Project, error) {
	p, err := s.d.Store.Get(ctx, projectID, viewerID)
	if err != nil {
		return domain.Project{}, err
	}
	if !p.Access.Visible() || !s.d.Permissions.Can(p.MyRole, domain.ActView) {
		return domain.Project{}, domain.ErrNotFound
	}
	return forViewer(p), nil
}

// forViewer hides what a non-member may not read.
func forViewer(p domain.Project) domain.Project {
	if p.MyRole == domain.RoleNone {
		p.Abstract = nil
	}
	return p
}

// UpdateInput is an edit as the caller sends it. A nil field is missing and keeps its value;
// SetAbstract with a nil Abstract clears it.
type UpdateInput struct {
	Title       *string
	Description *string
	Skills      *[]string
	SetAbstract bool
	Abstract    *string
	Status      *string
}

// Update changes the fields in, after checking, in this order and inside one transaction
// that locks the project: that the caller may see it (domain.ErrNotFound), that their role
// allows every field named (domain.ErrForbidden: the details and status need
// ActEditDetails, the abstract ActEditAbstract), then the field rules (*domain.FieldError)
// (FR-10.2).
func (s *Service) Update(ctx context.Context, viewerID, projectID string, in UpdateInput) (domain.Project, error) {
	err := s.d.Tx.InTx(ctx, func(ctx context.Context) error {
		a, err := s.d.Store.Lock(ctx, projectID, viewerID)
		if err != nil {
			return err
		}
		if !a.Visible() {
			return domain.ErrNotFound
		}
		details := in.Title != nil || in.Description != nil || in.Skills != nil || in.Status != nil
		if details && !s.d.Permissions.Can(a.Role, domain.ActEditDetails) ||
			in.SetAbstract && !s.d.Permissions.Can(a.Role, domain.ActEditAbstract) {
			return domain.ErrForbidden
		}
		c, err := validChanges(in)
		if err != nil {
			return err
		}
		if !details && !in.SetAbstract {
			return nil
		}
		if err := s.d.Store.Update(ctx, projectID, c); err != nil {
			return err
		}
		if c.Status != nil && *c.Status != a.Status {
			s.d.Watcher.ProjectChanged(ctx, projectID)
		}
		if c.Skills != nil {
			return s.d.Store.SetSkills(ctx, projectID, *c.Skills)
		}
		return nil
	})
	if err != nil {
		return domain.Project{}, err
	}
	p, err := s.d.Store.Get(ctx, projectID, viewerID)
	if err != nil {
		return domain.Project{}, err
	}
	return forViewer(p), nil
}

func validChanges(in UpdateInput) (domain.Changes, error) {
	var c domain.Changes
	if in.Title != nil {
		t, err := validTitle(*in.Title)
		if err != nil {
			return c, err
		}
		c.Title = &t
	}
	if in.Description != nil {
		d, err := validDescription(*in.Description)
		if err != nil {
			return c, err
		}
		c.Description = &d
	}
	if in.Skills != nil {
		sk, err := validSkills(*in.Skills)
		if err != nil {
			return c, err
		}
		c.Skills = &sk
	}
	if in.SetAbstract {
		a, err := validAbstract(in.Abstract)
		if err != nil {
			return c, err
		}
		c.SetAbstract, c.Abstract = true, a
	}
	if in.Status != nil {
		st := domain.Status(*in.Status)
		if !st.Valid() {
			return c, &domain.FieldError{Field: "status"}
		}
		c.Status = &st
	}
	return c, nil
}

// Delete deletes the project and everything in it (FR-10.6) after checking, in one
// transaction that locks it: that the caller may see it (domain.ErrNotFound), may delete it
// (domain.ErrForbidden), and that confirm is the title, letter case included, after trimming
// both (domain.ErrConfirmMismatch).
func (s *Service) Delete(ctx context.Context, viewerID, projectID string, confirm *string) error {
	return s.d.Tx.InTx(ctx, func(ctx context.Context) error {
		a, err := s.d.Store.Lock(ctx, projectID, viewerID)
		if err != nil {
			return err
		}
		if !a.Visible() {
			return domain.ErrNotFound
		}
		if !s.d.Permissions.Can(a.Role, domain.ActDelete) {
			return domain.ErrForbidden
		}
		if confirm == nil || strings.TrimSpace(*confirm) != strings.TrimSpace(a.Title) {
			return domain.ErrConfirmMismatch
		}
		if err := s.d.Store.Delete(ctx, projectID); err != nil {
			return err
		}
		s.d.Watcher.ProjectChanged(ctx, projectID)
		return nil
	})
}

// Page is one page of a list.
type Page struct {
	Items    []domain.Summary
	Page     int
	PageSize int
	Total    int
}

// ListMine lists the projects userID is a member of, any status (FR-11.6). page is from 1;
// pageSize from 1, a larger one than MaxPageSize is reduced; else domain.ErrInvalidPage.
func (s *Service) ListMine(ctx context.Context, userID string, page, pageSize int) (Page, error) {
	if page < 1 || pageSize < 1 {
		return Page{}, domain.ErrInvalidPage
	}
	pageSize = min(pageSize, MaxPageSize)
	offset, ok := offsetOf(page, pageSize)
	if !ok {
		return Page{}, domain.ErrInvalidPage
	}
	items, total, err := s.d.Store.ListMine(ctx, userID, pageSize, offset)
	if err != nil {
		return Page{}, err
	}
	return Page{Items: items, Page: page, PageSize: pageSize, Total: total}, nil
}

// offsetOf is (page-1)*size, false when it would not fit an int32.
func offsetOf(page, size int) (int, bool) {
	off := (page - 1) * size
	return off, page-1 <= (1<<31-1)/size
}

// ReportInput is a report as the caller sends it.
type ReportInput struct {
	Reason  string
	Details *string
}

// Report stores the caller's report of a project (FR-15.11). Checks, in this order and in
// one transaction: the caller may see the project (domain.ErrNotFound), their account is not
// suspended (*domain.SuspendedError), the reason and details (*domain.FieldError), the caller
// is not the owner (domain.ErrCannotReportOwn) and has no open report of it
// (domain.ErrAlreadyReported).
func (s *Service) Report(ctx context.Context, userID, projectID string, in ReportInput) (domain.ReportReceipt, error) {
	var out domain.ReportReceipt
	err := s.d.Tx.InTx(ctx, func(ctx context.Context) error {
		p, err := s.d.Store.Get(ctx, projectID, userID)
		if err != nil {
			return err
		}
		if !p.Access.Visible() {
			return domain.ErrNotFound
		}
		st, err := s.d.Actors.Standing(ctx, userID)
		if err != nil {
			return err
		}
		if st.Suspended {
			return &domain.SuspendedError{Reason: st.SuspensionReason, Until: st.SuspendedUntil}
		}
		if !slices.Contains(domain.ReportReasons, in.Reason) {
			return &domain.FieldError{Field: "reason"}
		}
		details, err := optionalText(in.Details, MaxReportDetails, "details")
		if err != nil {
			return err
		}
		if p.Access.OwnerID == userID {
			return domain.ErrCannotReportOwn
		}
		out, err = s.d.Store.CreateReport(ctx, domain.NewReport{ProjectID: projectID, ReporterID: userID, Reason: in.Reason, Details: details})
		return err
	})
	return out, err
}

// ReportPage is one page of reports.
type ReportPage struct {
	Items    []domain.Report
	Page     int
	PageSize int
	Total    int
}

// ListReports lists open or resolved reports, newest first (FR-15.11). status is "open"
// (also when nil) or "resolved", else a *domain.FieldError; page is from 1.
func (s *Service) ListReports(ctx context.Context, status *string, page int) (ReportPage, error) {
	open := true
	if status != nil {
		switch *status {
		case "open":
		case "resolved":
			open = false
		default:
			return ReportPage{}, &domain.FieldError{Field: "status"}
		}
	}
	if page < 1 {
		return ReportPage{}, domain.ErrInvalidPage
	}
	offset, ok := offsetOf(page, ReportPageSize)
	if !ok {
		return ReportPage{}, domain.ErrInvalidPage
	}
	items, total, err := s.d.Store.ListReports(ctx, open, ReportPageSize, offset)
	if err != nil {
		return ReportPage{}, err
	}
	return ReportPage{Items: items, Page: page, PageSize: ReportPageSize, Total: total}, nil
}

// GetReport returns a report and the reported project, whatever its status.
func (s *Service) GetReport(ctx context.Context, reportID string) (domain.Report, domain.ReportedProject, error) {
	return s.d.Store.GetReport(ctx, reportID)
}

// ResolveReport closes a report with an outcome and an optional note. It returns a
// *domain.FieldError for a bad outcome or note, domain.ErrReportNotFound and
// domain.ErrAlreadyResolved.
func (s *Service) ResolveReport(ctx context.Context, adminID, reportID, outcome string, note *string) (domain.Report, error) {
	if !slices.Contains(domain.ReportOutcomes, outcome) {
		return domain.Report{}, &domain.FieldError{Field: "outcome"}
	}
	n, err := optionalText(note, MaxResolutionNote, "note")
	if err != nil {
		return domain.Report{}, err
	}
	return s.d.Store.ResolveReport(ctx, reportID, adminID, outcome, n)
}
