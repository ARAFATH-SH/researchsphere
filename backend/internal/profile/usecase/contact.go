package usecase

import (
	"context"
	"net/url"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/profile/domain"
)

// Contact limits (FR-4.7). Migration 000006 holds the same rules as database constraints.
const (
	MaxLinks         = 5 // links per user
	MaxLinkURLLength = 300
)

// e164 is the shape of a phone number in E.164 format: +, then 2 to 15 digits, the
// first not 0. It does not check that the number exists.
var e164 = regexp.MustCompile(`^\+[1-9][0-9]{1,14}$`)

// ContactInput is what a user sends to edit their contact info. A nil Phone or Birthday
// clears it.
type ContactInput struct {
	EmailVisibility    domain.Visibility
	Phone              *string
	PhoneVisibility    domain.Visibility
	Birthday           *domain.Birthday
	BirthdayVisibility domain.Visibility
}

// GetContact returns the caller's contact info with its links (FR-4.7). It returns
// domain.ErrNotFound when the account no longer exists.
func (s *Service) GetContact(ctx context.Context, userID string) (domain.Contact, error) {
	c, err := s.store.FindContact(ctx, userID)
	if err != nil {
		return domain.Contact{}, err
	}
	if c.Links, err = s.store.ListLinks(ctx, userID); err != nil {
		return domain.Contact{}, err
	}
	return c, nil
}

// UpdateContact validates in and replaces the caller's phone, birthday and visibilities
// (FR-4.7). It returns the first invalid field's error, in the contract's order (phone,
// birthday, visibility), and stores nothing then. It returns domain.ErrNotFound when the
// account no longer exists.
func (s *Service) UpdateContact(ctx context.Context, userID string, in ContactInput) (domain.Contact, error) {
	phone, err := normalizePhone(in.Phone)
	if err != nil {
		return domain.Contact{}, err
	}
	if in.Birthday != nil && !validBirthday(*in.Birthday) {
		return domain.Contact{}, domain.ErrInvalidBirthday
	}
	for _, v := range []domain.Visibility{in.EmailVisibility, in.PhoneVisibility, in.BirthdayVisibility} {
		if !validVisibility(v) {
			return domain.Contact{}, domain.ErrInvalidVisibility
		}
	}
	err = s.store.UpsertContact(ctx, domain.Contact{
		UserID:             userID,
		EmailVisibility:    in.EmailVisibility,
		Phone:              phone,
		PhoneVisibility:    in.PhoneVisibility,
		Birthday:           in.Birthday,
		BirthdayVisibility: in.BirthdayVisibility,
	})
	if err != nil {
		return domain.Contact{}, err
	}
	return s.GetContact(ctx, userID)
}

// AddLink validates the link and adds it to the caller's profile (FR-4.7). It returns
// domain.ErrInvalidLinkKind or domain.ErrInvalidLinkURL, and domain.ErrLinkLimitReached
// when the user already has MaxLinks; nothing is stored then. It returns
// domain.ErrNotFound when the account no longer exists.
func (s *Service) AddLink(ctx context.Context, userID string, kind domain.LinkKind, rawURL string) (domain.Link, error) {
	if !validLinkKind(kind) {
		return domain.Link{}, domain.ErrInvalidLinkKind
	}
	u, err := normalizeLinkURL(rawURL)
	if err != nil {
		return domain.Link{}, err
	}
	var saved domain.Link
	err = s.store.RunInTx(ctx, func(tx domain.Tx) error {
		// Lock first, so the count below cannot change before the link is stored.
		if err := tx.LockUser(ctx, userID); err != nil {
			return err
		}
		n, err := tx.CountLinks(ctx, userID)
		if err != nil {
			return err
		}
		if n >= MaxLinks {
			return domain.ErrLinkLimitReached
		}
		saved, err = tx.InsertLink(ctx, domain.Link{UserID: userID, Kind: kind, URL: u})
		return err
	})
	if err != nil {
		return domain.Link{}, err
	}
	return saved, nil
}

// RemoveLink deletes the caller's link id. It returns domain.ErrNotFound when the user
// has no such link, including one that belongs to someone else.
func (s *Service) RemoveLink(ctx context.Context, userID, id string) error {
	return s.store.DeleteLink(ctx, userID, id)
}

// VisibleTo returns the part of c that viewerID may see (FR-4.7): everything that is
// set for the owner; for anyone else only the items set to domain.LoggedIn, and the
// links. The caller is a logged-in user; route guards make sure of that.
func VisibleTo(viewerID string, c domain.Contact) domain.VisibleContact {
	own := viewerID == c.UserID
	shown := func(v domain.Visibility) bool { return own || v == domain.LoggedIn }
	out := domain.VisibleContact{Links: c.Links}
	if out.Links == nil {
		out.Links = []domain.Link{}
	}
	if c.Email != "" && shown(c.EmailVisibility) {
		email := c.Email
		out.Email = &email
	}
	if c.Phone != nil && shown(c.PhoneVisibility) {
		phone := *c.Phone
		out.Phone = &phone
	}
	if c.Birthday != nil && shown(c.BirthdayVisibility) {
		b := *c.Birthday
		out.Birthday = &b
	}
	return out
}

// normalizePhone removes spaces and hyphens, then requires E.164. Empty means not set.
func normalizePhone(p *string) (*string, error) {
	if p == nil {
		return nil, nil
	}
	v := strings.Map(func(r rune) rune {
		if r == '-' || unicode.IsSpace(r) {
			return -1
		}
		return r
	}, *p)
	if v == "" {
		return nil, nil
	}
	if !e164.MatchString(v) {
		return nil, domain.ErrInvalidPhone
	}
	return &v, nil
}

// daysIn is the number of days each month can have; February has 29, since there is no
// year to rule 29 February out.
var daysIn = [13]int{0, 31, 29, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31}

func validBirthday(b domain.Birthday) bool {
	return b.Month >= 1 && b.Month <= 12 && b.Day >= 1 && b.Day <= daysIn[b.Month]
}

func validVisibility(v domain.Visibility) bool {
	return v == domain.OnlyMe || v == domain.LoggedIn
}

func validLinkKind(k domain.LinkKind) bool {
	switch k {
	case domain.LinkWebsite, domain.LinkGoogleScholar, domain.LinkResearchGate,
		domain.LinkLinkedIn, domain.LinkGitHub, domain.LinkOther:
		return true
	}
	return false
}

// normalizeLinkURL trims the URL and lower-cases its scheme, then requires https, a
// host, no whitespace and at most MaxLinkURLLength characters.
func normalizeLinkURL(raw string) (string, error) {
	v := strings.TrimSpace(raw)
	if len(v) >= len("https://") && strings.EqualFold(v[:len("https://")], "https://") {
		v = "https://" + v[len("https://"):]
	}
	if !strings.HasPrefix(v, "https://") || utf8.RuneCountInString(v) > MaxLinkURLLength ||
		strings.IndexFunc(v, unicode.IsSpace) >= 0 {
		return "", domain.ErrInvalidLinkURL
	}
	u, err := url.Parse(v)
	if err != nil || u.Host == "" {
		return "", domain.ErrInvalidLinkURL
	}
	return v, nil
}
