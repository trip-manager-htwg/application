package like

import (
	"context"
	"fmt"
)

type Service interface {
	GetEntityLikeInfo(ctx context.Context, userID, tenantID, entityID string) (*EntityLikeResponse, error)
	LikeEntity(ctx context.Context, userID, tenantID, entityID string) error
	UnlikeEntity(ctx context.Context, userID, entityID string) error
}

type ServiceImpl struct {
	repo Repository
}

func NewServiceImpl(repository Repository) Service {
	return &ServiceImpl{
		repo: repository,
	}
}

func (s *ServiceImpl) LikeEntity(ctx context.Context, userID, tenantID, entityID string) error {
	if err := s.repo.LikeEntity(ctx, userID, tenantID, entityID); err != nil {
		return fmt.Errorf("failed to like entity: %w", err)
	}
	return nil
}

func (s *ServiceImpl) UnlikeEntity(ctx context.Context, userID, entityID string) error {
	if err := s.repo.UnlikeEntity(ctx, userID, entityID); err != nil {
		return fmt.Errorf("failed to unlike entity: %w", err)
	}
	return nil
}

func (s *ServiceImpl) GetEntityLikeInfo(ctx context.Context, userID, tenantID, entityID string) (*EntityLikeResponse, error) {
	count, err := s.repo.CountEntityLikes(ctx, tenantID, entityID)
	if err != nil {
		return nil, fmt.Errorf("failed to count likes: %w", err)
	}

	hasLiked := false
	if userID != "" {
		hasLiked, err = s.repo.HasLiked(ctx, userID, entityID)
		if err != nil {
			return nil, fmt.Errorf("failed to check like status: %w", err)
		}
	}

	return &EntityLikeResponse{
		LikeCount: count,
		HasLiked:  hasLiked,
	}, nil
}
