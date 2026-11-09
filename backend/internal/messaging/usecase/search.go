package usecase

import (
	"context"
	"encoding/base64"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/messaging/domain"
)

// Search limits (FR-16.8).
const (
	MinQueryLength     = 2   // characters of the trimmed search text
	MaxQueryLength     = 100 // characters of the trimmed search text
	DefaultSearchLimit = 20
	MaxSearchLimit     = 50
)

// SearchInput is a search as the caller sends it. Empty strings and nil mean not set.
type SearchInput struct {
	Query          string
	ConversationID string
	From           string
	After          *time.Time // inclusive
	Before         *time.Time // exclusive
	Cursor         string
	Limit          *int
}

// Search returns one page of the messages userID can read whose text or attachment name
// matches in.Query (FR-16.8), by the thread's visibility rule (NFR-13). Checks run in this
// order: the text (ErrInvalidQuery), the cursor (ErrInvalidCursor), the limit
// (ErrInvalidLimit), the rate limit (*RateLimitedError; a valid search counts once), then the
// conversation, ErrNotFound when userID is not a member of it.
func (s *Service) Search(ctx context.Context, userID string, in SearchInput) (domain.SearchPage, error) {
	text := strings.TrimSpace(in.Query)
	if n := utf8.RuneCountInString(text); !utf8.ValidString(text) || strings.ContainsRune(text, 0) || n < MinQueryLength || n > MaxQueryLength {
		return domain.SearchPage{}, domain.ErrInvalidQuery
	}
	q := domain.SearchQuery{Text: text, ConversationID: in.ConversationID, SenderID: in.From, After: in.After, Before: in.Before}
	if in.Cursor != "" {
		c, err := decodeSearchCursor(in.Cursor)
		if err != nil {
			return domain.SearchPage{}, err
		}
		q.Cursor = &c
	}
	limit, err := pageLimit(in.Limit, DefaultSearchLimit, MaxSearchLimit)
	if err != nil {
		return domain.SearchPage{}, err
	}
	q.Limit = limit + 1
	if err := s.checkLimits(ctx, userID, s.searches, 1); err != nil {
		return domain.SearchPage{}, err
	}
	s.record(ctx, userID, s.searches, 1)
	if q.ConversationID != "" {
		if _, err := s.store.FindConversation(ctx, q.ConversationID, userID); err != nil {
			return domain.SearchPage{}, err
		}
	}
	hits, err := s.store.SearchMessages(ctx, userID, q)
	if err != nil {
		return domain.SearchPage{}, err
	}
	var next *string
	if len(hits) > limit {
		hits = hits[:limit]
		last := hits[limit-1]
		c := encodeSearchCursor(domain.SearchCursor{Tier: last.Tier, ID: last.MessageID})
		next = &c
	}
	var groups []string
	for _, h := range hits {
		if h.Kind == domain.KindGroup {
			groups = append(groups, h.ConversationID)
		}
	}
	avatars, err := s.store.GroupAvatars(ctx, userID, groups)
	if err != nil {
		return domain.SearchPage{}, err
	}
	terms := queryTerms(text)
	items := make([]domain.SearchResult, len(hits))
	for i, h := range hits {
		source := h.Body
		if h.MatchedOn == domain.MatchAttachment {
			source = h.AttachmentName
		}
		snippet, ranges := Snippet(source, terms)
		items[i] = domain.SearchResult{SearchHit: h, Avatars: avatars[h.ConversationID], Snippet: snippet, Highlights: ranges}
		if items[i].Avatars == nil {
			items[i].Avatars = []domain.Person{}
		}
	}
	return domain.SearchPage{Items: items, NextCursor: next}, nil
}

// encodeSearchCursor turns a position into the opaque value the client sends back.
func encodeSearchCursor(c domain.SearchCursor) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.Itoa(c.Tier) + "|" + c.ID))
}

// decodeSearchCursor reads a value made by encodeSearchCursor; anything else is ErrInvalidCursor.
func decodeSearchCursor(s string) (domain.SearchCursor, error) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return domain.SearchCursor{}, domain.ErrInvalidCursor
	}
	tier, id, ok := strings.Cut(string(raw), "|")
	if !ok || !isUUID(id) || (tier != "0" && tier != "1") {
		return domain.SearchCursor{}, domain.ErrInvalidCursor
	}
	t, _ := strconv.Atoi(tier)
	return domain.SearchCursor{Tier: t, ID: id}, nil
}
