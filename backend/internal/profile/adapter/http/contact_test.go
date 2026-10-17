package http

import (
	"errors"
	"net/http"
	"reflect"
	"testing"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/httpx"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/profile/domain"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/profile/usecase"
)

const linkID = "00000000-0000-0000-0000-0000000000a1"

var adaContact = domain.Contact{
	UserID: callerID, Email: "ada@example.org", EmailVisibility: domain.LoggedIn,
	Phone: ptr("+8801712345678"), PhoneVisibility: domain.OnlyMe,
	Birthday: &domain.Birthday{Month: 2, Day: 29}, BirthdayVisibility: domain.OnlyMe,
	Links: []domain.Link{{ID: linkID, UserID: callerID, Kind: domain.LinkGitHub, URL: "https://github.com/ada"}},
}

var adaContactJSON = map[string]any{
	"email": "ada@example.org", "emailVisibility": "logged_in",
	"phone": "+8801712345678", "phoneVisibility": "only_me",
	"birthday": map[string]any{"month": float64(2), "day": float64(29)}, "birthdayVisibility": "only_me",
	"links": []any{map[string]any{"id": linkID, "kind": "github", "url": "https://github.com/ada"}},
}

func TestGetMyContact(t *testing.T) {
	t.Run("everything, with visibilities", func(t *testing.T) {
		svc := &fakeService{contact: adaContact}
		rec := send(t, svc, http.MethodGet, "/api/v1/profiles/me/contact", "", callerID)
		if rec.Code != http.StatusOK || svc.userID != callerID {
			t.Fatalf("status = %d for %q, want 200 for the caller; body %s", rec.Code, svc.userID, rec.Body)
		}
		if got := decode(t, rec); !reflect.DeepEqual(got, adaContactJSON) {
			t.Errorf("body = %v\nwant   %v", got, adaContactJSON)
		}
	})

	t.Run("nothing set: phone and birthday are null, links empty", func(t *testing.T) {
		svc := &fakeService{contact: domain.Contact{
			UserID: callerID, Email: "ada@example.org",
			EmailVisibility: domain.OnlyMe, PhoneVisibility: domain.OnlyMe, BirthdayVisibility: domain.OnlyMe,
		}}
		got := decode(t, send(t, svc, http.MethodGet, "/api/v1/profiles/me/contact", "", callerID))
		want := map[string]any{
			"email": "ada@example.org", "emailVisibility": "only_me", "phone": nil, "phoneVisibility": "only_me",
			"birthday": nil, "birthdayVisibility": "only_me", "links": []any{},
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("body = %v\nwant   %v", got, want)
		}
	})

	t.Run("account no longer exists", func(t *testing.T) {
		rec := send(t, &fakeService{err: domain.ErrNotFound}, http.MethodGet, "/api/v1/profiles/me/contact", "", callerID)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", rec.Code)
		}
	})
}

func TestUpdateMyContact(t *testing.T) {
	t.Run("passes the fields on", func(t *testing.T) {
		svc := &fakeService{contact: adaContact}
		body := `{"emailVisibility":"logged_in","phone":"+880 1712-345678","phoneVisibility":"only_me",
			"birthday":{"month":2,"day":29},"birthdayVisibility":"only_me"}`
		rec := send(t, svc, http.MethodPut, "/api/v1/profiles/me/contact", body, callerID)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body %s", rec.Code, rec.Body)
		}
		want := usecase.ContactInput{
			EmailVisibility: domain.LoggedIn, Phone: ptr("+880 1712-345678"), PhoneVisibility: domain.OnlyMe,
			Birthday: &domain.Birthday{Month: 2, Day: 29}, BirthdayVisibility: domain.OnlyMe,
		}
		if !reflect.DeepEqual(svc.contactIn, want) || svc.userID != callerID {
			t.Errorf("service got %+v for %q, want %+v for the caller", svc.contactIn, svc.userID, want)
		}
		if got := decode(t, rec); !reflect.DeepEqual(got, adaContactJSON) {
			t.Errorf("body = %v\nwant   %v", got, adaContactJSON)
		}
	})

	t.Run("missing phone and birthday are passed as nil", func(t *testing.T) {
		svc := &fakeService{contact: adaContact}
		send(t, svc, http.MethodPut, "/api/v1/profiles/me/contact",
			`{"emailVisibility":"only_me","phoneVisibility":"only_me","birthdayVisibility":"only_me","phone":null}`, callerID)
		if svc.contactIn.Phone != nil || svc.contactIn.Birthday != nil {
			t.Errorf("service got %+v, want no phone and no birthday", svc.contactIn)
		}
	})

	for _, tt := range []struct {
		err  error
		code string
	}{
		{domain.ErrInvalidPhone, CodeInvalidPhone},
		{domain.ErrInvalidBirthday, CodeInvalidBirthday},
		{domain.ErrInvalidVisibility, CodeInvalidVisibility},
	} {
		t.Run(tt.code, func(t *testing.T) {
			rec := send(t, &fakeService{err: tt.err}, http.MethodPut, "/api/v1/profiles/me/contact",
				`{"emailVisibility":"x","phoneVisibility":"only_me","birthdayVisibility":"only_me"}`, callerID)
			got := decode(t, rec)
			if rec.Code != http.StatusBadRequest || got["code"] != tt.code || got["message"] == "" {
				t.Errorf("got %d %v, want 400 %s", rec.Code, got, tt.code)
			}
		})
	}

	t.Run("body that is not JSON", func(t *testing.T) {
		rec := send(t, &fakeService{}, http.MethodPut, "/api/v1/profiles/me/contact", "{", callerID)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", rec.Code)
		}
	})
}

func TestAddMyLink(t *testing.T) {
	t.Run("created", func(t *testing.T) {
		svc := &fakeService{link: domain.Link{ID: linkID, UserID: callerID, Kind: domain.LinkWebsite, URL: "https://ada.example.org"}}
		rec := send(t, svc, http.MethodPost, "/api/v1/profiles/me/links", `{"kind":"website","url":" https://ada.example.org "}`, callerID)
		if rec.Code != http.StatusCreated {
			t.Fatalf("status = %d, want 201; body %s", rec.Code, rec.Body)
		}
		if svc.linkKind != domain.LinkWebsite || svc.linkURL != " https://ada.example.org " || svc.userID != callerID {
			t.Errorf("service got %q %q for %q", svc.linkKind, svc.linkURL, svc.userID)
		}
		want := map[string]any{"id": linkID, "kind": "website", "url": "https://ada.example.org"}
		if got := decode(t, rec); !reflect.DeepEqual(got, want) {
			t.Errorf("body = %v, want %v", got, want)
		}
	})

	for _, tt := range []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"http URL", domain.ErrInvalidLinkURL, http.StatusBadRequest, CodeInvalidLinkURL},
		{"unknown kind", domain.ErrInvalidLinkKind, http.StatusBadRequest, CodeInvalidLinkKind},
		{"6th link", domain.ErrLinkLimitReached, http.StatusConflict, CodeLinkLimitReached},
		{"account gone", domain.ErrNotFound, http.StatusUnauthorized, httpx.CodeUnauthenticated},
	} {
		t.Run(tt.name, func(t *testing.T) {
			rec := send(t, &fakeService{err: tt.err}, http.MethodPost, "/api/v1/profiles/me/links",
				`{"kind":"website","url":"http://ada.example.org"}`, callerID)
			got := decode(t, rec)
			if rec.Code != tt.status || got["code"] != tt.code {
				t.Errorf("got %d %v, want %d %s", rec.Code, got, tt.status, tt.code)
			}
		})
	}

	t.Run("store failure is a 500", func(t *testing.T) {
		rec := send(t, &fakeService{err: errors.New("database down")}, http.MethodPost, "/api/v1/profiles/me/links",
			`{"kind":"website","url":"https://ada.example.org"}`, callerID)
		if rec.Code != http.StatusInternalServerError {
			t.Errorf("status = %d, want 500", rec.Code)
		}
	})
}

func TestRemoveMyLink(t *testing.T) {
	t.Run("removed", func(t *testing.T) {
		svc := &fakeService{}
		rec := send(t, svc, http.MethodDelete, "/api/v1/profiles/me/links/"+linkID, "", callerID)
		if rec.Code != http.StatusNoContent || svc.linkID != linkID || svc.userID != callerID {
			t.Errorf("got %d, service removed %q for %q", rec.Code, svc.linkID, svc.userID)
		}
	})

	// The usecase gives ErrNotFound for someone else's link and an unknown one alike.
	t.Run("not found", func(t *testing.T) {
		rec := send(t, &fakeService{err: domain.ErrNotFound}, http.MethodDelete, "/api/v1/profiles/me/links/not-a-uuid", "", callerID)
		got := decode(t, rec)
		if rec.Code != http.StatusNotFound || got["code"] != httpx.CodeNotFound {
			t.Errorf("got %d %v, want 404 not_found", rec.Code, got)
		}
	})
}

// A profile's contact object leaves hidden items out of the JSON: the keys are absent,
// not null (FR-4.7).
func TestProfileContactLeavesHiddenItemsOut(t *testing.T) {
	link := domain.Link{ID: linkID, UserID: otherID, Kind: domain.LinkOther, URL: "https://example.org"}
	tests := []struct {
		name    string
		contact domain.VisibleContact
		want    map[string]any
	}{
		{"only links", domain.VisibleContact{Links: []domain.Link{link}},
			map[string]any{"links": []any{map[string]any{"id": linkID, "kind": "other", "url": "https://example.org"}}}},
		{"phone only", domain.VisibleContact{Phone: ptr("+441234567890"), Links: []domain.Link{}},
			map[string]any{"phone": "+441234567890", "links": []any{}}},
		{"everything", domain.VisibleContact{
			Email: ptr("bob@example.org"), Phone: ptr("+441234567890"),
			Birthday: &domain.Birthday{Month: 12, Day: 10}, Links: []domain.Link{},
		}, map[string]any{
			"email": "bob@example.org", "phone": "+441234567890",
			"birthday": map[string]any{"month": float64(12), "day": float64(10)}, "links": []any{},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &fakeService{profile: domain.Profile{UserID: otherID, FullName: "Bob", Contact: tt.contact}}
			got := decode(t, send(t, svc, http.MethodGet, "/api/v1/profiles/"+otherID, "", callerID))
			if !reflect.DeepEqual(got["contact"], tt.want) {
				t.Errorf("contact = %v\nwant      %v", got["contact"], tt.want)
			}
		})
	}
}
