package http

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/api/gen"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/moderation/domain"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/httpx"
)

const (
	adminID = "b5a1e5f6-1f52-4b0a-9c37-0d6f2b6a1c01"
	userID  = "c6b2f607-2a63-4c1b-8d48-1e7a3c7b2d02"
	item    = "d7c3a718-3b74-4d2c-9e59-2f8b4d8c3e03"
)

// fakeService answers every action with err.
type fakeService struct {
	err     error
	history domain.History
	warning *domain.Warning
}

var at = time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)

func (f fakeService) action() (domain.Action, error) {
	return domain.Action{ID: item, UserID: userID, Kind: domain.ActionWarning, Reason: "Repeated spam", CreatedAt: at}, f.err
}
func (f fakeService) Warn(context.Context, string, string, domain.Input) (domain.Action, error) {
	return f.action()
}
func (f fakeService) Restrict(context.Context, string, string, domain.RestrictInput) (domain.Action, error) {
	return f.action()
}
func (f fakeService) Lift(context.Context, string, string, domain.ReverseInput) error { return f.err }
func (f fakeService) Suspend(context.Context, string, string, domain.SuspendInput) (domain.Action, error) {
	return f.action()
}
func (f fakeService) Restore(context.Context, string, string, domain.ReverseInput) (domain.Action, error) {
	return f.action()
}
func (f fakeService) History(context.Context, string, int) (domain.History, error) {
	return f.history, f.err
}
func (f fakeService) PendingWarnings(context.Context, string) ([]domain.Warning, error) {
	return nil, f.err
}
func (f fakeService) Warning(context.Context, string, string) (domain.Warning, error) {
	if f.warning != nil {
		return *f.warning, f.err
	}
	return domain.Warning{ID: item, Reason: "Repeated spam", CreatedAt: at}, f.err
}
func (f fakeService) Acknowledge(context.Context, string, string) error { return f.err }

func TestActionErrors(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		status int
		code   string
		field  string
	}{
		{"field", &domain.InvalidFieldError{Field: "reason"}, 400, httpx.CodeBadRequest, "reason"},
		{"report about someone else", &domain.InvalidFieldError{Field: "reportId"}, 400, httpx.CodeBadRequest, "reportId"},
		{"administrator", domain.ErrCannotModerateAdmin, 403, CodeCannotModerateAdmin, ""},
		{"unknown", domain.ErrNotFound, 404, httpx.CodeNotFound, ""},
		{"unknown report", domain.ErrReportNotFound, 404, httpx.CodeNotFound, ""},
		{"restricted", domain.ErrAlreadyRestricted, 409, CodeAlreadyRestricted, ""},
		{"suspended", domain.ErrAlreadySuspended, 409, CodeAlreadySuspended, ""},
		{"not suspended", domain.ErrNotSuspended, 409, CodeNotSuspended, ""},
		{"not active", domain.ErrRestrictionNotActive, 409, CodeRestrictionNotActive, ""},
		{"resolved", domain.ErrReportResolved, 409, CodeReportResolved, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := refusalOf(tt.err)
			if r == nil {
				t.Fatal("no refusal")
			}
			var code string
			var field *string
			status := 0
			switch {
			case r.invalid != nil:
				status, code, field = 400, r.invalid.Code, r.invalid.Field
				if r.invalid.Message == "" {
					t.Error("no message")
				}
			case r.forbidden != nil:
				status, code = 403, r.forbidden.Code
			case r.notFound:
				status, code = 404, httpx.CodeNotFound
			case r.conflict != nil:
				status, code = 409, r.conflict.Code
			}
			if status != tt.status || code != tt.code {
				t.Errorf("got %d %s, want %d %s", status, code, tt.status, tt.code)
			}
			if tt.field != "" && (field == nil || *field != tt.field) {
				t.Errorf("field = %v, want %s", field, tt.field)
			}
		})
	}
	if refusalOf(errors.New("database down")) != nil {
		t.Error("an unknown error must be a 500, not a refusal")
	}
}

// Each operation turns a refusal into its own response type: one row for every operation.
func TestOperationsAnswerRefusals(t *testing.T) {
	ctx := httpx.WithUserID(context.Background(), adminID)
	h := NewHandler(fakeService{err: domain.ErrCannotModerateAdmin})
	reason := "Repeated spam in messages"

	warn, err := h.WarnUser(ctx, gen.WarnUserRequestObject{UserId: userID, Body: &gen.WarnUserRequest{Reason: reason}})
	if _, ok := warn.(gen.WarnUser403JSONResponse); !ok || err != nil {
		t.Errorf("WarnUser = %T, %v", warn, err)
	}
	restrict, err := h.RestrictUser(ctx, gen.RestrictUserRequestObject{UserId: userID, Body: &gen.RestrictUserRequest{Reason: reason}})
	if _, ok := restrict.(gen.RestrictUser403JSONResponse); !ok || err != nil {
		t.Errorf("RestrictUser = %T, %v", restrict, err)
	}
	lift, err := h.LiftRestriction(ctx, gen.LiftRestrictionRequestObject{RestrictionId: item, Body: &gen.ReverseActionRequest{Reason: reason}})
	if _, ok := lift.(gen.LiftRestriction403JSONResponse); !ok || err != nil {
		t.Errorf("LiftRestriction = %T, %v", lift, err)
	}
	suspend, err := h.SuspendUser(ctx, gen.SuspendUserRequestObject{UserId: userID, Body: &gen.SuspendUserRequest{Reason: reason}})
	if _, ok := suspend.(gen.SuspendUser403JSONResponse); !ok || err != nil {
		t.Errorf("SuspendUser = %T, %v", suspend, err)
	}
	restore, err := h.RestoreUser(ctx, gen.RestoreUserRequestObject{UserId: userID, Body: &gen.ReverseActionRequest{Reason: reason}})
	if _, ok := restore.(gen.RestoreUser403JSONResponse); !ok || err != nil {
		t.Errorf("RestoreUser = %T, %v", restore, err)
	}
}

func TestMissingSessionAndBody(t *testing.T) {
	h := NewHandler(fakeService{})
	if r, _ := h.WarnUser(context.Background(), gen.WarnUserRequestObject{UserId: userID}); r == nil {
		t.Fatal("nil")
	} else if _, ok := r.(gen.WarnUser401JSONResponse); !ok {
		t.Errorf("no session: %T", r)
	}
	ctx := httpx.WithUserID(context.Background(), adminID)
	if r, _ := h.WarnUser(ctx, gen.WarnUserRequestObject{UserId: userID}); r == nil {
		t.Fatal("nil")
	} else if _, ok := r.(gen.WarnUser400JSONResponse); !ok {
		t.Errorf("no body: %T", r)
	}
}

// What a user reads of their warning has no note and names no administrator.
func TestWarningBodyHasNoNoteOrAdmin(t *testing.T) {
	ctx := httpx.WithUserID(context.Background(), userID)
	r, err := NewHandler(fakeService{}).GetMyWarning(ctx, gen.GetMyWarningRequestObject{WarningId: item})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"note", "adminId", "adminName"} {
		if _, ok := got[k]; ok {
			t.Errorf("the body has %q: %s", k, raw)
		}
	}
	if len(got) != 5 {
		t.Errorf("fields = %v, want id, reason, createdAt, acknowledged, acknowledgedAt", got)
	}
	if v, ok := got["acknowledgedAt"]; !ok || v != nil {
		t.Errorf("acknowledgedAt = %v, want null until acknowledged", v)
	}
}

func TestWarningBodySaysWhenAcknowledged(t *testing.T) {
	ctx := httpx.WithUserID(context.Background(), userID)
	when := at.Add(time.Hour)
	svc := fakeService{warning: &domain.Warning{ID: item, Reason: "Repeated spam", CreatedAt: at, AcknowledgedAt: &when}}
	r, err := NewHandler(svc).GetMyWarning(ctx, gen.GetMyWarningRequestObject{WarningId: item})
	if err != nil {
		t.Fatal(err)
	}
	got, ok := r.(gen.GetMyWarning200JSONResponse)
	if !ok || !got.Acknowledged || got.AcknowledgedAt == nil || !got.AcknowledgedAt.Equal(when) {
		t.Errorf("response = %+v", r)
	}
}

func TestHistoryBodyHasTheUser(t *testing.T) {
	why, ends := "Repeated spam", at.Add(time.Hour)
	account := domain.Account{
		ID: userID, Email: "ada@example.org", FullName: "Ada Lovelace", Verified: true, CreatedAt: at,
		SuspendedAt: &at, SuspendedUntil: &ends, SuspensionReason: &why,
	}
	admin := account
	admin.Admin = true
	tests := []struct {
		name       string
		history    domain.History
		wantRole   gen.Role
		wantReason bool
	}{
		{"suspended now", domain.History{User: account, Suspended: true}, gen.Researcher, true},
		{"the suspension has ended by time", domain.History{User: account}, gen.Researcher, false},
		{"administrator", domain.History{User: admin}, gen.Admin, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := httpx.WithUserID(context.Background(), adminID)
			r, err := NewHandler(fakeService{history: tt.history}).GetUserModeration(ctx, gen.GetUserModerationRequestObject{UserId: userID})
			if err != nil {
				t.Fatal(err)
			}
			got, ok := r.(gen.GetUserModeration200JSONResponse)
			if !ok {
				t.Fatalf("response = %#v", r)
			}
			u := got.User
			if u.Id.String() != userID || u.FullName != "Ada Lovelace" || u.Email != "ada@example.org" || !u.Verified ||
				u.Role != tt.wantRole || u.Suspended != tt.history.Suspended || !u.CreatedAt.Equal(at) {
				t.Errorf("user = %+v", u)
			}
			if (u.SuspensionReason != nil) != tt.wantReason || (u.SuspendedUntil != nil) != tt.wantReason {
				t.Errorf("reason = %v, until = %v, want them shown: %v", u.SuspensionReason, u.SuspendedUntil, tt.wantReason)
			}
		})
	}
}
