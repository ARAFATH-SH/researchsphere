package http

import (
	"context"
	"errors"
	"fmt"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/api/gen"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/httpx"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/recommendation/domain"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/recommendation/usecase"
)

// RecommendProjectVenues returns the venues most similar to a project's title and abstract,
// for its members (AI-6.1 to AI-6.4). Anyone else gets 404.
func (h *Handler) RecommendProjectVenues(ctx context.Context, req gen.RecommendProjectVenuesRequestObject) (gen.RecommendProjectVenuesResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.RecommendProjectVenues401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	res, err := h.svc.ProjectVenues(ctx, id, req.ProjectId)
	switch {
	case errors.Is(err, domain.ErrProjectNotFound):
		return gen.RecommendProjectVenues404JSONResponse{NotFoundJSONResponse: notFound}, nil
	case err != nil:
		return nil, err
	}
	body, err := venueBody(res)
	if err != nil {
		return nil, err
	}
	return gen.RecommendProjectVenues200JSONResponse(body), nil
}

func venueBody(res usecase.VenueResult) (gen.VenueRecommendations, error) {
	venues, err := venueItems(res.Venues)
	if err != nil {
		return gen.VenueRecommendations{}, err
	}
	journals, err := venueItems(res.Journals)
	if err != nil {
		return gen.VenueRecommendations{}, err
	}
	return gen.VenueRecommendations{
		State: gen.VenueRecommendationsState(res.State), Venues: venues, Journals: journals, Notice: res.Notice,
	}, nil
}

func venueItems(in []domain.VenueRecommendation) ([]gen.RecommendedVenue, error) {
	out := make([]gen.RecommendedVenue, len(in))
	for i, v := range in {
		vid, err := parseID(v.ID)
		if err != nil {
			return nil, fmt.Errorf("recommended venue id: %w", err)
		}
		fields := make([]gen.ResearchField, len(v.Fields))
		for j, f := range v.Fields {
			fields[j] = gen.ResearchField{Key: f.Key, Label: f.Label}
		}
		item := gen.RecommendedVenue{
			Id: vid, Name: v.Name, ShortName: v.ShortName, Type: gen.VenueType(v.Type), Fields: fields,
			OfficialUrl: v.OfficialURL, LinkKind: gen.RecommendedVenueLinkKind(v.LinkKind), Score: v.Score,
		}
		if v.Deadline != nil && v.TimeZone != nil {
			loc, err := deadlineZone(*v.TimeZone)
			if err != nil {
				return nil, fmt.Errorf("recommended venue %s: %w", v.ID, err)
			}
			item.TimeZone = v.TimeZone
			item.SubmissionDeadline = &gen.VenueTime{At: v.Deadline.UTC(), Local: v.Deadline.In(loc).Format(localLayout)}
			item.DaysLeft = v.DaysLeft
		}
		out[i] = item
	}
	return out, nil
}

func parseID(s string) (openapi_types.UUID, error) {
	var id openapi_types.UUID
	err := id.UnmarshalText([]byte(s))
	return id, err
}

// localLayout is VenueTime.local: YYYY-MM-DDTHH:MM:SS in the venue's zone.
const localLayout = "2006-01-02T15:04:05"

// aoe is "anywhere on Earth": UTC-12 with no daylight saving.
var aoe = time.FixedZone("AoE", -12*60*60)

// deadlineZone is the location of a venue's zone label: AoE, UTC or an IANA name. It is the
// rule of internal/event/usecase.LoadZone, which this module may not import; the database
// keeps only labels that rule accepts (venues_deadline_tz), and TestDeadlineZone checks both
// agree on the cases that matter.
func deadlineZone(label string) (*time.Location, error) {
	switch label {
	case "AoE":
		return aoe, nil
	case "UTC":
		return time.UTC, nil
	}
	return time.LoadLocation(label)
}
