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

const tagID = "00000000-0000-0000-0000-0000000000aa"

var tagKinds = []struct {
	kind           domain.TagKind
	profilePath    string // POST, and DELETE with /{tagId}
	suggestionPath string
	limitMessage   string
}{
	{domain.KindInterest, "/api/v1/profiles/me/interests", "/api/v1/tags/interests", "You can add at most 20 research interests."},
	{domain.KindSkill, "/api/v1/profiles/me/skills", "/api/v1/tags/skills", "You can add at most 30 skills."},
}

func TestAddTagOperations(t *testing.T) {
	for _, k := range tagKinds {
		t.Run(string(k.kind), func(t *testing.T) {
			ml := domain.Tag{ID: tagID, Name: "Machine Learning"}

			for name, created := range map[string]bool{"newly linked: 201": true, "already had it: 200": false} {
				t.Run(name, func(t *testing.T) {
					svc := &fakeService{tag: ml, created: created}
					rec := send(t, svc, http.MethodPost, k.profilePath, `{"name":"machine learning"}`, callerID)
					want := http.StatusOK
					if created {
						want = http.StatusCreated
					}
					if rec.Code != want {
						t.Fatalf("status = %d, want %d; body %s", rec.Code, want, rec.Body)
					}
					if svc.userID != callerID || svc.kind != k.kind || svc.name != "machine learning" {
						t.Errorf("service got user %q, kind %q, name %q", svc.userID, svc.kind, svc.name)
					}
					// The same body in both cases: the tag with its stored spelling.
					if got := decode(t, rec); !reflect.DeepEqual(got, map[string]any{"id": tagID, "name": "Machine Learning"}) {
						t.Errorf("body = %v", got)
					}
				})
			}

			tests := []struct {
				name     string
				err      error
				status   int
				code     string
				message  string
				noCaller bool
			}{
				{"invalid name", domain.ErrInvalidTagName, http.StatusBadRequest, CodeInvalidTagName, "", false},
				{"limit reached", domain.ErrTagLimitReached, http.StatusConflict, CodeTagLimitReached, k.limitMessage, false},
				{"account no longer exists", domain.ErrNotFound, http.StatusUnauthorized, httpx.CodeUnauthenticated, "", false},
				{"no user in the context", nil, http.StatusUnauthorized, httpx.CodeUnauthenticated, "", true},
			}
			for _, tt := range tests {
				t.Run(tt.name, func(t *testing.T) {
					caller := callerID
					if tt.noCaller {
						caller = ""
					}
					rec := send(t, &fakeService{err: tt.err}, http.MethodPost, k.profilePath, `{"name":"x"}`, caller)
					got := decode(t, rec)
					if rec.Code != tt.status || got["code"] != tt.code || got["message"] == "" {
						t.Errorf("got %d %v, want %d %s with a message", rec.Code, got, tt.status, tt.code)
					}
					if tt.message != "" && got["message"] != tt.message {
						t.Errorf("message = %q, want %q", got["message"], tt.message)
					}
				})
			}

			t.Run("unexpected error", func(t *testing.T) {
				rec := send(t, &fakeService{err: errors.New("boom")}, http.MethodPost, k.profilePath, `{"name":"x"}`, callerID)
				if rec.Code != http.StatusInternalServerError {
					t.Errorf("status = %d, want 500", rec.Code)
				}
			})

			t.Run("body is not JSON", func(t *testing.T) {
				svc := &fakeService{}
				rec := send(t, svc, http.MethodPost, k.profilePath, `{`, callerID)
				if rec.Code != http.StatusBadRequest || svc.userID != "" {
					t.Errorf("got %d, service called for %q; want 400 and no call", rec.Code, svc.userID)
				}
			})
		})
	}
}

func TestRemoveTagOperations(t *testing.T) {
	for _, k := range tagKinds {
		t.Run(string(k.kind), func(t *testing.T) {
			t.Run("removed", func(t *testing.T) {
				svc := &fakeService{}
				rec := send(t, svc, http.MethodDelete, k.profilePath+"/"+tagID, "", callerID)
				if rec.Code != http.StatusNoContent || rec.Body.Len() != 0 {
					t.Errorf("got %d %q, want 204 with no body", rec.Code, rec.Body)
				}
				if svc.userID != callerID || svc.kind != k.kind || svc.tagID != tagID {
					t.Errorf("service got user %q, kind %q, tag %q", svc.userID, svc.kind, svc.tagID)
				}
			})

			// The usecase gives ErrNotFound for a tag the user lacks, an unknown tag and a
			// malformed ID alike; the ID is passed on as sent.
			for _, id := range []string{tagID, "not-a-uuid"} {
				t.Run("not on the profile: "+id, func(t *testing.T) {
					svc := &fakeService{err: domain.ErrNotFound}
					rec := send(t, svc, http.MethodDelete, k.profilePath+"/"+id, "", callerID)
					got := decode(t, rec)
					if rec.Code != http.StatusNotFound || got["code"] != httpx.CodeNotFound || svc.tagID != id {
						t.Errorf("got %d %v, service asked for %q; want 404 not_found for %q", rec.Code, got, svc.tagID, id)
					}
				})
			}

			t.Run("no user in the context", func(t *testing.T) {
				rec := send(t, &fakeService{}, http.MethodDelete, k.profilePath+"/"+tagID, "", "")
				if rec.Code != http.StatusUnauthorized {
					t.Errorf("status = %d, want 401", rec.Code)
				}
			})
		})
	}
}

func TestSuggestTagOperations(t *testing.T) {
	for _, k := range tagKinds {
		t.Run(string(k.kind), func(t *testing.T) {
			t.Run("q and limit are passed on", func(t *testing.T) {
				svc := &fakeService{suggestions: []domain.Tag{{ID: tagID, Name: "Machine Learning"}}}
				rec := send(t, svc, http.MethodGet, k.suggestionPath+"?q=mach&limit=5", "", callerID)
				if rec.Code != http.StatusOK {
					t.Fatalf("status = %d, want 200; body %s", rec.Code, rec.Body)
				}
				if svc.kind != k.kind || svc.q != "mach" || svc.limit != 5 {
					t.Errorf("service got kind %q, q %q, limit %d", svc.kind, svc.q, svc.limit)
				}
				want := map[string]any{"items": []any{map[string]any{"id": tagID, "name": "Machine Learning"}}}
				if got := decode(t, rec); !reflect.DeepEqual(got, want) {
					t.Errorf("body = %v, want %v", got, want)
				}
			})

			t.Run("defaults: empty q, limit 10", func(t *testing.T) {
				svc := &fakeService{}
				rec := send(t, svc, http.MethodGet, k.suggestionPath, "", callerID)
				if rec.Code != http.StatusOK || svc.q != "" || svc.limit != usecase.DefaultSuggestions {
					t.Errorf("got %d, service got q %q, limit %d; want 200, empty q, limit 10", rec.Code, svc.q, svc.limit)
				}
				if got := decode(t, rec); !reflect.DeepEqual(got, map[string]any{"items": []any{}}) {
					t.Errorf("body = %v, want an empty items list", got)
				}
			})

			t.Run("limit below 1", func(t *testing.T) {
				rec := send(t, &fakeService{err: domain.ErrInvalidLimit}, http.MethodGet, k.suggestionPath+"?q=a&limit=0", "", callerID)
				if rec.Code != http.StatusBadRequest || decode(t, rec)["code"] != httpx.CodeBadRequest {
					t.Errorf("got %d %s, want 400 bad_request", rec.Code, rec.Body)
				}
			})

			t.Run("limit not a number", func(t *testing.T) {
				svc := &fakeService{}
				rec := send(t, svc, http.MethodGet, k.suggestionPath+"?limit=ten", "", callerID)
				if rec.Code != http.StatusBadRequest || svc.kind != "" {
					t.Errorf("got %d, service called for kind %q; want 400 and no call", rec.Code, svc.kind)
				}
			})
		})
	}
}

// GET /profiles/me returns the user's tags in the order the usecase gives them.
func TestProfileBodyHasTags(t *testing.T) {
	p := ada
	p.Interests = []domain.Tag{{ID: tagID, Name: "AI"}}
	p.Skills = []domain.Tag{{ID: otherID, Name: "Go"}}
	rec := send(t, &fakeService{profile: p}, http.MethodGet, "/api/v1/profiles/me", "", callerID)
	got := decode(t, rec)
	if want := []any{map[string]any{"id": tagID, "name": "AI"}}; !reflect.DeepEqual(got["interests"], want) {
		t.Errorf("interests = %v, want %v", got["interests"], want)
	}
	if want := []any{map[string]any{"id": otherID, "name": "Go"}}; !reflect.DeepEqual(got["skills"], want) {
		t.Errorf("skills = %v, want %v", got["skills"], want)
	}
}
