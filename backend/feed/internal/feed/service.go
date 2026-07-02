package feed

import (
	"context"

	generated "github.com/trip-manager-htwg/application/backend/feed/generated"
)

type service struct {
	repo Repository
}

func NewService(repo Repository) Service {
	return &service{repo: repo}
}

type Service interface {
	GetGlobalFeed(ctx context.Context, tenantID string, limit, offset int) ([]generated.FeedTrip, int, error)
	GetPersonalizedFeed(ctx context.Context, tenantID, userID string, limit, offset int) ([]generated.FeedTrip, int, error)
}

func (s *service) GetGlobalFeed(ctx context.Context, tenantID string, limit, offset int) ([]generated.FeedTrip, int, error) {
	limit, offset = clamp(limit, offset)
	return s.repo.GetFeed(ctx, tenantID, limit, offset)
}

func (s *service) GetPersonalizedFeed(ctx context.Context, tenantID, userID string, limit, offset int) ([]generated.FeedTrip, int, error) {
	limit, offset = clamp(limit, offset)
	return s.repo.GetPersonalizedFeed(ctx, tenantID, userID, limit, offset)
}

func clamp(limit, offset int) (int, int) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}
