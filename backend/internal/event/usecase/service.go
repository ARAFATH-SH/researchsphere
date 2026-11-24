// Package usecase holds the venue rules (FR-14, FR-15.1): the list and its filters, a
// venue's page, the administrators' changes with their audit trail, and the file import
// (FR-14.5).
package usecase

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/event/domain"
)

// Paging of the list.
const (
	DefaultPageSize = 20
	MaxPageSize     = 50
	MinQueryChars   = 2
	MaxQueryChars   = 100
	MinReasonChars  = 10
	MaxReasonChars  = 1000
)

// Service runs the venue rules.
type Service struct {
	store domain.Store
	tx    domain.Transactor
	now   func() time.Time
}

// New returns a Service. now is the clock; nil means time.Now.
func New(store domain.Store, tx domain.Transactor, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{store: store, tx: tx, now: now}
}

// ListInput is a list request as sent. A zero Page or PageSize means the default.
type ListInput struct {
	Page, PageSize int
	IncludePast    bool
	Rolling        bool
	Types, Fields  []string
	DeadlineFrom   string
	DeadlineTo     string
	TZ             string
	Q              string
}

// Fields returns the fixed list of research fields.
func (s *Service) Fields(ctx context.Context) ([]domain.Field, error) {
	return s.store.Fields(ctx)
}

// List returns one page of venues (FR-14.1 to FR-14.3) with the page number and size it
// used. It returns an InvalidError when a parameter breaks a rule.
func (s *Service) List(ctx context.Context, in ListInput) (p domain.Page, page, size int, err error) {
	q, page, size, err := s.query(ctx, in)
	if err != nil {
		return domain.Page{}, 0, 0, err
	}
	if p, err = s.store.List(ctx, q); err != nil {
		return domain.Page{}, 0, 0, err
	}
	return p, page, size, nil
}

func (s *Service) query(ctx context.Context, in ListInput) (domain.Query, int, int, error) {
	q := domain.Query{Now: s.now(), IncludePast: in.IncludePast, Rolling: in.Rolling}
	if in.Rolling {
		// A rolling journal has no deadline: these filters cannot apply.
		switch {
		case in.IncludePast:
			return q, 0, 0, domain.Invalid("includePast", domain.CodeNotAllowed)
		case strings.TrimSpace(in.DeadlineFrom) != "":
			return q, 0, 0, domain.Invalid("deadlineFrom", domain.CodeNotAllowed)
		case strings.TrimSpace(in.DeadlineTo) != "":
			return q, 0, 0, domain.Invalid("deadlineTo", domain.CodeNotAllowed)
		}
	}
	page, size := in.Page, in.PageSize
	switch {
	case page == 0:
		page = 1
	case page < 1:
		return q, 0, 0, domain.Invalid("page", domain.CodeInvalid)
	}
	switch {
	case size == 0:
		size = DefaultPageSize
	case size < 1:
		return q, 0, 0, domain.Invalid("pageSize", domain.CodeInvalid)
	case size > MaxPageSize:
		size = MaxPageSize
	}
	q.Limit, q.Offset = size, (page-1)*size

	for _, t := range in.Types {
		vt := domain.VenueType(strings.TrimSpace(t))
		if !slices.Contains(domain.VenueTypes, vt) {
			return q, 0, 0, domain.Invalid("type", domain.CodeInvalid)
		}
		if !slices.Contains(q.Types, vt) {
			q.Types = append(q.Types, vt)
		}
	}
	if len(in.Fields) > 0 {
		fields, err := s.store.Fields(ctx)
		if err != nil {
			return q, 0, 0, err
		}
		known := newCatalog(fields)
		for _, f := range in.Fields {
			f = strings.TrimSpace(f)
			if _, ok := known.index[f]; !ok {
				return q, 0, 0, domain.Invalid("field", domain.CodeUnknownField)
			}
			if !slices.Contains(q.Fields, f) {
				q.Fields = append(q.Fields, f)
			}
		}
	}

	loc, err := loadViewZone(strings.TrimSpace(in.TZ))
	if err != nil {
		return q, 0, 0, domain.Invalid("tz", domain.CodeInvalidTimeZone)
	}
	from, to, field, err := dayWindow(in.DeadlineFrom, in.DeadlineTo, loc)
	if err != nil {
		return q, 0, 0, domain.Invalid(field, domain.CodeInvalidDate)
	}
	q.From, q.To = from, to

	q.Q = strings.TrimSpace(in.Q)
	if n := utf8.RuneCountInString(q.Q); q.Q != "" && (n < MinQueryChars || n > MaxQueryChars || hasControl(q.Q, false)) {
		return q, 0, 0, domain.Invalid("q", domain.CodeInvalid)
	}
	return q, page, size, nil
}

// Get returns a live venue (FR-14.4).
func (s *Service) Get(ctx context.Context, id string) (domain.Venue, error) {
	return s.store.Get(ctx, id)
}

// Create adds a venue and its audit row in one transaction (FR-15.1).
func (s *Service) Create(ctx context.Context, adminID string, in Input) (domain.Venue, error) {
	known, err := s.catalog(ctx)
	if err != nil {
		return domain.Venue{}, err
	}
	v, problems := validate(in, known)
	if len(problems) > 0 {
		return domain.Venue{}, &domain.InvalidError{Problems: problems}
	}
	v.Source = domain.SourceManual
	var id string
	err = s.tx.InTx(ctx, func(ctx context.Context) error {
		name, err := s.store.AdminName(ctx, adminID)
		if err != nil {
			return err
		}
		if id, err = s.store.Create(ctx, v, adminID); err != nil {
			return err
		}
		return s.store.Record(ctx, domain.Action{VenueID: id, AdminID: adminID, AdminName: name, Kind: domain.ActCreate})
	})
	if err != nil {
		return domain.Venue{}, err
	}
	return s.store.Get(ctx, id)
}

// Opt is one field of a PATCH: absent, null, or a value.
type Opt struct {
	Set   bool
	Null  bool
	Value string
}

// OptBool is a true/false field of a PATCH: absent, or a value (null means false).
type OptBool struct {
	Set   bool
	Value bool
}

// Patch is the fields of a venue to change. Fields is the whole new list.
type Patch struct {
	Rolling                                                            OptBool
	Name, ShortName, Type, Attendance, City, Country, StartsOn, EndsOn Opt
	TimeZone, SubmissionDeadline, AbstractDeadline, NotificationAt     Opt
	OfficialURL, Scope                                                 Opt
	Fields                                                             []string
	FieldsSet, FieldsNull                                              bool
}

// Update changes the fields p names (FR-15.1). The venue as changed must follow every rule.
// A change writes an update audit row naming the changed fields; no change writes nothing.
// Changing the time zone alone keeps the deadlines' local times and reads them in the new
// zone. A change to a venue of the automatic import locks it: the import no longer changes
// it. A new link is the venue's own (LinkOfficial).
func (s *Service) Update(ctx context.Context, adminID, id string, p Patch) (domain.Venue, error) {
	known, err := s.catalog(ctx)
	if err != nil {
		return domain.Venue{}, err
	}
	err = s.tx.InTx(ctx, func(ctx context.Context) error {
		old, err := s.store.GetForUpdate(ctx, id)
		if err != nil {
			return err
		}
		in, err := applyPatch(inputOf(old), p)
		if err != nil {
			return err
		}
		v, problems := validate(in, known)
		if len(problems) > 0 {
			return &domain.InvalidError{Problems: problems}
		}
		changed := changedFields(old, v)
		if len(changed) == 0 {
			return nil
		}
		v.ID, v.Source, v.SourceKey = old.ID, old.Source, old.SourceKey
		v.LinkKind = old.Link()
		if v.OfficialURL != old.OfficialURL {
			v.LinkKind = domain.LinkOfficial
		}
		if err := s.store.Update(ctx, v); err != nil {
			return err
		}
		if old.Source.IsSync() {
			if err := s.store.Lock(ctx, id, s.now()); err != nil {
				return err
			}
		}
		name, err := s.store.AdminName(ctx, adminID)
		if err != nil {
			return err
		}
		return s.store.Record(ctx, domain.Action{VenueID: id, AdminID: adminID, AdminName: name, Kind: domain.ActUpdate, ChangedFields: changed})
	})
	if err != nil {
		return domain.Venue{}, err
	}
	return s.store.Get(ctx, id)
}

// Remove hides a venue for good, with a reason kept in the audit trail (FR-15.1). Its
// embedding goes at once (the database's trigger).
func (s *Service) Remove(ctx context.Context, adminID, id, reason string) error {
	reason = strings.TrimSpace(reason)
	switch n := utf8.RuneCountInString(reason); {
	case reason == "":
		return domain.Invalid("reason", domain.CodeRequired)
	case n < MinReasonChars:
		return domain.Invalid("reason", domain.CodeTooShort)
	case n > MaxReasonChars:
		return domain.Invalid("reason", domain.CodeTooLong)
	case hasControl(reason, true):
		return domain.Invalid("reason", domain.CodeInvalid)
	}
	return s.tx.InTx(ctx, func(ctx context.Context) error {
		if err := s.store.Remove(ctx, id, adminID, reason, s.now()); err != nil {
			return err
		}
		name, err := s.store.AdminName(ctx, adminID)
		if err != nil {
			return err
		}
		return s.store.Record(ctx, domain.Action{VenueID: id, AdminID: adminID, AdminName: name, Kind: domain.ActRemove, Reason: reason})
	})
}

func (s *Service) catalog(ctx context.Context) (catalog, error) {
	fields, err := s.store.Fields(ctx)
	if err != nil {
		return catalog{}, fmt.Errorf("read research fields: %w", err)
	}
	return newCatalog(fields), nil
}

// inputOf is a stored venue as an administrator would enter it: deadlines as local times in
// its zone.
func inputOf(v domain.Venue) Input {
	loc, err := LoadZone(v.TimeZone)
	if err != nil {
		loc = time.UTC
	}
	local := func(t time.Time) string {
		if t.IsZero() {
			return ""
		}
		return FormatLocal(t, loc)
	}
	date := func(t time.Time) string {
		if t.IsZero() {
			return ""
		}
		return FormatDate(t)
	}
	return Input{
		Name: v.Name, ShortName: v.ShortName, Type: string(v.Type), Rolling: v.Rolling, Fields: v.FieldKeys(),
		Attendance: string(v.Attendance), City: v.City, Country: v.Country,
		StartsOn: date(v.StartsOn), EndsOn: date(v.EndsOn), TimeZone: v.TimeZone,
		SubmissionDeadline: local(v.SubmissionDeadline), AbstractDeadline: local(v.AbstractDeadline),
		NotificationAt: local(v.NotificationAt), OfficialURL: v.OfficialURL, Scope: v.Scope,
	}
}

// applyPatch sets the fields p names. null clears an optional field and is refused for a
// required one.
func applyPatch(in Input, p Patch) (Input, error) {
	if p.Rolling.Set {
		in.Rolling = p.Rolling.Value
	}
	type slot struct {
		field    string
		opt      Opt
		dst      *string
		required bool
	}
	for _, s := range []slot{
		{"name", p.Name, &in.Name, true},
		{"shortName", p.ShortName, &in.ShortName, false},
		{"type", p.Type, &in.Type, true},
		{"attendance", p.Attendance, &in.Attendance, false},
		{"city", p.City, &in.City, false},
		{"country", p.Country, &in.Country, false},
		{"startsOn", p.StartsOn, &in.StartsOn, false},
		{"endsOn", p.EndsOn, &in.EndsOn, false},
		{"timeZone", p.TimeZone, &in.TimeZone, true},
		{"submissionDeadline", p.SubmissionDeadline, &in.SubmissionDeadline, true},
		{"abstractDeadline", p.AbstractDeadline, &in.AbstractDeadline, false},
		{"notificationAt", p.NotificationAt, &in.NotificationAt, false},
		{"officialUrl", p.OfficialURL, &in.OfficialURL, true},
		{"scope", p.Scope, &in.Scope, true},
	} {
		switch {
		case !s.opt.Set:
		case s.opt.Null && s.required:
			return Input{}, domain.Invalid(s.field, domain.CodeRequired)
		case s.opt.Null:
			*s.dst = ""
		default:
			*s.dst = s.opt.Value
		}
	}
	if p.FieldsSet {
		if p.FieldsNull {
			return Input{}, domain.Invalid("fields", domain.CodeRequired)
		}
		in.Fields = p.Fields
	}
	return in, nil
}

// changedFields names the fields whose values differ, in the API's names.
func changedFields(a, b domain.Venue) []string {
	var out []string
	add := func(name string, differ bool) {
		if differ {
			out = append(out, name)
		}
	}
	add("name", a.Name != b.Name)
	add("shortName", a.ShortName != b.ShortName)
	add("type", a.Type != b.Type)
	add("rolling", a.Rolling != b.Rolling)
	add("fields", !slices.Equal(a.FieldKeys(), b.FieldKeys()))
	add("attendance", a.Attendance != b.Attendance)
	add("city", a.City != b.City)
	add("country", a.Country != b.Country)
	add("startsOn", !a.StartsOn.Equal(b.StartsOn))
	add("endsOn", !a.EndsOn.Equal(b.EndsOn))
	add("timeZone", a.TimeZone != b.TimeZone)
	add("submissionDeadline", !a.SubmissionDeadline.Equal(b.SubmissionDeadline))
	add("abstractDeadline", !a.AbstractDeadline.Equal(b.AbstractDeadline))
	add("notificationAt", !a.NotificationAt.Equal(b.NotificationAt))
	add("officialUrl", a.OfficialURL != b.OfficialURL)
	add("linkKind", a.Link() != b.Link())
	add("scope", a.Scope != b.Scope)
	return out
}
