package comment

import (
	"context"
	"fmt"

	"github.com/trip-manager-htwg/application/backend/social/internal/shared"
)

type Service interface {
	CreateComment(ctx context.Context, firebaseUID, postgresID, userName, userEmail, userAvatarKey, tenantID, entityID, text string) (*CommentResponse, error)
	ListComments(ctx context.Context, tenantID, entityID string) (*CommentListResponse, error)
	DeleteComment(ctx context.Context, firebaseUID, tenantID, commentID string) error
	UpdateComment(ctx context.Context, firebaseUID, tenantID, commentID, text string) (*CommentResponse, error)
}

type ServiceImpl struct {
	repo Repository
}

func NewServiceImpl(repository Repository) Service {
	return &ServiceImpl{
		repo: repository,
	}
}

func (s *ServiceImpl) CreateComment(ctx context.Context, firebaseUID, postgresID, userName, userEmail, userAvatarKey, tenantID, entityID, text string) (*CommentResponse, error) {
	if text == "" {
		return nil, fmt.Errorf("%w: comment text cannot be empty", shared.ErrInvalidInput)
	}
	comment := &Comment{
		EntityID:    entityID,
		TenantID:    tenantID,
		FirebaseUID: firebaseUID,
		User: UserSummary{
			ID:        postgresID,
			Name:      userName,
			Email:     userEmail,
			AvatarUrl: userAvatarKey,
		},
		Text: text,
	}
	created, err := s.repo.CreateComment(ctx, comment)
	if err != nil {
		return nil, fmt.Errorf("failed to create comment: %w", err)
	}
	return toCommentResponse(created), nil
}

func toCommentResponse(c *Comment) *CommentResponse {
	return &CommentResponse{
		ID:        c.ID,
		EntityID:  c.EntityID,
		User:      c.User,
		Text:      c.Text,
		ImageKey:  c.ImageKey,
		CreatedAt: c.CreatedAt,
		UpdatedAt: c.UpdatedAt,
	}
}

func (s *ServiceImpl) ListComments(ctx context.Context, tenantID, entityID string) (*CommentListResponse, error) {
	comments, err := s.repo.ListComments(ctx, tenantID, entityID)
	if err != nil {
		return nil, fmt.Errorf("failed to list comments: %w", err)
	}
	var responses []*CommentResponse
	for _, c := range comments {
		responses = append(responses, toCommentResponse(c))
	}
	return &CommentListResponse{Data: responses}, nil
}

func (s *ServiceImpl) DeleteComment(ctx context.Context, firebaseUID, tenantID, commentID string) error {
	if err := s.repo.DeleteComment(ctx, commentID, firebaseUID, tenantID); err != nil {
		return fmt.Errorf("failed to delete comment: %w", err)
	}
	return nil
}

func (s *ServiceImpl) UpdateComment(ctx context.Context, firebaseUID, tenantID, commentID, text string) (*CommentResponse, error) {
	if text == "" {
		return nil, fmt.Errorf("%w: comment text cannot be empty", shared.ErrInvalidInput)
	}
	existing, err := s.repo.GetComment(ctx, commentID, tenantID)
	if err != nil {
		return nil, fmt.Errorf("failed to get comment: %w", err)
	}
	if existing.FirebaseUID != firebaseUID {
		return nil, shared.ErrForbidden
	}
	existing.Text = text
	updated, err := s.repo.UpdateComment(ctx, existing)
	if err != nil {
		return nil, fmt.Errorf("failed to update comment: %w", err)
	}
	return toCommentResponse(updated), nil
}
