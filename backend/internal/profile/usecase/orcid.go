package usecase

import (
	"context"
	"errors"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/profile/domain"
)

// ORCID import limits (FR-5.3, docs/decisions.md).
const (
	ORCIDListsPerWindow = 10        // list calls that reach ORCID, per user
	ORCIDListWindow     = time.Hour // fixed window, opened by the first call
	ORCIDWorksCacheTTL  = time.Hour
	MaxImportPutCodes   = 100 // works per import request
	// ImportEnrichBudget bounds the OpenAlex calls of one import; works not fetched by
	// then are stored from the ORCID data.
	ImportEnrichBudget = 15 * time.Second
	maxWorkTypeLength  = 100
)

// ORCIDImport is what ORCID imports need: the ORCID API, the cache of its lists and the
// per-user limit. Every field is required.
type ORCIDImport struct {
	Source  domain.ORCIDSource
	Cache   domain.ORCIDWorksCache
	Limiter domain.LookupLimiter
	Logger  *slog.Logger
}

// WithORCIDImport lets the Service list and import ORCID works (FR-5.3).
func WithORCIDImport(o ORCIDImport) Option {
	return func(s *Service) { s.orcid = o }
}

var orcidPattern = regexp.MustCompile(`^\d{4}-\d{4}-\d{4}-\d{3}[\dX]$`)

// orcidPrefixes are removed from the front of an ORCID iD, ignoring letter case.
var orcidPrefixes = []string{"https://orcid.org/", "http://orcid.org/"}

// NormalizeORCID returns the bare form of an ORCID iD: it trims v, removes a leading
// https://orcid.org/ or http://orcid.org/ (ignoring letter case) and upper-cases a final
// x. A missing or blank iD gives nil; one that is not of the form 0000-0000-0000-000X
// with a valid ISO 7064 11,2 check digit gives domain.ErrInvalidORCID.
func NormalizeORCID(v *string) (*string, error) {
	if v == nil {
		return nil, nil
	}
	s := strings.TrimSpace(*v)
	if s == "" {
		return nil, nil
	}
	for _, prefix := range orcidPrefixes {
		if len(s) >= len(prefix) && strings.EqualFold(s[:len(prefix)], prefix) {
			s = s[len(prefix):]
			break
		}
	}
	s = strings.ToUpper(s)
	if !orcidPattern.MatchString(s) || !orcidChecksumOK(s) {
		return nil, domain.ErrInvalidORCID
	}
	return &s, nil
}

// orcidChecksumOK checks the last character of id, which matches orcidPattern, against
// the ISO 7064 11,2 check digit of its other fifteen digits.
func orcidChecksumOK(id string) bool {
	digits := strings.ReplaceAll(id, "-", "")
	total := 0
	for _, r := range digits[:15] {
		total = (total + int(r-'0')) * 2
	}
	check := (12 - total%11) % 11
	want := byte('0' + check)
	if check == 10 {
		want = 'X'
	}
	return digits[15] == want
}

// SetOrcidID saves the ORCID iD raw, in any form NormalizeORCID accepts, on the user's
// profile, or clears it when raw is nil or blank, and returns what was saved. It returns
// domain.ErrInvalidORCID, domain.ErrORCIDTaken when another profile has the iD, and
// domain.ErrNotFound when the account no longer exists.
func (s *Service) SetOrcidID(ctx context.Context, userID string, raw *string) (*string, error) {
	id, err := NormalizeORCID(raw)
	if err != nil {
		return nil, err
	}
	if err := s.store.SetOrcidID(ctx, userID, id); err != nil {
		return nil, err
	}
	return id, nil
}

// ORCIDWorkItem is one work of the user's ORCID record, with whether a publication with
// its DOI is stored (FR-5.4). PublicationID is set only when ExistsAlready is true, and
// LinkedToMe says whether the user has confirmed it.
type ORCIDWorkItem struct {
	domain.ORCIDWork
	ExistsAlready bool
	PublicationID string
	LinkedToMe    bool
}

// ORCIDWorkList is the works of the ORCID iD on the user's profile.
type ORCIDWorkList struct {
	OrcidID string
	Items   []ORCIDWorkItem
}

// ListORCIDWorks returns the works of the ORCID iD on the user's profile (FR-5.3), each
// matched by DOI against the stored publications. It returns domain.ErrORCIDNotSet,
// domain.ErrORCIDNotFound, domain.ErrMetadataUnavailable, *domain.RateLimitedError and
// domain.ErrNotFound when the account no longer exists.
func (s *Service) ListORCIDWorks(ctx context.Context, userID string) (ORCIDWorkList, error) {
	orcidID, works, err := s.orcidWorks(ctx, userID)
	if err != nil {
		return ORCIDWorkList{}, err
	}
	var matches map[string]domain.DOIMatch
	err = s.store.RunInTx(ctx, func(tx domain.Tx) error {
		var err error
		matches, err = tx.MatchPublicationsByDOI(ctx, userID, worksDOIs(works))
		return err
	})
	if err != nil {
		return ORCIDWorkList{}, err
	}
	list := ORCIDWorkList{OrcidID: orcidID, Items: make([]ORCIDWorkItem, len(works))}
	for i, w := range works {
		item := ORCIDWorkItem{ORCIDWork: w}
		if w.DOI != nil {
			if m, ok := matches[*w.DOI]; ok {
				item.ExistsAlready, item.PublicationID, item.LinkedToMe = true, m.PublicationID, m.State == domain.Confirmed
			}
		}
		list.Items[i] = item
	}
	return list, nil
}

// ImportOutcome says what an import did with one work.
type ImportOutcome string

// Import outcomes.
const (
	ImportCreated ImportOutcome = "created" // a new record was stored
	ImportLinked  ImportOutcome = "linked"  // linked to a stored record, or a link confirmed
	ImportSkipped ImportOutcome = "skipped" // the user had confirmed it already
)

// ImportedWork is the outcome for one put-code.
type ImportedWork struct {
	PutCode       string
	Outcome       ImportOutcome
	PublicationID string
}

// ImportResult is the outcome for each put-code, in the order given, and their counts.
type ImportResult struct {
	Items                    []ImportedWork
	Created, Linked, Skipped int
}

// ImportORCIDWorks adds the works with the put-codes from the user's ORCID list to the
// user's publications (FR-5.3), with the user's links confirmed. A work whose DOI is
// stored links the user to that record (FR-5.4). A new DOI is fetched from OpenAlex as
// in AddPublicationFromDOI (source doi); when that fails, the record is made from the
// ORCID data (source orcid), as is a work without a DOI. A work the user has confirmed,
// or a second one with the same DOI, is skipped. The MaxPublications limit is checked
// before anything is fetched or saved, and the works are saved in one transaction.
//
// It returns domain.ErrInvalidPutCode when putCodes is empty, longer than
// MaxImportPutCodes, repeats a code or has one not in the list;
// domain.ErrPublicationLimitReached; and the errors of ListORCIDWorks.
func (s *Service) ImportORCIDWorks(ctx context.Context, userID string, putCodes []string) (ImportResult, error) {
	if len(putCodes) == 0 || len(putCodes) > MaxImportPutCodes {
		return ImportResult{}, domain.ErrInvalidPutCode
	}
	seen := make(map[string]bool, len(putCodes))
	for _, c := range putCodes {
		if seen[c] {
			return ImportResult{}, domain.ErrInvalidPutCode
		}
		seen[c] = true
	}
	_, works, err := s.orcidWorks(ctx, userID)
	if err != nil {
		return ImportResult{}, err
	}
	byCode := make(map[string]domain.ORCIDWork, len(works))
	for _, w := range works {
		byCode[w.PutCode] = w
	}
	selected := make([]domain.ORCIDWork, len(putCodes))
	for i, c := range putCodes {
		w, ok := byCode[c]
		if !ok {
			return ImportResult{}, domain.ErrInvalidPutCode
		}
		selected[i] = w
	}

	toFetch, err := s.planImport(ctx, userID, selected)
	if err != nil {
		return ImportResult{}, err
	}
	fetched := s.enrich(ctx, userID, toFetch)

	var res ImportResult
	err = s.store.RunInTx(ctx, func(tx domain.Tx) error {
		res = ImportResult{Items: make([]ImportedWork, len(selected))}
		if err := tx.LockUser(ctx, userID); err != nil {
			return err
		}
		for i, w := range selected {
			p := orcidPublication(w, userID)
			if w.DOI != nil {
				if m, ok := fetched[*w.DOI]; ok {
					p = fetchedPublication(m, w, userID)
				}
			}
			added, err := addToUser(ctx, tx, userID, p)
			item := ImportedWork{PutCode: w.PutCode, PublicationID: added.Publication.ID}
			switch {
			case errors.Is(err, domain.ErrPublicationAlreadyAdded):
				existing, err := tx.FindPublicationByDOI(ctx, *p.DOI)
				if err != nil {
					return err
				}
				item.Outcome, item.PublicationID = ImportSkipped, existing.ID
				res.Skipped++
			case err != nil:
				return err
			case added.Linked:
				item.Outcome = ImportLinked
				res.Linked++
			default:
				item.Outcome = ImportCreated
				res.Created++
			}
			res.Items[i] = item
		}
		// Checked again under the lock: another request may have added publications since
		// planImport. Going over rolls the whole import back.
		n, err := tx.CountPublicationLinks(ctx, userID)
		if err != nil {
			return err
		}
		if n > MaxPublications {
			return domain.ErrPublicationLimitReached
		}
		return nil
	})
	if err != nil {
		return ImportResult{}, err
	}
	return res, nil
}

// planImport checks that importing the works keeps the user within MaxPublications, and
// returns the DOIs of the works that would be new records, in order, each once. It reads
// only, so a refused import has saved and fetched nothing.
func (s *Service) planImport(ctx context.Context, userID string, works []domain.ORCIDWork) ([]string, error) {
	var toFetch []string
	err := s.store.RunInTx(ctx, func(tx domain.Tx) error {
		n, err := tx.CountPublicationLinks(ctx, userID)
		if err != nil {
			return err
		}
		matches, err := tx.MatchPublicationsByDOI(ctx, userID, worksDOIs(works))
		if err != nil {
			return err
		}
		adds := 0
		seen := map[string]bool{}
		for _, w := range works {
			if w.DOI == nil {
				adds++
				continue
			}
			if seen[*w.DOI] {
				continue // skipped: the first work with the DOI adds it
			}
			seen[*w.DOI] = true
			m, ok := matches[*w.DOI]
			switch {
			case !ok:
				adds++
				toFetch = append(toFetch, *w.DOI)
			case m.State == domain.NotLinked:
				adds++
			}
		}
		if n+adds > MaxPublications {
			return domain.ErrPublicationLimitReached
		}
		return nil
	})
	return toFetch, err
}

// enrich fetches the metadata of the DOIs through the DOI lookup path (its cache and its
// limit), one after another. It stops at the first answer that says OpenAlex is
// unreachable or the lookup limit is used up, and when ImportEnrichBudget has passed; a
// DOI OpenAlex does not know is passed over. The works not fetched are stored from the
// ORCID data.
func (s *Service) enrich(ctx context.Context, userID string, dois []string) map[string]domain.WorkMetadata {
	out := make(map[string]domain.WorkMetadata)
	if len(dois) == 0 {
		return out
	}
	ctx, cancel := context.WithTimeout(ctx, ImportEnrichBudget)
	defer cancel()
	for _, doi := range dois {
		w, err := s.metadata(ctx, userID, doi)
		switch {
		case err == nil:
			out[doi] = w
		case errors.Is(err, domain.ErrWorkNotFound):
		default:
			s.logger().InfoContext(ctx, "ORCID import goes on with ORCID data only", slog.Any("error", err))
			return out
		}
	}
	return out
}

// orcidPublication is the record made from an ORCID work, which is normalized.
func orcidPublication(w domain.ORCIDWork, userID string) domain.Publication {
	return domain.Publication{
		DOI: w.DOI, Title: w.Title, Year: w.Year, Venue: w.Venue, URL: w.URL,
		Source: domain.SourceORCID, CreatedBy: &userID,
	}
}

// fetchedPublication is the record made from OpenAlex's metadata of the work's DOI, with
// the ORCID year and venue where OpenAlex has none.
func fetchedPublication(m domain.WorkMetadata, w domain.ORCIDWork, userID string) domain.Publication {
	p := domain.Publication{
		DOI: &m.DOI, Title: m.Title, Year: m.Year, Venue: m.Venue, Abstract: m.Abstract, URL: m.URL,
		Source: domain.SourceDOI, CreatedBy: &userID,
	}
	if p.Year == nil {
		p.Year = w.Year
	}
	if p.Venue == nil {
		p.Venue = w.Venue
	}
	return p
}

// orcidWorks returns the ORCID iD on the user's profile and its normalized works: from
// the cache when it has them, otherwise from ORCID, which counts toward the user's
// ORCIDListsPerWindow whatever it answers. A successful answer is cached for
// ORCIDWorksCacheTTL. When the cache or the limiter cannot answer, the call goes on
// without it (fail open).
func (s *Service) orcidWorks(ctx context.Context, userID string) (string, []domain.ORCIDWork, error) {
	a, err := s.store.FindAccount(ctx, userID)
	if err != nil {
		return "", nil, err
	}
	if a.Profile == nil || a.Profile.OrcidID == nil {
		return "", nil, domain.ErrORCIDNotSet
	}
	orcidID := *a.Profile.OrcidID
	o := s.orcid
	if o.Source == nil {
		return "", nil, domain.ErrMetadataUnavailable
	}
	works, ok, err := o.Cache.GetORCIDWorks(ctx, orcidID)
	switch {
	case err != nil:
		s.logger().WarnContext(ctx, "ORCID cache unavailable, listing the works", slog.Any("error", err))
	case ok:
		return orcidID, works, nil
	}
	used, left, err := o.Limiter.Used(ctx, userID)
	switch {
	case err != nil:
		s.logger().WarnContext(ctx, "ORCID rate limit unavailable, allowing the call", slog.Any("error", err))
	case used >= ORCIDListsPerWindow:
		return "", nil, &domain.RateLimitedError{RetryAfter: left}
	}
	if err := o.Limiter.Record(ctx, userID, ORCIDListWindow); err != nil {
		s.logger().WarnContext(ctx, "ORCID rate limit not recorded", slog.Any("error", err))
	}
	raw, err := o.Source.ListWorks(ctx, orcidID)
	if err != nil {
		return "", nil, err
	}
	works = make([]domain.ORCIDWork, 0, len(raw))
	for _, w := range raw {
		if n, ok := s.normalizeORCIDWork(w); ok {
			works = append(works, n)
		}
	}
	if err := o.Cache.PutORCIDWorks(ctx, orcidID, works, ORCIDWorksCacheTTL); err != nil {
		s.logger().WarnContext(ctx, "ORCID works not cached", slog.Any("error", err))
	}
	return orcidID, works, nil
}

// normalizeORCIDWork makes an ORCID work fit the publication rules as normalizeWork does:
// the title and venue become one line and are cut at a word boundary; a year outside
// MinPublicationYear to next year is dropped; a DOI is normalized and dropped when not
// valid; a missing or invalid link becomes https://doi.org/<doi> when there is a DOI. A
// work without a put-code or a title gives false.
func (s *Service) normalizeORCIDWork(w domain.ORCIDWork) (domain.ORCIDWork, bool) {
	out := domain.ORCIDWork{PutCode: strings.TrimSpace(w.PutCode)}
	out.Title = cutWords(oneLine(w.Title), MaxPublicationTitleLength)
	if out.PutCode == "" || out.Title == "" {
		return domain.ORCIDWork{}, false
	}
	if w.Year != nil && *w.Year >= MinPublicationYear && *w.Year <= s.now().UTC().Year()+1 {
		year := *w.Year
		out.Year = &year
	}
	if w.Venue != nil {
		out.Venue = nonEmpty(cutWords(oneLine(*w.Venue), MaxVenueLength))
	}
	if w.Type != nil {
		out.Type = nonEmpty(cutWords(oneLine(*w.Type), maxWorkTypeLength))
	}
	if doi, err := NormalizeDOI(w.DOI); err == nil {
		out.DOI = doi
	}
	if u, err := publicationURL(w.URL); err == nil && u != nil {
		out.URL = u
	} else if out.DOI != nil {
		fallback := "https://doi.org/" + *out.DOI
		out.URL, _ = publicationURL(&fallback) // nil for a DOI too long for a link
	}
	return out, true
}

// worksDOIs returns the DOIs of the works that have one.
func worksDOIs(works []domain.ORCIDWork) []string {
	var dois []string
	for _, w := range works {
		if w.DOI != nil {
			dois = append(dois, *w.DOI)
		}
	}
	return dois
}
