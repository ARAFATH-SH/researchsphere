package domain

import (
	"context"
	"errors"
	"time"
)

// Message search (FR-16.8, NFR-13; slice 14B, chunk 14B.4). A user searches only what they
// could read in the thread: the store reads through the same visibility rule as ListMessages.

// ErrInvalidQuery refuses search text that is not 2 to 100 characters after trimming.
var ErrInvalidQuery = errors.New("messaging: the search text must have 2 to 100 characters")

// MatchSource says what of a message matched a search.
type MatchSource string

// Sources. MatchText is the message's text, MatchAttachment the name of one of its attachments.
const (
	MatchText       MatchSource = "text"
	MatchAttachment MatchSource = "attachment"
)

// Match tiers: a word match ranks above a similar (trigram) match.
const (
	TierWord    = 0
	TierSimilar = 1
)

// SearchCursor is a position in the results: the last item's tier and message ID.
type SearchCursor struct {
	Tier int
	ID   string
}

// SearchQuery is a checked search. Text is trimmed; the filters are empty or nil when not set.
type SearchQuery struct {
	Text           string
	ConversationID string
	SenderID       string
	After          *time.Time // inclusive
	Before         *time.Time // exclusive
	Cursor         *SearchCursor
	Limit          int
}

// SearchHit is one matching message as the store finds it.
type SearchHit struct {
	MessageID      string
	ConversationID string
	Kind           Kind
	Name           string  // a group's name, empty for a direct conversation
	Other          *Person // the other member of a direct conversation, nil for a group
	SenderID       string
	SenderName     string
	CreatedAt      time.Time
	Tier           int
	MatchedOn      MatchSource
	Body           string // the message's current text, empty when it has none
	AttachmentName string // the matching attachment's name when MatchedOn is MatchAttachment
}

// Range is a stretch of a snippet to highlight, in Unicode code points.
type Range struct {
	Start  int
	Length int
}

// SearchResult is a hit with what the client shows.
type SearchResult struct {
	SearchHit
	Avatars    []Person // for a group, up to 3 other members; empty for a direct conversation
	Snippet    string
	Highlights []Range
}

// SearchPage is one page of results. NextCursor is nil on the last page. People names the
// senders, as a page of messages does.
type SearchPage struct {
	Items      []SearchResult
	NextCursor *string
	People     map[string]PagePerson
}

// SearchStore finds messages.
type SearchStore interface {
	// SearchMessages returns the messages viewerID may read (as ListMessages does), not
	// deleted and not system messages, whose text or attachment name matches q.Text, best tier
	// first, then newest first, after q.Cursor when it is not nil. At most q.Limit.
	SearchMessages(ctx context.Context, viewerID string, q SearchQuery) ([]SearchHit, error)
}
