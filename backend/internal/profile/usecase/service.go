// Package usecase holds the profile module's logic: reading the caller's own profile and
// editing its basic fields (FR-4.1), research interests and skills as tags (FR-4.2) with
// suggestions (FR-4.3), and showing any user's profile to a logged-in user (FR-4.5). It
// imports only the profile domain.
package usecase

import (
	"context"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/profile/domain"
)

// Service reads and edits profiles.
type Service struct {
	store domain.Store
}

// New returns a Service.
func New(store domain.Store) *Service {
	return &Service{store: store}
}

// GetOwn returns the caller's profile. An account without a profile row gets an empty
// profile instead of an error. It returns domain.ErrNotFound only when the account no
// longer exists. Suspension is not checked: the caller's own token is still valid.
func (s *Service) GetOwn(ctx context.Context, userID string) (domain.Profile, error) {
	a, err := s.store.FindAccount(ctx, userID)
	if err != nil {
		return domain.Profile{}, err
	}
	return s.withTags(ctx, profileOf(a))
}

// View returns any user's profile to a logged-in user (FR-4.5). An unknown, suspended or
// unverified account gives domain.ErrNotFound, so the caller cannot tell them apart.
func (s *Service) View(ctx context.Context, userID string) (domain.Profile, error) {
	a, err := s.store.FindAccount(ctx, userID)
	if err != nil {
		return domain.Profile{}, err
	}
	if a.Suspended || !a.Verified {
		return domain.Profile{}, domain.ErrNotFound
	}
	return s.withTags(ctx, profileOf(a))
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
	return s.withTags(ctx, saved)
}

// profileOf returns the account's profile, or an empty one when it has no profile row.
func profileOf(a domain.Account) domain.Profile {
	if a.Profile == nil {
		return domain.Profile{UserID: a.UserID}
	}
	return *a.Profile
}
