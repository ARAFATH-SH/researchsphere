package postgres

import (
	"context"
	"fmt"
	"strconv"

	"github.com/jackc/pgx/v5"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/messaging/adapter/postgres/sqlcgen"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/messaging/domain"
)

// WordSimilarityThreshold is the least trigram word similarity of a similar match (FR-16.8).
// Measured on Bangla and English text (docs/decisions.md, 2026-11-10, message search): one-letter
// typos and partial words score 0.556 and up, while different words that share most of their
// letters ("draft" and "craft", "পত্রিকা" and "পত্র") score 0.5.
const WordSimilarityThreshold = 0.55

// SearchMessages runs in a read-only transaction of its own, so the threshold and the
// custom plan (PrepareSearch) hold for its query only. A viewer, conversation or sender
// that is not a UUID finds nothing.
func (s *Store) SearchMessages(ctx context.Context, viewerID string, q domain.SearchQuery) ([]domain.SearchHit, error) {
	viewer, ok := parseUUID(viewerID)
	if !ok {
		return nil, nil
	}
	arg := sqlcgen.SearchMessagesParams{ViewerID: viewer, Q: q.Text, Lim: int32(min(q.Limit, 1000))} //nolint:gosec // bounded above
	if q.ConversationID != "" {
		if arg.ConversationID, ok = parseUUID(q.ConversationID); !ok {
			return nil, nil
		}
		arg.HasConversation = true
	}
	if q.SenderID != "" {
		if arg.SenderID, ok = parseUUID(q.SenderID); !ok {
			return nil, nil
		}
		arg.HasSender = true
	}
	if q.After != nil {
		arg.HasAfter, arg.AfterAt = true, timestamptz(*q.After)
	}
	if q.Before != nil {
		arg.HasBefore, arg.BeforeAt = true, timestamptz(*q.Before)
	}
	if q.Cursor != nil {
		if arg.CursorID, ok = parseUUID(q.Cursor.ID); !ok {
			return nil, domain.ErrInvalidCursor
		}
		arg.HasCursor, arg.CursorTier = true, int32(q.Cursor.Tier) //nolint:gosec // 0 or 1
	}
	var rows []sqlcgen.SearchMessagesRow
	err := pgx.BeginTxFunc(ctx, s.pool, pgx.TxOptions{AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		qs := sqlcgen.New(tx)
		if err := qs.PrepareSearch(ctx, strconv.FormatFloat(WordSimilarityThreshold, 'f', -1, 64)); err != nil {
			return fmt.Errorf("prepare search: %w", err)
		}
		var err error
		rows, err = qs.SearchMessages(ctx, arg)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("search messages: %w", err)
	}
	out := make([]domain.SearchHit, len(rows))
	for i, r := range rows {
		h := domain.SearchHit{
			MessageID: r.ID.String(), ConversationID: r.ConversationID.String(), Kind: domain.Kind(r.Kind),
			SenderID: r.SenderID.String(), SenderName: r.SenderName, CreatedAt: r.CreatedAt.Time,
			Tier: int(r.Tier), MatchedOn: domain.MatchText, Body: r.Body.String,
		}
		if r.OnAttachment {
			h.MatchedOn, h.AttachmentName = domain.MatchAttachment, r.AttachmentName
		}
		if h.Kind == domain.KindGroup {
			h.Name = r.Name
		} else if r.OtherID.Valid {
			h.Other = &domain.Person{
				UserID: r.OtherID.String(), FullName: r.OtherName, Headline: textPtr(r.OtherHeadline),
				PhotoFileID: uuidPtr(r.OtherPhotoFileID), Available: r.OtherAvailable,
			}
		}
		out[i] = h
	}
	return out, nil
}
