package usecase

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/profile/domain"
)

// DOI lookup limits (FR-5.2, docs/decisions.md).
const (
	LookupsPerWindow = 30        // lookups that reach the outside service, per user
	LookupWindow     = time.Hour // fixed window, opened by the first lookup
	WorkCacheTTL     = 24 * time.Hour
)

// DOILookup is what DOI lookups need: the metadata service, the cache of its successful
// answers and the per-user limit. Every field is required.
type DOILookup struct {
	Source  domain.WorkSource
	Cache   domain.WorkCache
	Limiter domain.LookupLimiter
	Logger  *slog.Logger
}

// WithDOILookup lets the Service look up publications by DOI (FR-5.2).
func WithDOILookup(l DOILookup) Option {
	return func(s *Service) { s.lookup = l }
}

// DOIPreview is the metadata of a DOI shown for confirmation before it is saved
// (FR-5.2). When ExistsAlready is true the fields are the stored record's, PublicationID
// is its ID and LinkedToMe says whether the user has confirmed it.
type DOIPreview struct {
	DOI           string
	Title         string
	Year          *int
	Venue         *string
	Abstract      *string
	URL           *string
	ExistsAlready bool
	PublicationID string
	LinkedToMe    bool
}

// LookupDOI returns the metadata of the DOI raw, in any form NormalizeDOI accepts. A
// stored publication with the DOI is returned as stored, with no outside call. Otherwise
// the metadata comes from the cache or the outside service; see metadata. It returns
// domain.ErrInvalidDOI for a missing or invalid DOI, domain.ErrWorkNotFound,
// domain.ErrMetadataUnavailable and *domain.RateLimitedError.
func (s *Service) LookupDOI(ctx context.Context, userID, raw string) (DOIPreview, error) {
	doi, err := requiredDOI(raw)
	if err != nil {
		return DOIPreview{}, err
	}
	var (
		found bool
		pv    DOIPreview
	)
	err = s.store.RunInTx(ctx, func(tx domain.Tx) error {
		p, err := tx.FindPublicationByDOI(ctx, doi)
		if errors.Is(err, domain.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		state, err := tx.PublicationLink(ctx, userID, p.ID)
		if err != nil {
			return err
		}
		found = true
		pv = DOIPreview{
			DOI: doi, Title: p.Title, Year: p.Year, Venue: p.Venue, Abstract: p.Abstract, URL: p.URL,
			ExistsAlready: true, PublicationID: p.ID, LinkedToMe: state == domain.Confirmed,
		}
		return nil
	})
	if err != nil || found {
		return pv, err
	}
	w, err := s.metadata(ctx, userID, doi)
	if err != nil {
		return DOIPreview{}, err
	}
	return DOIPreview{DOI: doi, Title: w.Title, Year: w.Year, Venue: w.Venue, Abstract: w.Abstract, URL: w.URL}, nil
}

// AddPublicationFromDOI adds the publication with the DOI raw to the user's publications
// after the user confirmed its preview (FR-5.2), with the user's link confirmed. When a
// publication with the DOI is stored, the user is linked to it with no outside call
// (FR-5.4). Otherwise the record is made from the metadata, cached or fetched again, with
// source doi. It returns the errors of LookupDOI and of AddPublication; the limit is
// checked before any outside call, so a refused request spends no lookup.
func (s *Service) AddPublicationFromDOI(ctx context.Context, userID, raw string) (AddedPublication, error) {
	doi, err := requiredDOI(raw)
	if err != nil {
		return AddedPublication{}, err
	}
	var (
		linked bool
		res    AddedPublication
	)
	err = s.store.RunInTx(ctx, func(tx domain.Tx) error {
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
		existing, err := tx.FindPublicationByDOI(ctx, doi)
		if errors.Is(err, domain.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		linked = true
		res, err = linkExisting(ctx, tx, userID, existing)
		return err
	})
	if err != nil || linked {
		return res, err
	}
	w, err := s.metadata(ctx, userID, doi)
	if err != nil {
		return AddedPublication{}, err
	}
	// The limit and the DOI are checked again under the lock: another request may have
	// added a publication, or stored this DOI, during the outside call.
	return s.storePublication(ctx, userID, domain.Publication{
		DOI: &doi, Title: w.Title, Year: w.Year, Venue: w.Venue, Abstract: w.Abstract, URL: w.URL,
		Source: domain.SourceDOI, CreatedBy: &userID,
	})
}

// requiredDOI normalizes raw; a missing DOI is as invalid as a malformed one.
func requiredDOI(raw string) (string, error) {
	doi, err := NormalizeDOI(&raw)
	if err != nil {
		return "", err
	}
	if doi == nil {
		return "", domain.ErrInvalidDOI
	}
	return *doi, nil
}

// metadata returns the normalized metadata of doi: from the cache when it has it,
// otherwise from the outside service, which counts toward the user's LookupsPerWindow
// whatever it answers. A successful answer is cached for WorkCacheTTL. When the cache or
// the limiter cannot answer, the lookup goes on without it (fail open).
func (s *Service) metadata(ctx context.Context, userID, doi string) (domain.WorkMetadata, error) {
	l := s.lookup
	if l.Source == nil {
		return domain.WorkMetadata{}, domain.ErrMetadataUnavailable
	}
	w, ok, err := l.Cache.GetWork(ctx, doi)
	switch {
	case err != nil:
		s.logger().WarnContext(ctx, "DOI cache unavailable, fetching the work", slog.Any("error", err))
	case ok:
		return w, nil
	}
	used, left, err := l.Limiter.Used(ctx, userID)
	switch {
	case err != nil:
		s.logger().WarnContext(ctx, "DOI lookup rate limit unavailable, allowing the lookup", slog.Any("error", err))
	case used >= LookupsPerWindow:
		return domain.WorkMetadata{}, &domain.RateLimitedError{RetryAfter: left}
	}
	if err := l.Limiter.Record(ctx, userID, LookupWindow); err != nil {
		s.logger().WarnContext(ctx, "DOI lookup rate limit not recorded", slog.Any("error", err))
	}
	w, err = l.Source.FetchWork(ctx, doi)
	if err != nil {
		return domain.WorkMetadata{}, err
	}
	if w, err = s.normalizeWork(doi, w); err != nil {
		return domain.WorkMetadata{}, err
	}
	if err := l.Cache.PutWork(ctx, w, WorkCacheTTL); err != nil {
		s.logger().WarnContext(ctx, "DOI lookup not cached", slog.Any("error", err))
	}
	return w, nil
}

func (s *Service) logger() *slog.Logger {
	switch {
	case s.lookup.Logger != nil:
		return s.lookup.Logger
	case s.orcid.Logger != nil:
		return s.orcid.Logger
	default:
		return slog.New(slog.DiscardHandler)
	}
}

// normalizeWork makes fetched metadata fit the publication rules of FR-5.1 instead of
// refusing it: text is trimmed; the title and venue become one line with single spaces;
// a title, venue or abstract over its limit is cut at a word boundary; a year outside
// MinPublicationYear to next year is dropped; a missing or invalid link becomes
// https://doi.org/<doi>. A work without a title gives domain.ErrWorkNotFound.
func (s *Service) normalizeWork(doi string, w domain.WorkMetadata) (domain.WorkMetadata, error) {
	out := domain.WorkMetadata{DOI: doi}
	out.Title = cutWords(oneLine(w.Title), MaxPublicationTitleLength)
	if out.Title == "" {
		return domain.WorkMetadata{}, domain.ErrWorkNotFound
	}
	if w.Year != nil && *w.Year >= MinPublicationYear && *w.Year <= s.now().UTC().Year()+1 {
		year := *w.Year
		out.Year = &year
	}
	if w.Venue != nil {
		out.Venue = nonEmpty(cutWords(oneLine(*w.Venue), MaxVenueLength))
	}
	if w.Abstract != nil {
		out.Abstract = nonEmpty(cutWords(strings.TrimSpace(*w.Abstract), MaxAbstractLength))
	}
	if u, err := publicationURL(w.URL); err == nil && u != nil {
		out.URL = u
	} else {
		fallback := "https://doi.org/" + doi
		out.URL, _ = publicationURL(&fallback) // nil for a DOI too long for a link
	}
	return out, nil
}

// oneLine joins the words of s, of any whitespace including line breaks, with single
// spaces.
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func nonEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// cutWords returns s when it has at most limit characters. Otherwise it cuts s after the
// last whole word that fits in limit characters and trims the space at the end; a first
// word longer than limit is cut at limit.
func cutWords(s string, limit int) string {
	if utf8.RuneCountInString(s) <= limit {
		return s
	}
	end, n := 0, 0 // byte offset of the first rune past the limit
	for i := range s {
		if n == limit {
			end = i
			break
		}
		n++
	}
	if r, _ := utf8.DecodeRuneInString(s[end:]); unicode.IsSpace(r) {
		return strings.TrimRightFunc(s[:end], unicode.IsSpace)
	}
	head := s[:end]
	if i := strings.LastIndexFunc(head, unicode.IsSpace); i > 0 {
		if cut := strings.TrimRightFunc(head[:i], unicode.IsSpace); cut != "" {
			return cut
		}
	}
	return head
}
