// Package http serves the profile operations in docs/openapi.yaml: the caller's own
// profile and editing its basic fields (FR-4.1), research interests and skills (FR-4.2)
// with suggestions (FR-4.3), education entries (FR-4.4), contact info and links (FR-4.7),
// work experience entries (FR-4.8), languages (FR-4.9), any user's profile (FR-4.5) and
// publications added by hand, from a DOI or from ORCID (FR-5.1 to FR-5.7), and researcher
// search (FR-6.1 to FR-6.3).
// It turns the usecase's errors into the error codes the web client shows next to each
// field. cmd/api guards each route with its platform action (SRS 2.3) before it gets here.
package http

import (
	"context"
	"errors"
	"fmt"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/api/gen"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/httpx"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/profile/domain"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/profile/usecase"
)

// Error codes of the profile operations (docs/openapi.yaml).
const (
	CodeInvalidFullName    = "invalid_full_name"
	CodeInvalidHeadline    = "invalid_headline"
	CodeInvalidBio         = "invalid_bio"
	CodeInvalidInstitution = "invalid_institution"
	CodeInvalidDepartment  = "invalid_department"
	CodeInvalidJobTitle    = "invalid_job_title"
	CodeInvalidCountry     = "invalid_country"
	CodeInvalidCity        = "invalid_city"
	CodeInvalidTagName     = "invalid_tag_name"
	CodeTagLimitReached    = "tag_limit_reached"

	CodeInvalidDegree         = "invalid_degree"
	CodeInvalidFieldOfStudy   = "invalid_field_of_study"
	CodeInvalidStartYear      = "invalid_start_year"
	CodeInvalidEndYear        = "invalid_end_year"
	CodeInvalidGrade          = "invalid_grade"
	CodeEducationLimitReached = "education_limit_reached"

	CodeInvalidTitle           = "invalid_title"
	CodeInvalidOrganization    = "invalid_organization"
	CodeInvalidEmploymentType  = "invalid_employment_type"
	CodeInvalidLocation        = "invalid_location"
	CodeInvalidLocationType    = "invalid_location_type"
	CodeInvalidStartDate       = "invalid_start_date"
	CodeInvalidEndDate         = "invalid_end_date"
	CodeInvalidIsCurrent       = "invalid_is_current"
	CodeEndDateRequired        = "end_date_required"
	CodeEndDateNotAllowed      = "end_date_not_allowed"
	CodeInvalidDateRange       = "invalid_date_range"
	CodeInvalidDescription     = "invalid_description"
	CodeExperienceLimitReached = "experience_limit_reached"

	CodeInvalidLanguageName  = "invalid_language_name"
	CodeInvalidProficiency   = "invalid_proficiency"
	CodeLanguageExists       = "language_exists"
	CodeLanguageLimitReached = "language_limit_reached"

	CodeInvalidPhone      = "invalid_phone"
	CodeInvalidBirthday   = "invalid_birthday"
	CodeInvalidVisibility = "invalid_visibility"
	CodeInvalidLinkKind   = "invalid_link_kind"
	CodeInvalidLinkURL    = "invalid_link_url"
	CodeLinkLimitReached  = "link_limit_reached"

	CodeInvalidYear               = "invalid_year"
	CodeInvalidVenue              = "invalid_venue"
	CodeInvalidAbstract           = "invalid_abstract"
	CodeInvalidURL                = "invalid_url"
	CodeInvalidDOI                = "invalid_doi"
	CodePublicationLimitReached   = "publication_limit_reached"
	CodePublicationAlreadyAdded   = "publication_already_added"
	CodePublicationSharedReadonly = "publication_shared_readonly"
	CodeDuplicateDOI              = "duplicate_doi"
	CodeDOINotFound               = "doi_not_found"
	CodeMetadataUnavailable       = "metadata_unavailable"
	CodeRateLimited               = "rate_limited"

	CodeInvalidORCIDID = "invalid_orcid_id"
	CodeORCIDTaken     = "orcid_taken"
	CodeORCIDNotSet    = "orcid_not_set"
	CodeORCIDNotFound  = "orcid_not_found"
	CodeInvalidPutCode = "invalid_put_code"
)

// service is the part of usecase.Service this handler needs.
type service interface {
	GetOwn(ctx context.Context, userID string) (domain.Profile, error)
	UpdateOwn(ctx context.Context, userID string, in usecase.UpdateInput) (domain.Profile, error)
	View(ctx context.Context, viewerID, userID string) (domain.Profile, error)
	AddTag(ctx context.Context, userID string, kind domain.TagKind, name string) (domain.Tag, bool, error)
	RemoveTag(ctx context.Context, userID string, kind domain.TagKind, tagID string) error
	SuggestTags(ctx context.Context, kind domain.TagKind, q string, limit int) ([]domain.Tag, error)
	AddEducation(ctx context.Context, userID string, in usecase.EducationInput) (domain.Education, error)
	UpdateEducation(ctx context.Context, userID, id string, patch usecase.EducationPatch) (domain.Education, error)
	RemoveEducation(ctx context.Context, userID, id string) error
	AddExperience(ctx context.Context, userID string, in usecase.ExperienceInput) (domain.Experience, error)
	UpdateExperience(ctx context.Context, userID, id string, patch usecase.ExperiencePatch) (domain.Experience, error)
	RemoveExperience(ctx context.Context, userID, id string) error
	AddLanguage(ctx context.Context, userID string, in usecase.LanguageInput) (domain.Language, error)
	UpdateLanguage(ctx context.Context, userID, id string, patch usecase.LanguagePatch) (domain.Language, error)
	RemoveLanguage(ctx context.Context, userID, id string) error
	GetContact(ctx context.Context, userID string) (domain.Contact, error)
	UpdateContact(ctx context.Context, userID string, in usecase.ContactInput) (domain.Contact, error)
	AddLink(ctx context.Context, userID string, kind domain.LinkKind, url string) (domain.Link, error)
	RemoveLink(ctx context.Context, userID, id string) error
	AddPublication(ctx context.Context, userID string, in usecase.PublicationInput) (usecase.AddedPublication, error)
	UpdatePublication(ctx context.Context, userID, id string, patch usecase.PublicationPatch) (domain.Publication, error)
	RemovePublication(ctx context.Context, userID, id string) error
	ListOwnPublications(ctx context.Context, userID string, page, pageSize int) (domain.PublicationPage, error)
	ListPublications(ctx context.Context, viewerID, userID string, page, pageSize int) (domain.PublicationPage, error)
	LookupDOI(ctx context.Context, userID, doi string) (usecase.DOIPreview, error)
	AddPublicationFromDOI(ctx context.Context, userID, doi string) (usecase.AddedPublication, error)
	SetOrcidID(ctx context.Context, userID string, orcidID *string) (*string, error)
	ListORCIDWorks(ctx context.Context, userID string) (usecase.ORCIDWorkList, error)
	ImportORCIDWorks(ctx context.Context, userID string, putCodes []string) (usecase.ImportResult, error)
	SearchPeople(ctx context.Context, userID string, in usecase.SearchInput) (domain.PeopleResults, error)
}

// Handler implements the profile operations of gen.StrictServerInterface.
type Handler struct {
	svc service
}

// NewHandler returns a Handler that calls svc.
func NewHandler(svc service) *Handler {
	return &Handler{svc: svc}
}

// inputErrors maps the usecase's validation errors to a code and a message for the user.
var inputErrors = []struct {
	err           error
	code, message string
}{
	{domain.ErrInvalidFullName, CodeInvalidFullName, "Enter your full name on one line (at most 200 characters)."},
	{domain.ErrInvalidHeadline, CodeInvalidHeadline, "The headline must be one line of at most 220 characters."},
	{domain.ErrInvalidBio, CodeInvalidBio, "The bio must be at most 2000 characters long."},
	{domain.ErrInvalidInstitution, CodeInvalidInstitution, "The institution must be one line of at most 200 characters."},
	{domain.ErrInvalidDepartment, CodeInvalidDepartment, "The department must be one line of at most 200 characters."},
	{domain.ErrInvalidJobTitle, CodeInvalidJobTitle, "The job title must be one line of at most 200 characters."},
	{domain.ErrInvalidCountry, CodeInvalidCountry, "Choose a country from the list."},
	{domain.ErrInvalidCity, CodeInvalidCity, fmt.Sprintf("The city must be one line of at most %d characters.", usecase.MaxCityLength)},
}

func inputError(err error) (gen.Error, bool) {
	for _, e := range inputErrors {
		if errors.Is(err, e.err) {
			return gen.Error{Code: e.code, Message: e.message}, true
		}
	}
	return gen.Error{}, false
}

var unauthenticated = gen.UnauthorizedJSONResponse{Code: httpx.CodeUnauthenticated, Message: "Log in to continue."}

// GetMyProfile returns the caller's profile (FR-4.1). A valid token for an account that
// no longer exists is refused like a missing one, as GET /auth/me does.
func (h *Handler) GetMyProfile(ctx context.Context, _ gen.GetMyProfileRequestObject) (gen.GetMyProfileResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.GetMyProfile401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	p, err := h.svc.GetOwn(ctx, id)
	if errors.Is(err, domain.ErrNotFound) {
		return gen.GetMyProfile401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	if err != nil {
		return nil, err
	}
	body, err := profileBody(p)
	if err != nil {
		return nil, err
	}
	return gen.GetMyProfile200JSONResponse(body), nil
}

// UpdateMyProfile creates or replaces the caller's profile fields (FR-4.1).
func (h *Handler) UpdateMyProfile(ctx context.Context, req gen.UpdateMyProfileRequestObject) (gen.UpdateMyProfileResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.UpdateMyProfile401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	if req.Body == nil {
		return gen.UpdateMyProfile400JSONResponse{Code: httpx.CodeBadRequest, Message: "The request is not valid."}, nil
	}
	p, err := h.svc.UpdateOwn(ctx, id, usecase.UpdateInput{
		FullName:    req.Body.FullName,
		Headline:    req.Body.Headline,
		Bio:         req.Body.Bio,
		Institution: req.Body.Institution,
		Department:  req.Body.Department,
		JobTitle:    req.Body.JobTitle,
		Country:     req.Body.Country,
		City:        req.Body.City,
	})
	if e, ok := inputError(err); ok {
		return gen.UpdateMyProfile400JSONResponse(e), nil
	}
	if errors.Is(err, domain.ErrNotFound) {
		return gen.UpdateMyProfile401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	if err != nil {
		return nil, err
	}
	body, err := profileBody(p)
	if err != nil {
		return nil, err
	}
	return gen.UpdateMyProfile200JSONResponse(body), nil
}

// GetProfile returns any user's profile to a logged-in user (FR-4.5), with the contact
// info the caller may see (FR-4.7). Unknown, suspended and unverified accounts get the
// same 404, so the answer reveals nothing about them. An ID that is not a UUID names no
// user and gets the same 404.
func (h *Handler) GetProfile(ctx context.Context, req gen.GetProfileRequestObject) (gen.GetProfileResponseObject, error) {
	viewer, ok := httpx.UserID(ctx)
	if !ok {
		return gen.GetProfile401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	p, err := h.svc.View(ctx, viewer, req.UserId)
	if errors.Is(err, domain.ErrNotFound) {
		return gen.GetProfile404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse{
			Code: httpx.CodeNotFound, Message: "This profile does not exist.",
		}}, nil
	}
	if err != nil {
		return nil, err
	}
	body, err := profileBody(p)
	if err != nil {
		return nil, err
	}
	return gen.GetProfile200JSONResponse(body), nil
}

// profileBody builds the response. Every list is empty, never null, when there is
// nothing in it.
func profileBody(p domain.Profile) (gen.Profile, error) {
	var id openapi_types.UUID
	if err := id.UnmarshalText([]byte(p.UserID)); err != nil {
		return gen.Profile{}, fmt.Errorf("profile user id: %w", err)
	}
	interests, err := tagBodies(p.Interests)
	if err != nil {
		return gen.Profile{}, err
	}
	skills, err := tagBodies(p.Skills)
	if err != nil {
		return gen.Profile{}, err
	}
	education, err := educationBodies(p.Education)
	if err != nil {
		return gen.Profile{}, err
	}
	experience, err := experienceBodies(p.Experience)
	if err != nil {
		return gen.Profile{}, err
	}
	languages, err := languageBodies(p.Languages)
	if err != nil {
		return gen.Profile{}, err
	}
	contact, err := publicContactBody(p.Contact)
	if err != nil {
		return gen.Profile{}, err
	}
	status, requestID, err := connectionFields(p.Connection)
	if err != nil {
		return gen.Profile{}, err
	}
	conversation, err := conversationField(p.Conversation)
	if err != nil {
		return gen.Profile{}, err
	}
	return gen.Profile{
		UserId:              id,
		FullName:            p.FullName,
		Headline:            p.Headline,
		Bio:                 p.Bio,
		Institution:         p.Institution,
		Department:          p.Department,
		JobTitle:            p.JobTitle,
		Country:             p.Country,
		City:                p.City,
		OrcidId:             p.OrcidID,
		Interests:           interests,
		Skills:              skills,
		Education:           education,
		Experience:          experience,
		Languages:           languages,
		PublicationCount:    p.PublicationCount,
		ConnectionStatus:    status,
		ConnectionRequestId: requestID,
		ConnectionCount:     p.ConnectionCount,
		BlockedByMe:         &p.BlockedByMe,
		Conversation:        conversation,
		Contact:             contact,
		PhotoUrl:            mediaURL(p.PhotoFileID),
		CoverUrl:            mediaURL(p.CoverFileID),
	}, nil
}

// mediaURL is where the client reads a profile image (FR-4.6), nil when none is set.
func mediaURL(fileID *string) *string {
	if fileID == nil {
		return nil
	}
	u := httpx.MediaURL(*fileID)
	return &u
}

// conversationField is the contract's conversation for link, nil when there is none.
func conversationField(link *domain.ConversationLink) (*gen.ProfileConversation, error) {
	if link == nil {
		return nil, nil
	}
	var id openapi_types.UUID
	if err := id.UnmarshalText([]byte(link.ID)); err != nil {
		return nil, fmt.Errorf("conversation id: %w", err)
	}
	out := &gen.ProfileConversation{Id: id, State: gen.ConversationState(link.State), CanMessage: link.CanMessage}
	if link.RequestedBy != nil {
		by := new(openapi_types.UUID)
		if err := by.UnmarshalText([]byte(*link.RequestedBy)); err != nil {
			return nil, fmt.Errorf("conversation requested by: %w", err)
		}
		out.RequestedBy = by
	}
	return out, nil
}

// connectionFields is the contract's connectionStatus and connectionRequestId for rel; a
// status the contract does not know reads as none.
func connectionFields(rel domain.ConnectionRelation) (gen.ConnectionStatus, *openapi_types.UUID, error) {
	status := gen.ConnectionStatus(rel.Status)
	if !status.Valid() {
		status = gen.ConnectionStatusNone
	}
	if rel.RequestID == nil {
		return status, nil, nil
	}
	id := new(openapi_types.UUID)
	if err := id.UnmarshalText([]byte(*rel.RequestID)); err != nil {
		return "", nil, fmt.Errorf("connection request id: %w", err)
	}
	return status, id, nil
}
