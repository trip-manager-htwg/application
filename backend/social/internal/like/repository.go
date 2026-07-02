package like

import (
	"context"
	"errors"
	"fmt"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/trip-manager-htwg/application//backend/social/internal/shared"
	"google.golang.org/api/iterator"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const collEntityLikes = "entityLikes"

type entityLikeDoc struct {
	EntityID  string    `firestore:"entityId"`
	TenantID  string    `firestore:"tenantId"`
	UserID    string    `firestore:"userId"`
	CreatedAt time.Time `firestore:"createdAt"`
}

type Repository interface {
	LikeEntity(ctx context.Context, userID, tenantID, entityId string) error
	UnlikeEntity(ctx context.Context, userID, entityId string) error
	HasLiked(ctx context.Context, userID, entityId string) (bool, error)
	CountEntityLikes(ctx context.Context, tenantID, entityId string) (int, error)
}

type RepositoryImpl struct {
	client *firestore.Client
}

func NewLikeRepository(client *firestore.Client) Repository {
	return &RepositoryImpl{client: client}
}

func (r *RepositoryImpl) LikeEntity(ctx context.Context, userID, tenantID, entityID string) error {
	docID := userID + "_" + entityID
	_, err := r.client.Collection(collEntityLikes).Doc(docID).Create(ctx, entityLikeDoc{
		EntityID:  entityID,
		TenantID:  tenantID,
		UserID:    userID,
		CreatedAt: time.Now(),
	})
	if err != nil {
		if status.Code(err) == codes.AlreadyExists {
			return shared.ErrConflict
		}
		return fmt.Errorf("%w: %v", shared.ErrInternal, err)
	}
	return nil
}

func (r *RepositoryImpl) UnlikeEntity(ctx context.Context, userID, entityID string) error {
	docID := userID + "_" + entityID
	_, err := r.client.Collection(collEntityLikes).Doc(docID).Delete(ctx)
	if err != nil {
		return fmt.Errorf("%w: %v", shared.ErrInternal, err)
	}
	return nil
}

func (r *RepositoryImpl) HasLiked(ctx context.Context, userID, entityID string) (bool, error) {
	docID := userID + "_" + entityID
	_, err := r.client.Collection(collEntityLikes).Doc(docID).Get(ctx)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return false, nil
		}
		return false, fmt.Errorf("%w: %v", shared.ErrInternal, err)
	}
	return true, nil
}

func (r *RepositoryImpl) CountEntityLikes(ctx context.Context, tenantID, entityID string) (int, error) {
	iter := r.client.Collection(collEntityLikes).
		Where("entityId", "==", entityID).
		Where("tenantId", "==", tenantID).
		Documents(ctx)
	defer iter.Stop()

	count := 0
	for {
		_, err := iter.Next()
		if errors.Is(err, iterator.Done) {
			break
		}
		if err != nil {
			return 0, fmt.Errorf("%w: %v", shared.ErrInternal, err)
		}
		count++
	}
	return count, nil
}
