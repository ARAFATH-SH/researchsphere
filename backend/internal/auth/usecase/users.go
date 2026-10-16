package usecase

import (
	"context"
	"math"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/auth/domain"
)

// Page sizes of the users list. The server caps every list at 50 (docs/openapi.yaml).
const (
	DefaultPageSize = 20
	MaxPageSize     = 50
)

// ListUsers returns one page of users, newest first, for an administrator (FR-15.3). The
// caller has already checked the role (httpx.RequirePlatform). page counts from 1; a
// pageSize above MaxPageSize is reduced to it. A page or page size below 1 returns
// ErrInvalidPage.
func (s *Service) ListUsers(ctx context.Context, page, pageSize int) (domain.UserPage, error) {
	if page < 1 || pageSize < 1 {
		return domain.UserPage{}, domain.ErrInvalidPage
	}
	pageSize = min(pageSize, MaxPageSize)
	if page-1 > math.MaxInt32/pageSize {
		return domain.UserPage{}, domain.ErrInvalidPage
	}
	users, err := s.store.ListUsers(ctx, pageSize, (page-1)*pageSize)
	if err != nil {
		return domain.UserPage{}, err
	}
	total, err := s.store.CountUsers(ctx)
	if err != nil {
		return domain.UserPage{}, err
	}
	if users == nil {
		users = []domain.User{}
	}
	return domain.UserPage{Users: users, Page: page, PageSize: pageSize, Total: total}, nil
}
