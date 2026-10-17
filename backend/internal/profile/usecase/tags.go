package usecase

import (
	"context"
	"strings"
	"unicode/utf8"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/profile/domain"
)

// Tag limits (FR-4.2, FR-4.3). Migration 000004 holds the name length as a constraint.
const (
	MaxTagNameLength   = 60
	MaxInterests       = 20 // research interests per user
	MaxSkills          = 30 // skills per user
	DefaultSuggestions = 10
	MaxSuggestions     = 20 // a larger limit is reduced to this
)

// maxTags returns how many tags of kind a user may have.
func maxTags(kind domain.TagKind) int {
	if kind == domain.KindSkill {
		return MaxSkills
	}
	return MaxInterests
}

// NormalizeTagName trims name and turns every run of whitespace into one space, then
// requires 1 to MaxTagNameLength characters.
func NormalizeTagName(name string) (string, error) {
	name = strings.Join(strings.Fields(name), " ")
	if name == "" || utf8.RuneCountInString(name) > MaxTagNameLength {
		return "", domain.ErrInvalidTagName
	}
	return name, nil
}

// AddTag puts the tag named name on the user's profile (FR-4.2). A tag whose name
// differs only in letter case is reused, keeping its first spelling. created is false
// when the user already had the tag; that is not an error, even at the limit. A new link
// beyond the limit gives domain.ErrTagLimitReached and leaves nothing behind. It returns
// domain.ErrNotFound when the account no longer exists.
func (s *Service) AddTag(ctx context.Context, userID string, kind domain.TagKind, name string) (tag domain.Tag, created bool, err error) {
	name, err = NormalizeTagName(name)
	if err != nil {
		return domain.Tag{}, false, err
	}
	err = s.store.RunInTx(ctx, func(tx domain.Tx) error {
		// Lock first, so the count below cannot change before the link is stored.
		if err := tx.LockUser(ctx, userID); err != nil {
			return err
		}
		t, err := tx.FindOrCreateTag(ctx, kind, name)
		if err != nil {
			return err
		}
		has, err := tx.HasTag(ctx, userID, kind, t.ID)
		if err != nil {
			return err
		}
		tag = t
		if has {
			return nil
		}
		n, err := tx.CountTags(ctx, userID, kind)
		if err != nil {
			return err
		}
		if n >= maxTags(kind) {
			return domain.ErrTagLimitReached
		}
		created = true
		return tx.LinkTag(ctx, userID, kind, t.ID)
	})
	if err != nil {
		return domain.Tag{}, false, err
	}
	return tag, created, nil
}

// RemoveTag takes the tag off the user's profile. The tag itself stays, for other users
// and for suggestions. It returns domain.ErrNotFound when the user does not have it.
func (s *Service) RemoveTag(ctx context.Context, userID string, kind domain.TagKind, tagID string) error {
	return s.store.RemoveTag(ctx, userID, kind, tagID)
}

// SuggestTags returns existing tags of kind that match what the user has typed (FR-4.3),
// best match first. q is normalized like a tag name; an empty q, or one longer than any
// tag name, gives an empty list. limit is capped at MaxSuggestions; below 1 it gives
// domain.ErrInvalidLimit.
func (s *Service) SuggestTags(ctx context.Context, kind domain.TagKind, q string, limit int) ([]domain.Tag, error) {
	if limit < 1 {
		return nil, domain.ErrInvalidLimit
	}
	limit = min(limit, MaxSuggestions)
	q = strings.Join(strings.Fields(q), " ")
	if q == "" || utf8.RuneCountInString(q) > MaxTagNameLength {
		return []domain.Tag{}, nil
	}
	return s.store.SuggestTags(ctx, kind, q, limit)
}
