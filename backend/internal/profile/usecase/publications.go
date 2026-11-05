package usecase

import (
	"cmp"
	"context"
	"errors"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/profile/domain"
)

// Publication limits (FR-5.1). Lengths count characters after trimming; migration 000012
// holds them as database constraints. "At most next year" depends on the clock, so only
// the usecase checks it.
const (
	MaxPublications           = 300 // publications per user, confirmed or not
	MaxPublicationTitleLength = 500
	MaxVenueLength            = 300
	MaxAbstractLength         = 5000
	MaxPublicationURLLength   = 500
	MinPublicationYear        = 1900

	DefaultPublicationPageSize = 10
	MaxPublicationPageSize     = 50
)

// doiPattern is the bare form of a DOI: the 10. directory prefix, a registrant code of 4
// to 9 digits, a slash and a suffix without whitespace. The database checks the same.
var doiPattern = regexp.MustCompile(`^10\.\d{4,9}/\S+$`)

// doiPrefixes are removed from the front of a DOI, ignoring letter case.
var doiPrefixes = []string{"https://doi.org/", "http://doi.org/", "https://dx.doi.org/", "http://dx.doi.org/", "doi:"}

// PublicationInput is a publication added by hand. A nil optional field means "not
// set"; a nil Year is refused.
type PublicationInput struct {
	Title    string
	Year     *int
	Venue    *string
	Abstract *string
	URL      *string
	DOI      *string
}

// PublicationPatch is a partial update of a publication. Null clears Venue, Abstract,
// URL or DOI; for Title or Year it is refused like an empty value.
type PublicationPatch struct {
	Title    Field[string]
	Year     Field[int]
	Venue    Field[string]
	Abstract Field[string]
	URL      Field[string]
	DOI      Field[string]
}

// AddedPublication is the result of adding a publication: the record, and whether it
// existed already and the user was linked to it (FR-5.4).
type AddedPublication struct {
	Publication domain.Publication
	Linked      bool
}

// AddPublication validates in and adds it to the user's publications by hand (FR-5.1),
// with the user's link confirmed. When a publication with the same DOI exists, the user
// is linked to it instead and the other fields of in are ignored (FR-5.4). It returns the
// first invalid field's error, in the contract's order; domain.ErrPublicationLimitReached
// when the user already has MaxPublications; domain.ErrPublicationAlreadyAdded when the
// user has already confirmed the publication with this DOI; and domain.ErrNotFound when
// the account no longer exists. Nothing is stored then.
func (s *Service) AddPublication(ctx context.Context, userID string, in PublicationInput) (AddedPublication, error) {
	p, err := s.normalizePublication(domain.Publication{
		Title:    in.Title,
		Year:     in.Year,
		Venue:    in.Venue,
		Abstract: in.Abstract,
		URL:      in.URL,
		DOI:      in.DOI,
	})
	if err != nil {
		return AddedPublication{}, err
	}
	p.Source = domain.SourceManual
	p.CreatedBy = &userID
	return s.storePublication(ctx, userID, p)
}

// storePublication stores p, which is valid, with the user's link confirmed, or links the
// user to the stored publication with p's DOI (FR-5.4), under the MaxPublications limit.
// It returns the errors of AddPublication.
func (s *Service) storePublication(ctx context.Context, userID string, p domain.Publication) (AddedPublication, error) {
	var res AddedPublication
	err := s.store.RunInTx(ctx, func(tx domain.Tx) error {
		// Lock first, so the count below cannot change before the link is stored.
		if err := tx.LockUser(ctx, userID); err != nil {
			return err
		}
		n, err := tx.CountPublicationLinks(ctx, userID)
		if err != nil {
			return err
		}
		if n >= MaxPublications {
			return domain.ErrPublicationLimitReached
		}
		res, err = addToUser(ctx, tx, userID, p)
		return err
	})
	if err != nil {
		return AddedPublication{}, err
	}
	return res, nil
}

// addToUser stores p, which is valid, with the user's link confirmed, or links the user
// to the stored publication with p's DOI (FR-5.4), including one another user stores at
// the same moment. It returns the errors of linkExisting. The caller checks the limit.
func addToUser(ctx context.Context, tx domain.Tx, userID string, p domain.Publication) (AddedPublication, error) {
	if p.DOI != nil {
		existing, err := tx.FindPublicationByDOI(ctx, *p.DOI)
		switch {
		case err == nil:
			return linkExisting(ctx, tx, userID, existing)
		case !errors.Is(err, domain.ErrNotFound):
			return AddedPublication{}, err
		}
	}
	saved, err := tx.InsertPublication(ctx, p)
	if errors.Is(err, domain.ErrDuplicateDOI) {
		// Another user stored the DOI after the lookup above; link to theirs.
		existing, err := tx.FindPublicationByDOI(ctx, *p.DOI)
		if err != nil {
			return AddedPublication{}, err
		}
		return linkExisting(ctx, tx, userID, existing)
	}
	if err != nil {
		return AddedPublication{}, err
	}
	if err := tx.LinkPublication(ctx, userID, saved.ID); err != nil {
		return AddedPublication{}, err
	}
	return AddedPublication{Publication: saved}, nil
}

// linkExisting links the user to the existing publication p, confirmed, or confirms a
// link the user has not confirmed yet. It returns domain.ErrPublicationAlreadyAdded when
// the user has confirmed p already.
func linkExisting(ctx context.Context, tx domain.Tx, userID string, p domain.Publication) (AddedPublication, error) {
	state, err := tx.PublicationLink(ctx, userID, p.ID)
	if err != nil {
		return AddedPublication{}, err
	}
	switch state {
	case domain.Confirmed:
		return AddedPublication{}, domain.ErrPublicationAlreadyAdded
	case domain.Unconfirmed:
		err = tx.ConfirmPublication(ctx, userID, p.ID)
	default:
		err = tx.LinkPublication(ctx, userID, p.ID)
	}
	if err != nil {
		return AddedPublication{}, err
	}
	return AddedPublication{Publication: p, Linked: true}, nil
}

// UpdatePublication changes the fields set in patch on the publication id and validates
// the record as a whole. Only a manual publication that no other user is linked to can
// change; otherwise it returns domain.ErrPublicationSharedReadonly. It returns
// domain.ErrNotFound when the user is not linked to the publication; that check comes
// before validation, so an invalid patch to someone else's publication reveals nothing.
// It returns domain.ErrDuplicateDOI when another publication has the new DOI.
func (s *Service) UpdatePublication(ctx context.Context, userID, id string, patch PublicationPatch) (domain.Publication, error) {
	var saved domain.Publication
	err := s.store.RunInTx(ctx, func(tx domain.Tx) error {
		p, err := tx.FindPublicationForUpdate(ctx, userID, id)
		if err != nil {
			return err
		}
		authors, err := tx.CountPublicationAuthors(ctx, p.ID)
		if err != nil {
			return err
		}
		if authors > 1 || p.Source != domain.SourceManual {
			return domain.ErrPublicationSharedReadonly
		}
		if p, err = s.normalizePublication(applyPublication(p, patch)); err != nil {
			return err
		}
		saved, err = tx.UpdatePublication(ctx, p)
		return err
	})
	if err != nil {
		return domain.Publication{}, err
	}
	return saved, nil
}

// RemovePublication removes the user's link to the publication id (FR-5.7) and deletes
// the publication when no other user is linked to it. It returns domain.ErrNotFound when
// the user is not linked to it.
func (s *Service) RemovePublication(ctx context.Context, userID, id string) error {
	return s.store.RunInTx(ctx, func(tx domain.Tx) error {
		// Lock the record before the link goes, as account deletion does, so a user
		// linking to it at the same moment either keeps it or waits for the delete.
		p, err := tx.FindPublicationForUpdate(ctx, userID, id)
		if err != nil {
			return err
		}
		if err := tx.UnlinkPublication(ctx, userID, p.ID); err != nil {
			return err
		}
		return tx.DeletePublicationIfOrphaned(ctx, p.ID)
	})
}

// ListOwnPublications returns one page of the caller's confirmed publications (FR-5.5),
// in the order of ListPublications.
func (s *Service) ListOwnPublications(ctx context.Context, userID string, page, pageSize int) (domain.PublicationPage, error) {
	return s.publicationPage(ctx, userID, page, pageSize)
}

// ListPublications returns one page of userID's confirmed publications to a logged-in
// user (FR-5.5): by year, newest first, a publication without a year last, then by when
// the user added it, newest first. pageSize above MaxPublicationPageSize is reduced to
// it, and a page or page size below 1 returns domain.ErrInvalidPage. An unknown,
// suspended or unverified account, or an owner who has blocked viewerID (FR-16.9), gives
// domain.ErrNotFound, as for the profile.
func (s *Service) ListPublications(ctx context.Context, viewerID, userID string, page, pageSize int) (domain.PublicationPage, error) {
	a, err := s.store.FindAccount(ctx, userID)
	if err != nil {
		return domain.PublicationPage{}, err
	}
	if a.Suspended || !a.Verified {
		return domain.PublicationPage{}, domain.ErrNotFound
	}
	blocks, err := s.blockState(ctx, viewerID, userID)
	if err != nil {
		return domain.PublicationPage{}, err
	}
	if blocks.ByOwner {
		return domain.PublicationPage{}, domain.ErrNotFound
	}
	return s.publicationPage(ctx, userID, page, pageSize)
}

// publicationPage reads all the user's confirmed publications, at most MaxPublications,
// sorts them and cuts out one page.
func (s *Service) publicationPage(ctx context.Context, userID string, page, pageSize int) (domain.PublicationPage, error) {
	if page < 1 || pageSize < 1 {
		return domain.PublicationPage{}, domain.ErrInvalidPage
	}
	pageSize = min(pageSize, MaxPublicationPageSize)
	all, err := s.store.ListConfirmedPublications(ctx, userID)
	if err != nil {
		return domain.PublicationPage{}, err
	}
	sortPublications(all)
	items := []domain.Publication{}
	if pages := (len(all) + pageSize - 1) / pageSize; page <= pages {
		start := (page - 1) * pageSize
		items = all[start:min(start+pageSize, len(all))]
	}
	return domain.PublicationPage{Items: items, Page: page, PageSize: pageSize, Total: len(all)}, nil
}

// sortPublications orders by year, newest first and no year last, then by when the
// user added the publication, newest first; the ID makes the order stable.
func sortPublications(ps []domain.Publication) {
	slices.SortFunc(ps, func(a, b domain.Publication) int {
		switch {
		case a.Year == nil && b.Year != nil:
			return 1
		case a.Year != nil && b.Year == nil:
			return -1
		case a.Year != nil && *a.Year != *b.Year:
			return cmp.Compare(*b.Year, *a.Year)
		}
		if c := b.AddedAt.Compare(a.AddedAt); c != 0 {
			return c
		}
		return cmp.Compare(a.ID, b.ID)
	})
}

// applyPublication copies the fields set in patch onto p. A null Title or Year becomes
// the empty value, which validation then refuses in the usual field order.
func applyPublication(p domain.Publication, patch PublicationPatch) domain.Publication {
	if patch.Title.Set {
		p.Title = patch.Title.Value // "" when null
	}
	if patch.Year.Set {
		p.Year = nullable(patch.Year)
	}
	if patch.Venue.Set {
		p.Venue = nullable(patch.Venue)
	}
	if patch.Abstract.Set {
		p.Abstract = nullable(patch.Abstract)
	}
	if patch.URL.Set {
		p.URL = nullable(patch.URL)
	}
	if patch.DOI.Set {
		p.DOI = nullable(patch.DOI)
	}
	return p
}

// normalizePublication trims the text, turns blank optional text into nil and checks
// each field in the contract's order: title, year, venue, abstract, URL, DOI.
func (s *Service) normalizePublication(p domain.Publication) (domain.Publication, error) {
	p.Title = strings.TrimSpace(p.Title)
	if p.Title == "" || !fits(p.Title, MaxPublicationTitleLength, true) {
		return p, domain.ErrInvalidTitle
	}
	if p.Year == nil || *p.Year < MinPublicationYear || *p.Year > s.now().UTC().Year()+1 {
		return p, domain.ErrInvalidYear
	}
	var err error
	if p.Venue, err = optional(p.Venue, MaxVenueLength, true, domain.ErrInvalidVenue); err != nil {
		return p, err
	}
	if p.Abstract, err = optional(p.Abstract, MaxAbstractLength, false, domain.ErrInvalidAbstract); err != nil {
		return p, err
	}
	if p.URL, err = publicationURL(p.URL); err != nil {
		return p, err
	}
	if p.DOI, err = NormalizeDOI(p.DOI); err != nil {
		return p, err
	}
	return p, nil
}

// publicationURL trims v and returns nil when it is missing or blank. Otherwise it
// requires an http or https URL with a host, no whitespace and at most
// MaxPublicationURLLength characters.
func publicationURL(v *string) (*string, error) {
	if v == nil {
		return nil, nil
	}
	s := strings.TrimSpace(*v)
	if s == "" {
		return nil, nil
	}
	lower := strings.ToLower(s)
	if !strings.HasPrefix(lower, "http://") && !strings.HasPrefix(lower, "https://") ||
		utf8.RuneCountInString(s) > MaxPublicationURLLength || strings.IndexFunc(s, unicode.IsSpace) >= 0 {
		return nil, domain.ErrInvalidPublicationURL
	}
	u, err := url.Parse(s)
	if err != nil || u.Host == "" {
		return nil, domain.ErrInvalidPublicationURL
	}
	return &s, nil
}

// NormalizeDOI returns the bare, lower-case form of a DOI: it trims v, removes a leading
// https://doi.org/, http://doi.org/, https://dx.doi.org/, http://dx.doi.org/ or doi:
// (ignoring letter case) and lower-cases the rest. A missing or blank DOI gives nil; one
// that is not of the form 10.NNNN/... gives domain.ErrInvalidDOI. DOI lookup and ORCID
// import (slices 11.2 and 11.3) store DOIs in the same form.
func NormalizeDOI(v *string) (*string, error) {
	if v == nil {
		return nil, nil
	}
	s := strings.TrimSpace(*v)
	if s == "" {
		return nil, nil
	}
	s = strings.ToLower(s)
	for _, prefix := range doiPrefixes {
		if rest, ok := strings.CutPrefix(s, prefix); ok {
			s = strings.TrimSpace(rest)
			break
		}
	}
	if !doiPattern.MatchString(s) {
		return nil, domain.ErrInvalidDOI
	}
	return &s, nil
}
