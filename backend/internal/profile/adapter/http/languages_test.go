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

const (
	languageID        = "00000000-0000-0000-0000-0000000000a1"
	languagePath      = "/api/v1/profiles/me/languages"
	languageEntryPath = languagePath + "/" + languageID
	languageJSON      = `{"name":"Bangla","proficiency":"native_or_bilingual"}`
)

var bangla = domain.Language{ID: languageID, UserID: callerID, Name: "Bangla", Proficiency: domain.ProficiencyNativeOrBilingual}

var banglaBody = map[string]any{"id": languageID, "name": "Bangla", "proficiency": "native_or_bilingual"}

// The validation errors in the contract's order, and the codes they become.
var languageInputErrors = []struct {
	err  error
	code string
}{
	{domain.ErrInvalidLanguageName, CodeInvalidLanguageName},
	{domain.ErrInvalidProficiency, CodeInvalidProficiency},
}

func TestAddMyLanguage(t *testing.T) {
	t.Run("created: 201 with the stored language", func(t *testing.T) {
		svc := &fakeService{language: bangla}
		rec := send(t, svc, http.MethodPost, languagePath, `{"name":" Bangla ","proficiency":"native_or_bilingual"}`, callerID)
		if rec.Code != http.StatusCreated {
			t.Fatalf("status = %d, want 201; body %s", rec.Code, rec.Body)
		}
		want := usecase.LanguageInput{Name: " Bangla ", Proficiency: domain.ProficiencyNativeOrBilingual}
		if svc.userID != callerID || !reflect.DeepEqual(svc.languageIn, want) {
			t.Errorf("service got user %q, %+v; want %q, %+v", svc.userID, svc.languageIn, callerID, want)
		}
		if got := decode(t, rec); !reflect.DeepEqual(got, banglaBody) {
			t.Errorf("body = %v, want %v", got, banglaBody)
		}
	})

	tests := []struct {
		name     string
		err      error
		status   int
		code     string
		noCaller bool
	}{
		{"exists", domain.ErrLanguageExists, http.StatusConflict, CodeLanguageExists, false},
		{"limit reached", domain.ErrLanguageLimitReached, http.StatusConflict, CodeLanguageLimitReached, false},
		{"account no longer exists", domain.ErrNotFound, http.StatusUnauthorized, httpx.CodeUnauthenticated, false},
		{"no user in the context", nil, http.StatusUnauthorized, httpx.CodeUnauthenticated, true},
	}
	for _, e := range languageInputErrors {
		tests = append(tests, struct {
			name     string
			err      error
			status   int
			code     string
			noCaller bool
		}{e.code, e.err, http.StatusBadRequest, e.code, false})
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			caller := callerID
			if tt.noCaller {
				caller = ""
			}
			rec := send(t, &fakeService{err: tt.err}, http.MethodPost, languagePath, languageJSON, caller)
			got := decode(t, rec)
			if rec.Code != tt.status || got["code"] != tt.code || got["message"] == "" {
				t.Errorf("got %d %v, want %d %s with a message", rec.Code, got, tt.status, tt.code)
			}
		})
	}

	t.Run("limit message names the limit", func(t *testing.T) {
		rec := send(t, &fakeService{err: domain.ErrLanguageLimitReached}, http.MethodPost, languagePath, languageJSON, callerID)
		if got := decode(t, rec)["message"]; got != "You can add at most 20 languages." {
			t.Errorf("message = %q", got)
		}
	})

	t.Run("unexpected error", func(t *testing.T) {
		rec := send(t, &fakeService{err: errors.New("boom")}, http.MethodPost, languagePath, languageJSON, callerID)
		if rec.Code != http.StatusInternalServerError {
			t.Errorf("status = %d, want 500", rec.Code)
		}
	})

	t.Run("body is not JSON", func(t *testing.T) {
		svc := &fakeService{}
		rec := send(t, svc, http.MethodPost, languagePath, `{`, callerID)
		if rec.Code != http.StatusBadRequest || svc.userID != "" {
			t.Errorf("got %d, service called for %q; want 400 and no call", rec.Code, svc.userID)
		}
	})
}

func TestUpdateMyLanguage(t *testing.T) {
	t.Run("set fields reach the usecase", func(t *testing.T) {
		svc := &fakeService{language: bangla}
		rec := send(t, svc, http.MethodPatch, languageEntryPath, `{"name":"Bengali","proficiency":"full_professional"}`, callerID)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body %s", rec.Code, rec.Body)
		}
		want := usecase.LanguagePatch{
			Name:        usecase.Field[string]{Set: true, Value: "Bengali"},
			Proficiency: usecase.Field[domain.Proficiency]{Set: true, Value: domain.ProficiencyFullProfessional},
		}
		if svc.userID != callerID || svc.languageID != languageID || !reflect.DeepEqual(svc.languagePatch, want) {
			t.Errorf("service got user %q, id %q, %+v; want %+v", svc.userID, svc.languageID, svc.languagePatch, want)
		}
		if got := decode(t, rec); !reflect.DeepEqual(got, banglaBody) {
			t.Errorf("body = %v, want %v", got, banglaBody)
		}
	})

	t.Run("null fields are passed on as null", func(t *testing.T) {
		svc := &fakeService{language: bangla}
		send(t, svc, http.MethodPatch, languageEntryPath, `{"name":null,"proficiency":null}`, callerID)
		want := usecase.LanguagePatch{
			Name:        usecase.Field[string]{Set: true, Null: true},
			Proficiency: usecase.Field[domain.Proficiency]{Set: true, Null: true},
		}
		if !reflect.DeepEqual(svc.languagePatch, want) {
			t.Errorf("patch = %+v, want %+v", svc.languagePatch, want)
		}
	})

	t.Run("empty body changes nothing", func(t *testing.T) {
		svc := &fakeService{language: bangla}
		rec := send(t, svc, http.MethodPatch, languageEntryPath, `{}`, callerID)
		if rec.Code != http.StatusOK || !reflect.DeepEqual(svc.languagePatch, usecase.LanguagePatch{}) {
			t.Errorf("got %d, patch %+v; want 200 and an empty patch", rec.Code, svc.languagePatch)
		}
	})

	tests := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		// Someone else's language and one that does not exist look the same.
		{"not found", domain.ErrNotFound, http.StatusNotFound, httpx.CodeNotFound},
		{"exists", domain.ErrLanguageExists, http.StatusConflict, CodeLanguageExists},
	}
	for _, e := range languageInputErrors {
		tests = append(tests, struct {
			name   string
			err    error
			status int
			code   string
		}{e.code, e.err, http.StatusBadRequest, e.code})
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := send(t, &fakeService{err: tt.err}, http.MethodPatch, languageEntryPath, `{"name":"Bengali"}`, callerID)
			if got := decode(t, rec); rec.Code != tt.status || got["code"] != tt.code || got["message"] == "" {
				t.Errorf("got %d %v, want %d %s with a message", rec.Code, got, tt.status, tt.code)
			}
		})
	}

	t.Run("no user in the context", func(t *testing.T) {
		rec := send(t, &fakeService{}, http.MethodPatch, languageEntryPath, `{}`, "")
		if got := decode(t, rec); rec.Code != http.StatusUnauthorized || got["code"] != httpx.CodeUnauthenticated {
			t.Errorf("got %d %v, want 401", rec.Code, got)
		}
	})

	t.Run("unexpected error", func(t *testing.T) {
		rec := send(t, &fakeService{err: errors.New("boom")}, http.MethodPatch, languageEntryPath, `{}`, callerID)
		if rec.Code != http.StatusInternalServerError {
			t.Errorf("status = %d, want 500", rec.Code)
		}
	})

	t.Run("body is not JSON", func(t *testing.T) {
		svc := &fakeService{}
		rec := send(t, svc, http.MethodPatch, languageEntryPath, `{`, callerID)
		if rec.Code != http.StatusBadRequest || svc.userID != "" {
			t.Errorf("got %d, service called for %q; want 400 and no call", rec.Code, svc.userID)
		}
	})
}

func TestRemoveMyLanguage(t *testing.T) {
	t.Run("removed: 204", func(t *testing.T) {
		svc := &fakeService{}
		rec := send(t, svc, http.MethodDelete, languageEntryPath, "", callerID)
		if rec.Code != http.StatusNoContent || svc.userID != callerID || svc.languageID != languageID {
			t.Errorf("got %d, service got user %q, id %q", rec.Code, svc.userID, svc.languageID)
		}
	})

	t.Run("not found: 404", func(t *testing.T) {
		rec := send(t, &fakeService{err: domain.ErrNotFound}, http.MethodDelete, languageEntryPath, "", callerID)
		if got := decode(t, rec); rec.Code != http.StatusNotFound || got["code"] != httpx.CodeNotFound {
			t.Errorf("got %d %v, want 404 not_found", rec.Code, got)
		}
	})

	t.Run("no user in the context", func(t *testing.T) {
		rec := send(t, &fakeService{}, http.MethodDelete, languageEntryPath, "", "")
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", rec.Code)
		}
	})

	t.Run("unexpected error", func(t *testing.T) {
		rec := send(t, &fakeService{err: errors.New("boom")}, http.MethodDelete, languageEntryPath, "", callerID)
		if rec.Code != http.StatusInternalServerError {
			t.Errorf("status = %d, want 500", rec.Code)
		}
	})
}

// GET /profiles/me returns the languages in the order the usecase gives them (FR-4.9).
func TestProfileBodyHasLanguages(t *testing.T) {
	p := ada
	english := domain.Language{ID: otherID, UserID: callerID, Name: "English", Proficiency: domain.ProficiencyFullProfessional}
	p.Languages = []domain.Language{bangla, english}
	rec := send(t, &fakeService{profile: p}, http.MethodGet, "/api/v1/profiles/me", "", callerID)
	want := []any{banglaBody, map[string]any{"id": otherID, "name": "English", "proficiency": "full_professional"}}
	if got := decode(t, rec)["languages"]; !reflect.DeepEqual(got, want) {
		t.Errorf("languages = %v, want %v", got, want)
	}
}
