package usecase

import (
	"context"
	"errors"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/profile/domain"
)

// fakeStore keeps accounts in memory. A test sets err to make every call fail.
type fakeStore struct {
	accounts map[string]domain.Account
	upserts  int
	err      error
}

func newFakeStore(accounts ...domain.Account) *fakeStore {
	s := &fakeStore{accounts: map[string]domain.Account{}}
	for _, a := range accounts {
		s.accounts[a.UserID] = a
	}
	return s
}

func (s *fakeStore) FindAccount(_ context.Context, userID string) (domain.Account, error) {
	if s.err != nil {
		return domain.Account{}, s.err
	}
	a, ok := s.accounts[userID]
	if !ok {
		return domain.Account{}, domain.ErrNotFound
	}
	return a, nil
}

func (s *fakeStore) UpsertProfile(_ context.Context, p domain.Profile) (domain.Profile, error) {
	if s.err != nil {
		return domain.Profile{}, s.err
	}
	a, ok := s.accounts[p.UserID]
	if !ok {
		return domain.Profile{}, domain.ErrNotFound
	}
	s.upserts++
	stored := p
	a.Profile = &stored
	s.accounts[p.UserID] = a
	return p, nil
}

var errDatabase = errors.New("database down")

const (
	adaID   = "00000000-0000-0000-0000-000000000001"
	bobID   = "00000000-0000-0000-0000-000000000002"
	carlID  = "00000000-0000-0000-0000-000000000003"
	dianaID = "00000000-0000-0000-0000-000000000004"
	eveID   = "00000000-0000-0000-0000-000000000005"
	nobody  = "00000000-0000-0000-0000-0000000000ff"
)

func ptr(s string) *string { return &s }

// accounts: Ada has a full profile, Bob is verified with no profile row, Carl is
// suspended, Diana's address was never verified, and Eve has only a name.
func testAccounts() []domain.Account {
	return []domain.Account{
		{UserID: adaID, Verified: true, Profile: &domain.Profile{
			UserID: adaID, FullName: "Ada Lovelace", Headline: ptr("Analyst"), Bio: ptr("Notes on the engine."),
			Institution: ptr("University of London"), Department: ptr("Mathematics"), JobTitle: ptr("Researcher"), Country: ptr("GB"),
		}},
		{UserID: bobID, Verified: true},
		{UserID: carlID, Verified: true, Suspended: true, Profile: &domain.Profile{UserID: carlID, FullName: "Carl Suspended"}},
		{UserID: dianaID, Profile: &domain.Profile{UserID: dianaID, FullName: "Diana Unverified"}},
		{UserID: eveID, Verified: true, Profile: &domain.Profile{UserID: eveID, FullName: "Eve"}},
	}
}
