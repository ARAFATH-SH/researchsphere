package usecase

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"math"
	"slices"
	"strings"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/profile/domain"
)

// fakeStore keeps accounts and tags in memory. A test sets err to make every call fail.
// RunInTx restores the state from before fn when fn fails, like a rollback.
type fakeStore struct {
	accounts map[string]domain.Account
	upserts  int
	err      error

	tags    map[domain.TagKind][]domain.Tag               // the shared vocabularies
	links   map[domain.TagKind]map[string]map[string]bool // kind -> user -> tag IDs
	nextTag int

	txs        int    // RunInTx calls
	lockedUser string // the last user LockUser locked
	suggestQ   string // what SuggestTags was given
	suggestMax int
	suggests   int // SuggestTags calls
	suggestion []domain.Tag

	education     map[string]domain.Education // by ID
	nextEducation int
}

func newFakeStore(accounts ...domain.Account) *fakeStore {
	s := &fakeStore{
		accounts: map[string]domain.Account{},
		tags:     map[domain.TagKind][]domain.Tag{},
		links:    map[domain.TagKind]map[string]map[string]bool{domain.KindInterest: {}, domain.KindSkill: {}},

		education: map[string]domain.Education{},
	}
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

func (s *fakeStore) RunInTx(_ context.Context, fn func(domain.Tx) error) error {
	s.txs++
	if s.err != nil {
		return s.err
	}
	tags, links, next := s.snapshot()
	education, nextEducation := maps.Clone(s.education), s.nextEducation
	if err := fn(s); err != nil {
		s.tags, s.links, s.nextTag = tags, links, next
		s.education, s.nextEducation = education, nextEducation
		return err
	}
	return nil
}

func (s *fakeStore) snapshot() (map[domain.TagKind][]domain.Tag, map[domain.TagKind]map[string]map[string]bool, int) {
	tags := map[domain.TagKind][]domain.Tag{}
	for k, v := range s.tags {
		tags[k] = slices.Clone(v)
	}
	links := map[domain.TagKind]map[string]map[string]bool{}
	for k, users := range s.links {
		links[k] = map[string]map[string]bool{}
		for u, ids := range users {
			links[k][u] = maps.Clone(ids)
		}
	}
	return tags, links, s.nextTag
}

func (s *fakeStore) LockUser(_ context.Context, userID string) error {
	if _, ok := s.accounts[userID]; !ok {
		return domain.ErrNotFound
	}
	s.lockedUser = userID
	return nil
}

// FindOrCreateTag compares names ignoring letter case, as the citext column does.
func (s *fakeStore) FindOrCreateTag(_ context.Context, kind domain.TagKind, name string) (domain.Tag, error) {
	for _, t := range s.tags[kind] {
		if strings.EqualFold(t.Name, name) {
			return t, nil
		}
	}
	s.nextTag++
	t := domain.Tag{ID: fmt.Sprintf("00000000-0000-0000-0000-%012d", s.nextTag), Name: name}
	s.tags[kind] = append(s.tags[kind], t)
	return t, nil
}

func (s *fakeStore) HasTag(_ context.Context, userID string, kind domain.TagKind, tagID string) (bool, error) {
	return s.links[kind][userID][tagID], nil
}

func (s *fakeStore) CountTags(_ context.Context, userID string, kind domain.TagKind) (int, error) {
	return len(s.links[kind][userID]), nil
}

func (s *fakeStore) LinkTag(_ context.Context, userID string, kind domain.TagKind, tagID string) error {
	if s.links[kind][userID] == nil {
		s.links[kind][userID] = map[string]bool{}
	}
	s.links[kind][userID][tagID] = true
	return nil
}

func (s *fakeStore) ListTags(_ context.Context, userID string, kind domain.TagKind) ([]domain.Tag, error) {
	if s.err != nil {
		return nil, s.err
	}
	var out []domain.Tag
	for _, t := range s.tags[kind] {
		if s.links[kind][userID][t.ID] {
			out = append(out, t)
		}
	}
	slices.SortFunc(out, func(a, b domain.Tag) int {
		if c := strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)); c != 0 {
			return c
		}
		return strings.Compare(a.ID, b.ID)
	})
	return out, nil
}

func (s *fakeStore) RemoveTag(_ context.Context, userID string, kind domain.TagKind, tagID string) error {
	if s.err != nil {
		return s.err
	}
	if !s.links[kind][userID][tagID] {
		return domain.ErrNotFound
	}
	delete(s.links[kind][userID], tagID)
	return nil
}

func (s *fakeStore) SuggestTags(_ context.Context, _ domain.TagKind, q string, limit int) ([]domain.Tag, error) {
	s.suggests++
	s.suggestQ, s.suggestMax = q, limit
	return s.suggestion, s.err
}

// tagCount returns how many tags of kind exist in the vocabulary.
func (s *fakeStore) tagCount(kind domain.TagKind) int { return len(s.tags[kind]) }

// giveTags links n new tags of kind to the user.
func (s *fakeStore) giveTags(userID string, kind domain.TagKind, n int) {
	for i := range n {
		t, _ := s.FindOrCreateTag(context.Background(), kind, fmt.Sprintf("Existing %s %d", kind, i))
		_ = s.LinkTag(context.Background(), userID, kind, t.ID)
	}
}

func (s *fakeStore) CountEducation(_ context.Context, userID string) (int, error) {
	n := 0
	for _, e := range s.education {
		if e.UserID == userID {
			n++
		}
	}
	return n, nil
}

func (s *fakeStore) InsertEducation(_ context.Context, e domain.Education) (domain.Education, error) {
	s.nextEducation++
	e.ID = fmt.Sprintf("00000000-0000-0000-0000-0000000e%04d", s.nextEducation)
	s.education[e.ID] = e
	return e, nil
}

// FindEducationForUpdate matches the owner too, as the owner-scoped query does.
func (s *fakeStore) FindEducationForUpdate(_ context.Context, userID, id string) (domain.Education, error) {
	e, ok := s.education[id]
	if !ok || e.UserID != userID {
		return domain.Education{}, domain.ErrNotFound
	}
	return e, nil
}

func (s *fakeStore) UpdateEducation(_ context.Context, e domain.Education) (domain.Education, error) {
	old, ok := s.education[e.ID]
	if !ok || old.UserID != e.UserID {
		return domain.Education{}, domain.ErrNotFound
	}
	s.education[e.ID] = e
	return e, nil
}

func (s *fakeStore) DeleteEducation(_ context.Context, userID, id string) error {
	if s.err != nil {
		return s.err
	}
	e, ok := s.education[id]
	if !ok || e.UserID != userID {
		return domain.ErrNotFound
	}
	delete(s.education, id)
	return nil
}

// ListEducation sorts like the query: current first, then the latest end year, then the
// latest start year, then by ID.
func (s *fakeStore) ListEducation(_ context.Context, userID string) ([]domain.Education, error) {
	if s.err != nil {
		return nil, s.err
	}
	var out []domain.Education
	for _, e := range s.education {
		if e.UserID == userID {
			out = append(out, e)
		}
	}
	end := func(e domain.Education) int {
		if e.EndYear == nil {
			return math.MaxInt
		}
		return *e.EndYear
	}
	slices.SortFunc(out, func(a, b domain.Education) int {
		if c := cmp.Compare(end(b), end(a)); c != 0 {
			return c
		}
		if c := cmp.Compare(b.StartYear, a.StartYear); c != 0 {
			return c
		}
		return strings.Compare(a.ID, b.ID)
	})
	return out, nil
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
