package http

import (
	"context"
	"errors"
	"math"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/api/gen"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/messaging/domain"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/messaging/usecase"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/httpx"
)

// SearchService is what the search operation needs from the usecase.
type SearchService interface {
	Search(ctx context.Context, userID string, in usecase.SearchInput) (domain.SearchPage, error)
}

func fieldError(message, field string) gen.SearchMessages400JSONResponse {
	return gen.SearchMessages400JSONResponse{Code: httpx.CodeBadRequest, Message: message, Field: &field}
}

// SearchMessages searches the caller's own conversations (FR-16.8, NFR-13).
func (h *Handler) SearchMessages(ctx context.Context, req gen.SearchMessagesRequestObject) (gen.SearchMessagesResponseObject, error) {
	id, ok := httpx.UserID(ctx)
	if !ok {
		return gen.SearchMessages401JSONResponse{UnauthorizedJSONResponse: unauthenticated}, nil
	}
	p := req.Params
	in := usecase.SearchInput{Query: text(p.Q), After: p.After, Before: p.Before, Cursor: text(p.Cursor), Limit: p.Limit}
	if p.ConversationId != nil {
		in.ConversationID = p.ConversationId.String()
	}
	if p.From != nil {
		in.From = p.From.String()
	}
	page, err := h.svc.Search(ctx, id, in)
	var limited *domain.RateLimitedError
	switch {
	case errors.Is(err, domain.ErrInvalidQuery):
		return fieldError("The search text must have 2 to 100 characters.", "q"), nil
	case errors.Is(err, domain.ErrInvalidCursor):
		return fieldError("The cursor is not valid.", "cursor"), nil
	case errors.Is(err, domain.ErrInvalidLimit):
		return fieldError("Limit must be a whole number of at least 1.", "limit"), nil
	case errors.Is(err, domain.ErrNotFound):
		return gen.SearchMessages404JSONResponse{NotFoundJSONResponse: notFound}, nil
	case errors.As(err, &limited):
		return gen.SearchMessages429JSONResponse{MessageSearchRateLimitedJSONResponse: gen.MessageSearchRateLimitedJSONResponse{
			Body:    gen.Error{Code: CodeRateLimited, Message: "You are searching too quickly. Try again in a minute."},
			Headers: gen.MessageSearchRateLimitedResponseHeaders{RetryAfter: max(1, int(math.Ceil(limited.RetryAfter.Seconds())))},
		}}, nil
	case err != nil:
		return nil, err
	}
	items := make([]gen.MessageSearchResult, len(page.Items))
	for i, r := range page.Items {
		if items[i], err = searchResultBody(r); err != nil {
			return nil, err
		}
	}
	return gen.SearchMessages200JSONResponse{Items: items, NextCursor: page.NextCursor, People: peopleBody(page.People)}, nil
}

func searchResultBody(r domain.SearchResult) (gen.MessageSearchResult, error) {
	msg, err := uuid(r.MessageID)
	if err != nil {
		return gen.MessageSearchResult{}, err
	}
	conv, err := uuid(r.ConversationID)
	if err != nil {
		return gen.MessageSearchResult{}, err
	}
	sender, err := uuid(r.SenderID)
	if err != nil {
		return gen.MessageSearchResult{}, err
	}
	c := gen.MessageSearchConversation{Kind: gen.ConversationKind(r.Kind), Avatars: []gen.GroupAvatar{}}
	if r.Kind == domain.KindGroup {
		name := r.Name
		c.Name = &name
		for _, p := range r.Avatars {
			a, err := avatarBody(p)
			if err != nil {
				return gen.MessageSearchResult{}, err
			}
			c.Avatars = append(c.Avatars, a)
		}
	} else if r.Other != nil {
		a, err := avatarBody(*r.Other)
		if err != nil {
			return gen.MessageSearchResult{}, err
		}
		c.OtherMember = &a
	}
	ranges := make([]gen.TextRange, len(r.Highlights))
	for i, h := range r.Highlights {
		ranges[i] = gen.TextRange{Start: h.Start, Length: h.Length}
	}
	return gen.MessageSearchResult{
		MessageId: msg, ConversationId: conv, Conversation: c,
		Sender:    gen.MessageSearchSender{UserId: sender, FullName: r.SenderName},
		CreatedAt: r.CreatedAt, Snippet: r.Snippet, Highlights: ranges, MatchedOn: gen.MessageSearchMatch(r.MatchedOn),
	}, nil
}

func avatarBody(p domain.Person) (gen.GroupAvatar, error) {
	user, err := uuid(p.UserID)
	if err != nil {
		return gen.GroupAvatar{}, err
	}
	return gen.GroupAvatar{UserId: user, FullName: p.FullName, PhotoUrl: mediaURL(p.PhotoFileID)}, nil
}
