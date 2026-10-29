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
	"time"

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

	experience     map[string]domain.Experience // by ID
	nextExperience int

	languages     map[string]domain.Language // by ID
	nextLanguage  int
	languageError error // returned by InsertLanguage and UpdateLanguage when set

	emails       map[string]string         // the account's address, by user ID
	contacts     map[string]domain.Contact // by user ID, without links
	profileLinks map[string]domain.Link    // by ID
	nextLink     int

	publications map[string]domain.Publication     // by ID
	pubLinks     map[string]map[string]fakePubLink // publication ID -> user ID -> link
	nextPub      int
	clock        time.Time // the time of the next link; each link moves it on a second
	// hideDOIOnce makes the next FindPublicationByDOI miss an existing record, as if
	// another user stored it just after the lookup.
	hideDOIOnce bool
}

// fakePubLink is a user's link to a publication.
type fakePubLink struct {
	confirmed bool
	at        time.Time
}

func newFakeStore(accounts ...domain.Account) *fakeStore {
	s := &fakeStore{
		accounts: map[string]domain.Account{},
		tags:     map[domain.TagKind][]domain.Tag{},
		links:    map[domain.TagKind]map[string]map[string]bool{domain.KindInterest: {}, domain.KindSkill: {}},

		education:  map[string]domain.Education{},
		experience: map[string]domain.Experience{},
		languages:  map[string]domain.Language{},

		emails:       map[string]string{},
		contacts:     map[string]domain.Contact{},
		profileLinks: map[string]domain.Link{},

		publications: map[string]domain.Publication{},
		pubLinks:     map[string]map[string]fakePubLink{},
		clock:        time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC),
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
	experience, nextExperience := maps.Clone(s.experience), s.nextExperience
	languages, nextLanguage := maps.Clone(s.languages), s.nextLanguage
	profileLinks, nextLink := maps.Clone(s.profileLinks), s.nextLink
	publications, nextPub := maps.Clone(s.publications), s.nextPub
	pubLinks := map[string]map[string]fakePubLink{}
	for id, users := range s.pubLinks {
		pubLinks[id] = maps.Clone(users)
	}
	if err := fn(s); err != nil {
		s.publications, s.pubLinks, s.nextPub = publications, pubLinks, nextPub
		s.tags, s.links, s.nextTag = tags, links, next
		s.education, s.nextEducation = education, nextEducation
		s.experience, s.nextExperience = experience, nextExperience
		s.languages, s.nextLanguage = languages, nextLanguage
		s.profileLinks, s.nextLink = profileLinks, nextLink
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

func (s *fakeStore) CountExperience(_ context.Context, userID string) (int, error) {
	n := 0
	for _, e := range s.experience {
		if e.UserID == userID {
			n++
		}
	}
	return n, nil
}

// InsertExperience numbers IDs in insertion order, so sorting by ID sorts by creation.
func (s *fakeStore) InsertExperience(_ context.Context, e domain.Experience) (domain.Experience, error) {
	s.nextExperience++
	e.ID = fmt.Sprintf("00000000-0000-0000-0000-0000000f%04d", s.nextExperience)
	s.experience[e.ID] = e
	return e, nil
}

// FindExperienceForUpdate matches the owner too, as the owner-scoped query does.
func (s *fakeStore) FindExperienceForUpdate(_ context.Context, userID, id string) (domain.Experience, error) {
	e, ok := s.experience[id]
	if !ok || e.UserID != userID {
		return domain.Experience{}, domain.ErrNotFound
	}
	return e, nil
}

func (s *fakeStore) UpdateExperience(_ context.Context, e domain.Experience) (domain.Experience, error) {
	old, ok := s.experience[e.ID]
	if !ok || old.UserID != e.UserID {
		return domain.Experience{}, domain.ErrNotFound
	}
	s.experience[e.ID] = e
	return e, nil
}

func (s *fakeStore) DeleteExperience(_ context.Context, userID, id string) error {
	if s.err != nil {
		return s.err
	}
	e, ok := s.experience[id]
	if !ok || e.UserID != userID {
		return domain.ErrNotFound
	}
	delete(s.experience, id)
	return nil
}

// ListExperience sorts like the query: current first, then the latest start, then the
// oldest entry (the lowest ID here).
func (s *fakeStore) ListExperience(_ context.Context, userID string) ([]domain.Experience, error) {
	if s.err != nil {
		return nil, s.err
	}
	var out []domain.Experience
	for _, e := range s.experience {
		if e.UserID == userID {
			out = append(out, e)
		}
	}
	slices.SortFunc(out, func(a, b domain.Experience) int {
		if a.IsCurrent != b.IsCurrent {
			if a.IsCurrent {
				return -1
			}
			return 1
		}
		if c := cmp.Compare(b.Start.Year, a.Start.Year); c != 0 {
			return c
		}
		if c := cmp.Compare(b.Start.Month, a.Start.Month); c != 0 {
			return c
		}
		return strings.Compare(a.ID, b.ID)
	})
	return out, nil
}

func (s *fakeStore) CountLanguages(_ context.Context, userID string) (int, error) {
	n := 0
	for _, l := range s.languages {
		if l.UserID == userID {
			n++
		}
	}
	return n, nil
}

// LanguageNameTaken compares names ignoring letter case, as the unique index on
// lower(name) does.
func (s *fakeStore) LanguageNameTaken(_ context.Context, userID, name, exceptID string) (bool, error) {
	for _, l := range s.languages {
		if l.UserID == userID && l.ID != exceptID && strings.EqualFold(l.Name, name) {
			return true, nil
		}
	}
	return false, nil
}

// InsertLanguage numbers IDs in insertion order, so sorting by ID sorts by creation.
func (s *fakeStore) InsertLanguage(_ context.Context, l domain.Language) (domain.Language, error) {
	if s.languageError != nil {
		return domain.Language{}, s.languageError
	}
	s.nextLanguage++
	l.ID = fmt.Sprintf("00000000-0000-0000-0000-0000000a%04d", s.nextLanguage)
	s.languages[l.ID] = l
	return l, nil
}

// FindLanguageForUpdate matches the owner too, as the owner-scoped query does.
func (s *fakeStore) FindLanguageForUpdate(_ context.Context, userID, id string) (domain.Language, error) {
	l, ok := s.languages[id]
	if !ok || l.UserID != userID {
		return domain.Language{}, domain.ErrNotFound
	}
	return l, nil
}

func (s *fakeStore) UpdateLanguage(_ context.Context, l domain.Language) (domain.Language, error) {
	if s.languageError != nil {
		return domain.Language{}, s.languageError
	}
	old, ok := s.languages[l.ID]
	if !ok || old.UserID != l.UserID {
		return domain.Language{}, domain.ErrNotFound
	}
	s.languages[l.ID] = l
	return l, nil
}

func (s *fakeStore) DeleteLanguage(_ context.Context, userID, id string) error {
	if s.err != nil {
		return s.err
	}
	l, ok := s.languages[id]
	if !ok || l.UserID != userID {
		return domain.ErrNotFound
	}
	delete(s.languages, id)
	return nil
}

// ListLanguages returns the oldest first (the lowest ID here), like the query.
func (s *fakeStore) ListLanguages(_ context.Context, userID string) ([]domain.Language, error) {
	if s.err != nil {
		return nil, s.err
	}
	var out []domain.Language
	for _, l := range s.languages {
		if l.UserID == userID {
			out = append(out, l)
		}
	}
	slices.SortFunc(out, func(a, b domain.Language) int { return strings.Compare(a.ID, b.ID) })
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

func (s *fakeStore) FindContact(_ context.Context, userID string) (domain.Contact, error) {
	if s.err != nil {
		return domain.Contact{}, s.err
	}
	if _, ok := s.accounts[userID]; !ok {
		return domain.Contact{}, domain.ErrNotFound
	}
	c, ok := s.contacts[userID]
	if !ok {
		c = domain.Contact{UserID: userID, EmailVisibility: domain.OnlyMe, PhoneVisibility: domain.OnlyMe, BirthdayVisibility: domain.OnlyMe}
	}
	c.Email = s.emails[userID]
	return c, nil
}

func (s *fakeStore) UpsertContact(_ context.Context, c domain.Contact) error {
	if s.err != nil {
		return s.err
	}
	if _, ok := s.accounts[c.UserID]; !ok {
		return domain.ErrNotFound
	}
	c.Email, c.Links = "", nil
	s.contacts[c.UserID] = c
	return nil
}

func (s *fakeStore) ListLinks(_ context.Context, userID string) ([]domain.Link, error) {
	if s.err != nil {
		return nil, s.err
	}
	var out []domain.Link
	for _, l := range s.profileLinks {
		if l.UserID == userID {
			out = append(out, l)
		}
	}
	slices.SortFunc(out, func(a, b domain.Link) int { return cmp.Compare(a.ID, b.ID) })
	return out, nil
}

func (s *fakeStore) DeleteLink(_ context.Context, userID, id string) error {
	if s.err != nil {
		return s.err
	}
	l, ok := s.profileLinks[id]
	if !ok || l.UserID != userID {
		return domain.ErrNotFound
	}
	delete(s.profileLinks, id)
	return nil
}

func (s *fakeStore) CountLinks(_ context.Context, userID string) (int, error) {
	links, err := s.ListLinks(context.Background(), userID)
	return len(links), err
}

func (s *fakeStore) InsertLink(_ context.Context, l domain.Link) (domain.Link, error) {
	if _, ok := s.accounts[l.UserID]; !ok {
		return domain.Link{}, domain.ErrNotFound
	}
	s.nextLink++
	l.ID = fmt.Sprintf("link-%02d", s.nextLink)
	s.profileLinks[l.ID] = l
	return l, nil
}

// giveLinks adds n links to the user, as earlier requests would have.
func (s *fakeStore) giveLinks(userID string, n int) {
	for range n {
		s.nextLink++
		id := fmt.Sprintf("link-%02d", s.nextLink)
		s.profileLinks[id] = domain.Link{ID: id, UserID: userID, Kind: domain.LinkWebsite, URL: "https://example.org/" + id}
	}
}

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

func (s *fakeStore) CountPublicationLinks(_ context.Context, userID string) (int, error) {
	n := 0
	for _, users := range s.pubLinks {
		if _, ok := users[userID]; ok {
			n++
		}
	}
	return n, nil
}

func (s *fakeStore) FindPublicationByDOI(_ context.Context, doi string) (domain.Publication, error) {
	if s.hideDOIOnce {
		s.hideDOIOnce = false
		return domain.Publication{}, domain.ErrNotFound
	}
	for _, p := range s.publications {
		if p.DOI != nil && *p.DOI == doi {
			return p, nil
		}
	}
	return domain.Publication{}, domain.ErrNotFound
}

// InsertPublication refuses a DOI another record has, as the unique index does.
func (s *fakeStore) InsertPublication(_ context.Context, p domain.Publication) (domain.Publication, error) {
	if p.DOI != nil {
		for _, o := range s.publications {
			if o.DOI != nil && *o.DOI == *p.DOI {
				return domain.Publication{}, domain.ErrDuplicateDOI
			}
		}
	}
	s.nextPub++
	p.ID = fmt.Sprintf("pub-%03d", s.nextPub)
	p.CreatedAt, p.UpdatedAt = s.clock, s.clock
	s.publications[p.ID] = p
	return p, nil
}

func (s *fakeStore) PublicationLink(_ context.Context, userID, publicationID string) (domain.LinkState, error) {
	l, ok := s.pubLinks[publicationID][userID]
	switch {
	case !ok:
		return domain.NotLinked, nil
	case l.confirmed:
		return domain.Confirmed, nil
	default:
		return domain.Unconfirmed, nil
	}
}

func (s *fakeStore) LinkPublication(_ context.Context, userID, publicationID string) error {
	s.link(userID, publicationID, true)
	return nil
}

func (s *fakeStore) ConfirmPublication(_ context.Context, userID, publicationID string) error {
	l := s.pubLinks[publicationID][userID]
	l.confirmed = true
	s.pubLinks[publicationID][userID] = l
	return nil
}

func (s *fakeStore) FindPublicationForUpdate(_ context.Context, userID, id string) (domain.Publication, error) {
	if _, ok := s.pubLinks[id][userID]; !ok {
		return domain.Publication{}, domain.ErrNotFound
	}
	return s.publications[id], nil
}

func (s *fakeStore) CountPublicationAuthors(_ context.Context, publicationID string) (int, error) {
	return len(s.pubLinks[publicationID]), nil
}

func (s *fakeStore) UpdatePublication(_ context.Context, p domain.Publication) (domain.Publication, error) {
	if p.DOI != nil {
		for _, o := range s.publications {
			if o.ID != p.ID && o.DOI != nil && *o.DOI == *p.DOI {
				return domain.Publication{}, domain.ErrDuplicateDOI
			}
		}
	}
	p.UpdatedAt = s.clock
	s.publications[p.ID] = p
	return p, nil
}

func (s *fakeStore) UnlinkPublication(_ context.Context, userID, publicationID string) error {
	if _, ok := s.pubLinks[publicationID][userID]; !ok {
		return domain.ErrNotFound
	}
	delete(s.pubLinks[publicationID], userID)
	return nil
}

func (s *fakeStore) DeletePublicationIfOrphaned(_ context.Context, publicationID string) error {
	if len(s.pubLinks[publicationID]) == 0 {
		delete(s.pubLinks, publicationID)
		delete(s.publications, publicationID)
	}
	return nil
}

func (s *fakeStore) ListConfirmedPublications(_ context.Context, userID string) ([]domain.Publication, error) {
	if s.err != nil {
		return nil, s.err
	}
	var out []domain.Publication
	for id, users := range s.pubLinks {
		if l, ok := users[userID]; ok && l.confirmed {
			p := s.publications[id]
			p.AddedAt = l.at
			out = append(out, p)
		}
	}
	return out, nil
}

func (s *fakeStore) CountConfirmedPublications(ctx context.Context, userID string) (int, error) {
	ps, err := s.ListConfirmedPublications(ctx, userID)
	return len(ps), err
}

// link links the user to the publication at the fake clock's time.
func (s *fakeStore) link(userID, publicationID string, confirmed bool) {
	if s.pubLinks[publicationID] == nil {
		s.pubLinks[publicationID] = map[string]fakePubLink{}
	}
	s.pubLinks[publicationID][userID] = fakePubLink{confirmed: confirmed, at: s.clock}
	s.clock = s.clock.Add(time.Second)
}

// givePublication stores p as a record already in the database, with the users linked
// to it (confirmed when confirmed is true), and returns its ID.
func (s *fakeStore) givePublication(p domain.Publication, confirmed bool, userIDs ...string) string {
	s.nextPub++
	p.ID = fmt.Sprintf("pub-%03d", s.nextPub)
	if p.Source == "" {
		p.Source = domain.SourceManual
	}
	s.publications[p.ID] = p
	for _, u := range userIDs {
		s.link(u, p.ID, confirmed)
	}
	return p.ID
}

// fakeSource answers FetchWork with work or err and counts the calls.
type fakeSource struct {
	work  domain.WorkMetadata
	err   error
	calls int
}

func (f *fakeSource) FetchWork(_ context.Context, doi string) (domain.WorkMetadata, error) {
	f.calls++
	if f.err != nil {
		return domain.WorkMetadata{}, f.err
	}
	w := f.work
	w.DOI = doi
	return w, nil
}

// fakeCache keeps works by DOI; err makes every call fail.
type fakeCache struct {
	works map[string]domain.WorkMetadata
	ttl   time.Duration // given to the last PutWork
	err   error
}

func (f *fakeCache) GetWork(_ context.Context, doi string) (domain.WorkMetadata, bool, error) {
	if f.err != nil {
		return domain.WorkMetadata{}, false, f.err
	}
	w, ok := f.works[doi]
	return w, ok, nil
}

func (f *fakeCache) PutWork(_ context.Context, w domain.WorkMetadata, ttl time.Duration) error {
	if f.err != nil {
		return f.err
	}
	if f.works == nil {
		f.works = map[string]domain.WorkMetadata{}
	}
	f.works[w.DOI], f.ttl = w, ttl
	return nil
}

// fakeLimiter counts lookups per user; err makes every call fail.
type fakeLimiter struct {
	used map[string]int
	left time.Duration // returned by Used
	err  error
}

func (f *fakeLimiter) Used(_ context.Context, userID string) (int, time.Duration, error) {
	if f.err != nil {
		return 0, 0, f.err
	}
	return f.used[userID], f.left, nil
}

func (f *fakeLimiter) Record(_ context.Context, userID string, _ time.Duration) error {
	if f.err != nil {
		return f.err
	}
	if f.used == nil {
		f.used = map[string]int{}
	}
	f.used[userID]++
	return nil
}
