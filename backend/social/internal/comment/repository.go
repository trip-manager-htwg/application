package comment

import (
	"context"
	"errors"
	"fmt"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/google/uuid"
	"github.com/trip-manager-htwg/application/backend/social/internal/shared"
	"google.golang.org/api/iterator"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const collComments = "comments"

type Repository interface {
	CreateComment(ctx context.Context, comment *Comment) (*Comment, error)
	GetComment(ctx context.Context, commentID, tenantID string) (*Comment, error)
	ListComments(ctx context.Context, tenantID, entityID string) ([]*Comment, error)
	DeleteComment(ctx context.Context, commentID, userID, tenantID string) error
	UpdateComment(ctx context.Context, comment *Comment) (*Comment, error)
}

type RepositoryImpl struct {
	client *firestore.Client
}

func NewCommentRepository(client *firestore.Client) Repository {
	return &RepositoryImpl{client: client}
}

func (r *RepositoryImpl) CreateComment(ctx context.Context, comment *Comment) (*Comment, error) {
	if comment.ID == "" {
		comment.ID = uuid.New().String()
	}
	if comment.CreatedAt.IsZero() {
		comment.CreatedAt = time.Now()
	}
	if comment.UpdatedAt.IsZero() {
		comment.UpdatedAt = time.Now()
	}
	_, err := r.client.Collection(collComments).Doc(comment.ID).Create(ctx, comment)
	if err != nil {
		if status.Code(err) == codes.AlreadyExists {
			return nil, shared.ErrConflict
		}
		return nil, fmt.Errorf("%w: %v", shared.ErrInternal, err)
	}
	return comment, nil
}

func (r *RepositoryImpl) GetComment(ctx context.Context, commentID, tenantID string) (*Comment, error) {
	doc, err := r.client.Collection(collComments).Doc(commentID).Get(ctx)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return nil, shared.ErrNotFound
		}
		return nil, fmt.Errorf("%w: %v", shared.ErrInternal, err)
	}
	var comment Comment
	if err := doc.DataTo(&comment); err != nil {
		return nil, fmt.Errorf("%w: failed to parse comment: %v", shared.ErrInternal, err)
	}
	if comment.TenantID != tenantID {
		return nil, shared.ErrNotFound
	}
	return &comment, nil
}

func (r *RepositoryImpl) ListComments(ctx context.Context, tenantID, entityID string) ([]*Comment, error) {
	iter := r.client.Collection(collComments).
		Where("entityId", "==", entityID).
		Where("tenantId", "==", tenantID).
		OrderBy("createdAt", firestore.Desc).
		Documents(ctx)
	defer iter.Stop()

	var comments []*Comment
	for {
		doc, err := iter.Next()
		if errors.Is(err, iterator.Done) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("%w: %v", shared.ErrInternal, err)
		}
		var comment Comment
		if err := doc.DataTo(&comment); err != nil {
			return nil, fmt.Errorf("%w: failed to parse comment: %v", shared.ErrInternal, err)
		}
		comments = append(comments, &comment)
	}
	return comments, nil
}

func (r *RepositoryImpl) DeleteComment(ctx context.Context, commentID, userID, tenantID string) error {
	comment, err := r.GetComment(ctx, commentID, tenantID)
	if err != nil {
		return err
	}

	if comment.FirebaseUID != userID {
		return shared.ErrForbidden
	}
	_, err = r.client.Collection(collComments).Doc(commentID).Delete(ctx)
	if err != nil {
		return fmt.Errorf("%w: %v", shared.ErrInternal, err)
	}
	return nil
}

func (r *RepositoryImpl) UpdateComment(ctx context.Context, comment *Comment) (*Comment, error) {
	comment.UpdatedAt = time.Now()
	_, err := r.client.Collection(collComments).Doc(comment.ID).Set(ctx, comment)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", shared.ErrInternal, err)
	}
	return comment, nil
}
