package usecase

import (
	"context"
	"fmt"
	"math"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/social/domain"
)

// Block makes blockerID block blockedID (FR-16.9). ErrBlockSelf for oneself, ErrNotFound
// for a user who is unknown, suspended or not verified. In one transaction it removes the
// connection or pending requests between the two, in both directions, and tells the
// notifier, which takes back the rest. Blocking someone already blocked changes nothing.
func (s *Service) Block(ctx context.Context, blockerID, blockedID string) error {
	if blockerID == blockedID {
		return domain.ErrBlockSelf
	}
	visible, err := s.store.UserVisible(ctx, blockedID)
	if err != nil {
		return err
	}
	if !visible {
		return domain.ErrNotFound
	}
	return s.tx.InTx(ctx, func(ctx context.Context) error {
		if err := s.store.LockPair(ctx, blockerID, blockedID); err != nil {
			return err
		}
		added, err := s.store.AddBlock(ctx, blockerID, blockedID)
		if err != nil || !added {
			return err
		}
		if err := s.store.RemoveLiveLink(ctx, blockerID, blockedID); err != nil {
			return err
		}
		if err := s.notifier.Blocked(ctx, blockerID, blockedID); err != nil {
			return fmt.Errorf("apply block of %s: %w", blockedID, err)
		}
		return nil
	})
}

// Unblock ends blockerID's block of blockedID, if there is one (FR-16.9). Nothing the block
// removed comes back.
func (s *Service) Unblock(ctx context.Context, blockerID, blockedID string) error {
	if err := s.store.RemoveBlock(ctx, blockerID, blockedID); err != nil {
		return err
	}
	s.notifier.Unblocked(ctx, blockerID, blockedID)
	return nil
}

// ListBlocks returns one page of the people userID has blocked (FR-16.9).
func (s *Service) ListBlocks(ctx context.Context, userID string, page int) (domain.BlockedPage, error) {
	if page < 1 || page-1 > math.MaxInt32/PageSize {
		return domain.BlockedPage{}, domain.ErrInvalidPage
	}
	items, total, err := s.store.ListBlocks(ctx, userID, PageSize, (page-1)*PageSize)
	if err != nil {
		return domain.BlockedPage{}, err
	}
	return domain.BlockedPage{Items: items, Page: page, PageSize: PageSize, Total: total}, nil
}

// BlockBetween reads who has blocked whom between me and other.
func (s *Service) BlockBetween(ctx context.Context, me, other string) (domain.BlockState, error) {
	return s.store.BlockBetween(ctx, me, other)
}

// BlockedAmong returns which of ids are in a block with userID, either way.
func (s *Service) BlockedAmong(ctx context.Context, userID string, ids []string) ([]string, error) {
	return s.store.BlockedAmong(ctx, userID, ids)
}

// LockPair serializes a block and any write that a block must refuse, between a and b. It
// joins the transaction the context carries.
func (s *Service) LockPair(ctx context.Context, a, b string) error {
	return s.store.LockPair(ctx, a, b)
}

// refuseBlocked takes the pair's lock and refuses a new connection request when either user
// has blocked the other: the blocker is told, the blocked person hears the answer for an
// unknown user. It runs inside the request's transaction.
func (s *Service) refuseBlocked(ctx context.Context, requesterID, receiverID string) error {
	if err := s.store.LockPair(ctx, requesterID, receiverID); err != nil {
		return err
	}
	state, err := s.store.BlockBetween(ctx, requesterID, receiverID)
	switch {
	case err != nil:
		return err
	case state.ByMe:
		return domain.ErrYouBlocked
	case state.ByThem:
		return domain.ErrNotFound
	}
	return nil
}
