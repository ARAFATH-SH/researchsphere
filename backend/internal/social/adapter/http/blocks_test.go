package http

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/api/gen"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/social/domain"
)

func TestBlockUser(t *testing.T) {
	tests := []struct {
		name, body string
		err        error
		status     int
		want       string
	}{
		{"blocked", `{"userId":"` + otherID + `"}`, nil, http.StatusNoContent, ""},
		{"oneself", `{"userId":"` + otherID + `"}`, domain.ErrBlockSelf, http.StatusBadRequest, CodeCannotBlockSelf},
		{"unknown", `{"userId":"` + otherID + `"}`, domain.ErrNotFound, http.StatusNotFound, "not_found"},
		{"not a UUID", `{"userId":"x"}`, nil, http.StatusBadRequest, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := send(t, &fakeService{err: tc.err}, http.MethodPost, "/api/v1/blocks", tc.body, callerID)
			if rec.Code != tc.status || !strings.Contains(rec.Body.String(), tc.want) {
				t.Errorf("%d %s, want %d with %q", rec.Code, rec.Body, tc.status, tc.want)
			}
		})
	}
}

func TestUnblockUser(t *testing.T) {
	svc := &fakeService{}
	rec := send(t, svc, http.MethodDelete, "/api/v1/blocks/"+otherID, "", callerID)
	if rec.Code != http.StatusNoContent || svc.blocked != otherID {
		t.Errorf("%d, blocked %q", rec.Code, svc.blocked)
	}
}

func TestListBlockedPeople(t *testing.T) {
	svc := &fakeService{blockedPeople: []domain.BlockedPerson{{BlockedAt: at, Other: other}}}
	rec := send(t, svc, http.MethodGet, "/api/v1/blocks?page=2", "", callerID)
	if rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	var page gen.BlockedPersonPage
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	got := page.Items[0]
	if got.UserId.String() != otherID || got.FullName != "Bob" || got.Headline == nil || !got.BlockedAt.Equal(at) ||
		got.PhotoUrl == nil || *got.PhotoUrl != "/api/v1/media/"+photoID || page.Page != 2 || page.PageSize != 20 || page.Total != 1 {
		t.Errorf("page = %+v", page)
	}
	if rec := send(t, &fakeService{err: domain.ErrInvalidPage}, http.MethodGet, "/api/v1/blocks?page=0", "", callerID); rec.Code != http.StatusBadRequest {
		t.Errorf("bad page: %d", rec.Code)
	}
}

// The blocked person hears the answer for an unknown user; only the blocker is told.
func TestSendConnectionRequestBlocked(t *testing.T) {
	body := `{"userId":"` + otherID + `"}`
	rec := send(t, &fakeService{err: domain.ErrYouBlocked}, http.MethodPost, "/api/v1/connections/requests", body, callerID)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), `"you_blocked_this_user"`) {
		t.Errorf("%d %s", rec.Code, rec.Body)
	}
}
