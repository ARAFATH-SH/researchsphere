// Package usecase holds the profile module's logic: reading the caller's own profile and
// editing its basic fields (FR-4.1), research interests and skills as tags (FR-4.2) with
// suggestions (FR-4.3), education entries (FR-4.4), contact info and links with who may
// see them (FR-4.7), work experience entries (FR-4.8), languages (FR-4.9), and showing
// any user's profile to a logged-in user (FR-4.5), and publications added by hand, from
// a DOI or from ORCID, listed and removed (FR-5.1 to FR-5.7). It imports only the
// profile domain.
package usecase

import (
	"context"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/profile/domain"
)

// Service reads and edits profiles.
type Service struct {
	store  domain.Store
	now    func() time.Time // the clock for the education years and experience dates
	lookup DOILookup
	orcid  ORCIDImport
	search PeopleSearch
	conns  domain.ConnectionLookup // nil until WithConnections
}

// Option configures a Service.
type Option func(*Service)

// New returns a Service. Without WithDOILookup, DOI lookups fail with
// domain.ErrMetadataUnavailable; without WithORCIDImport, so do ORCID lists and imports.
// Without WithConnections, every profile reads as not connected, with no connections.
func New(store domain.Store, opts ...Option) *Service {
	s := &Service{store: store, now: time.Now}
	for _, o := range opts {
		o(s)
	}
	return s
}

// WithConnections makes profiles show the viewer's connection status and the owner's
// number of connections (FR-7).
func WithConnections(l domain.ConnectionLookup) Option {
	return func(s *Service) { s.conns = l }
}

// GetOwn returns the caller's profile. An account without a profile row gets an empty
// profile instead of an error. It returns domain.ErrNotFound only when the account no
// longer exists. Suspension is not checked: the caller's own token is still valid.
func (s *Service) GetOwn(ctx context.Context, userID string) (domain.Profile, error) {
	a, err := s.store.FindAccount(ctx, userID)
	if err != nil {
		return domain.Profile{}, err
	}
	return s.withDetails(ctx, profileOf(a), userID)
}

// View returns any user's profile to the logged-in user viewerID (FR-4.5), with the
// contact info viewerID may see (FR-4.7). An unknown, suspended or unverified account
// gives domain.ErrNotFound, so the caller cannot tell them apart, and so does an owner who
// has blocked viewerID (FR-16.9). A viewer who has blocked the owner still sees the profile.
func (s *Service) View(ctx context.Context, viewerID, userID string) (domain.Profile, error) {
	a, err := s.store.FindAccount(ctx, userID)
	if err != nil {
		return domain.Profile{}, err
	}
	if a.Suspended || !a.Verified {
		return domain.Profile{}, domain.ErrNotFound
	}
	blocks, err := s.blockState(ctx, viewerID, userID)
	if err != nil {
		return domain.Profile{}, err
	}
	if blocks.ByOwner {
		return domain.Profile{}, domain.ErrNotFound
	}
	p, err := s.withDetails(ctx, profileOf(a), viewerID)
	if err != nil {
		return domain.Profile{}, err
	}
	p.BlockedByMe = blocks.ByViewer
	return p, nil
}

// blockState reads the block between viewerID and ownerID; none without a ConnectionLookup.
func (s *Service) blockState(ctx context.Context, viewerID, ownerID string) (domain.BlockState, error) {
	if s.conns == nil {
		return domain.BlockState{}, nil
	}
	return s.conns.Block(ctx, viewerID, ownerID)
}

// UpdateInput is what a user sends to edit their profile. A nil optional field means
// "not set".
type UpdateInput struct {
	FullName    string
	Headline    *string
	Bio         *string
	Institution *string
	Department  *string
	JobTitle    *string
	Country     *string
	City        *string
}

// UpdateOwn validates in and creates or replaces the caller's profile fields (FR-4.1).
// It returns the first invalid field's error, in the contract's order, and stores
// nothing then. It returns domain.ErrNotFound when the account no longer exists.
func (s *Service) UpdateOwn(ctx context.Context, userID string, in UpdateInput) (domain.Profile, error) {
	p, err := normalize(in)
	if err != nil {
		return domain.Profile{}, err
	}
	p.UserID = userID
	saved, err := s.store.UpsertProfile(ctx, p)
	if err != nil {
		return domain.Profile{}, err
	}
	return s.withDetails(ctx, saved, userID)
}

// profileOf returns the account's profile, or an empty one when it has no profile row.
func profileOf(a domain.Account) domain.Profile {
	if a.Profile == nil {
		return domain.Profile{UserID: a.UserID}
	}
	return *a.Profile
}

// withDetails fills the profile's interests, skills, education and experience entries,
// languages, the contact info viewerID may see, the number of confirmed publications, and
// viewerID's connection status with the owner and the owner's number of connections.
func (s *Service) withDetails(ctx context.Context, p domain.Profile, viewerID string) (domain.Profile, error) {
	var err error
	if p.Interests, err = s.store.ListTags(ctx, p.UserID, domain.KindInterest); err != nil {
		return domain.Profile{}, err
	}
	if p.Skills, err = s.store.ListTags(ctx, p.UserID, domain.KindSkill); err != nil {
		return domain.Profile{}, err
	}
	if p.Education, err = s.store.ListEducation(ctx, p.UserID); err != nil {
		return domain.Profile{}, err
	}
	if p.Experience, err = s.store.ListExperience(ctx, p.UserID); err != nil {
		return domain.Profile{}, err
	}
	if p.Languages, err = s.store.ListLanguages(ctx, p.UserID); err != nil {
		return domain.Profile{}, err
	}
	sortLanguages(p.Languages)
	c, err := s.GetContact(ctx, p.UserID)
	if err != nil {
		return domain.Profile{}, err
	}
	p.Contact = VisibleTo(viewerID, c)
	if p.PublicationCount, err = s.store.CountConfirmedPublications(ctx, p.UserID); err != nil {
		return domain.Profile{}, err
	}
	p.Connection = noConnection(viewerID, p.UserID)
	if s.conns == nil {
		return p, nil
	}
	if p.Connection, err = s.conns.Relation(ctx, viewerID, p.UserID); err != nil {
		return domain.Profile{}, err
	}
	if p.ConnectionCount, err = s.conns.Count(ctx, viewerID, p.UserID); err != nil {
		return domain.Profile{}, err
	}
	return p, nil
}

// noConnection is the relation shown without a ConnectionLookup: ConnectionSelf for the
// viewer's own, ConnectionNone for anyone else.
func noConnection(viewerID, userID string) domain.ConnectionRelation {
	if viewerID == userID {
		return domain.ConnectionRelation{Status: domain.ConnectionSelf}
	}
	return domain.ConnectionRelation{Status: domain.ConnectionNone}
}
