package comment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/trip-manager-htwg/application//backend/shared/authclient"
	"github.com/trip-manager-htwg/application//backend/shared/userclient"
	"github.com/trip-manager-htwg/application//backend/social/internal/shared"
	"github.com/trip-manager-htwg/application//backend/social/pubsub"
)

func enrichCommentsWithUserInfo(ctx context.Context, comments []*CommentResponse, usersClient *userclient.UsersClient) {
	userIDs := make(map[string]struct{})
	for _, c := range comments {
		userIDs[c.User.ID] = struct{}{}
	}

	users := make(map[string]*userclient.UserResponse)
	var mu sync.Mutex
	var wg sync.WaitGroup

	for id := range userIDs {
		wg.Add(1)
		go func(userID string) {
			defer wg.Done()
			user, err := usersClient.GetByID(ctx, userID)
			if err != nil {
				return
			}
			mu.Lock()
			users[userID] = user
			mu.Unlock()
		}(id)
	}
	wg.Wait()

	for _, c := range comments {
		if u, ok := users[c.User.ID]; ok {
			c.User.Name = u.Name
			c.User.Email = u.Email
			c.User.AvatarUrl = u.AvatarUrl
		}
	}
}

// ListTripCommentsHandler handles GET /trips/{tripId}/comments (authclient required)
func ListTripCommentsHandler(svc Service, userClient *userclient.UsersClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tripID := r.PathValue("tripId")
		if tripID == "" {
			shared.RespondError(w, http.StatusBadRequest, "Trip ID is required")
			return
		}
		tenantID := authclient.GetTenantID(r)

		resp, err := svc.ListComments(r.Context(), tenantID, tripID)
		if err != nil {
			shared.RespondError(w, http.StatusInternalServerError, err.Error())
			return
		}

		enrichCommentsWithUserInfo(r.Context(), resp.Data, userClient)
		shared.RespondJSON(w, http.StatusOK, resp)
	}
}

// ListRepliesHandler handles GET /comments/{commentId}/replies (authclient required)
func ListRepliesHandler(svc Service, userClient userclient.UsersClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		commentID := r.PathValue("commentId")
		if commentID == "" {
			shared.RespondError(w, http.StatusBadRequest, "Comment ID is required")
			return
		}
		tenantID := authclient.GetTenantID(r)

		resp, err := svc.ListComments(r.Context(), tenantID, commentID)
		if err != nil {
			shared.RespondError(w, http.StatusInternalServerError, err.Error())
			return
		}

		shared.RespondJSON(w, http.StatusOK, resp)
	}
}

// CreateTripCommentHandler handles POST /trips/{tripId}/comments (authclient required)
func CreateTripCommentHandler(svc Service, usersClient *userclient.UsersClient, producer *pubsub.Producer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := authclient.GetUserID(r)
		if !ok {
			shared.RespondError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		tenantID := authclient.GetTenantID(r)

		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		user, err := usersClient.GetMe(r.Context(), token)
		if err != nil {
			shared.RespondError(w, http.StatusInternalServerError, "failed to get user info")
			return
		}

		tripID := r.PathValue("tripId")
		if tripID == "" {
			shared.RespondError(w, http.StatusBadRequest, "Trip ID is required")
			return
		}

		var req CreateCommentRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			shared.RespondError(w, http.StatusBadRequest, fmt.Sprintf("Invalid request body: %v", err))
			return
		}

		resp, err := svc.CreateComment(r.Context(), userID, user.ID, user.Name, user.Email, user.AvatarUrl, tenantID, tripID, req.Text)
		if err != nil {
			if errors.Is(err, shared.ErrInvalidInput) {
				shared.RespondError(w, http.StatusBadRequest, err.Error())
				return
			}
			shared.RespondError(w, http.StatusInternalServerError, err.Error())
			return
		}

		// Pub/Sub Event – fire-and-forget
		if producer != nil {
			if err := producer.PublishTripLiked(r.Context(), pubsub.TripLikedEvent{
				TripID:    tripID,
				UserID:    userID, // Firebase UID
				CreatedAt: time.Now().UTC().Format(time.RFC3339),
				TenantID:  tenantID,
			}); err != nil {
				log.Printf("warn: failed to publish trip.liked for trip %s: %v", tripID, err)
			}
		}

		shared.RespondJSON(w, http.StatusCreated, resp)
	}
}

// CreateReplyHandler handles POST /comments/{commentId}/replies (authclient required)
func CreateReplyHandler(svc Service, usersClient *userclient.UsersClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := authclient.GetUserID(r)
		if !ok {
			shared.RespondError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		tenantID := authclient.GetTenantID(r)

		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		user, err := usersClient.GetMe(r.Context(), token)
		if err != nil {
			shared.RespondError(w, http.StatusInternalServerError, "failed to get user info")
			return
		}

		commentID := r.PathValue("commentId")
		if commentID == "" {
			shared.RespondError(w, http.StatusBadRequest, "Comment ID is required")
			return
		}

		var req CreateCommentRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			shared.RespondError(w, http.StatusBadRequest, fmt.Sprintf("Invalid request body: %v", err))
			return
		}

		resp, err := svc.CreateComment(r.Context(), userID, user.ID, user.Name, user.Email, "", tenantID, commentID, req.Text)
		if err != nil {
			if errors.Is(err, shared.ErrInvalidInput) {
				shared.RespondError(w, http.StatusBadRequest, err.Error())
				return
			}
			if errors.Is(err, shared.ErrNotFound) {
				shared.RespondError(w, http.StatusNotFound, "parent comment not found")
				return
			}
			shared.RespondError(w, http.StatusInternalServerError, err.Error())
			return
		}

		shared.RespondJSON(w, http.StatusCreated, resp)
	}
}

// UpdateCommentHandler handles PUT /comments/{commentId} (authclient required)
func UpdateCommentHandler(svc Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := authclient.GetUserID(r)
		if !ok {
			shared.RespondError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		tenantID := authclient.GetTenantID(r)

		commentID := r.PathValue("commentId")
		if commentID == "" {
			shared.RespondError(w, http.StatusBadRequest, "Comment ID is required")
			return
		}

		var req CreateCommentRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			shared.RespondError(w, http.StatusBadRequest, fmt.Sprintf("Invalid request body: %v", err))
			return
		}

		resp, err := svc.UpdateComment(r.Context(), userID, tenantID, commentID, req.Text)
		if err != nil {
			if errors.Is(err, shared.ErrInvalidInput) {
				shared.RespondError(w, http.StatusBadRequest, err.Error())
				return
			}
			if errors.Is(err, shared.ErrNotFound) {
				shared.RespondError(w, http.StatusNotFound, "comment not found")
				return
			}
			if errors.Is(err, shared.ErrForbidden) {
				shared.RespondError(w, http.StatusForbidden, "forbidden")
				return
			}
			shared.RespondError(w, http.StatusInternalServerError, err.Error())
			return
		}

		shared.RespondJSON(w, http.StatusOK, resp)
	}
}

// DeleteCommentHandler handles DELETE /comments/{commentId} (authclient required)
func DeleteCommentHandler(svc Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := authclient.GetUserID(r)
		if !ok {
			shared.RespondError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		tenantID := authclient.GetTenantID(r)

		commentID := r.PathValue("commentId")
		if commentID == "" {
			shared.RespondError(w, http.StatusBadRequest, "Comment ID is required")
			return
		}

		err := svc.DeleteComment(r.Context(), userID, tenantID, commentID)
		if err != nil {
			if errors.Is(err, shared.ErrNotFound) {
				shared.RespondError(w, http.StatusNotFound, "comment not found")
				return
			}
			if errors.Is(err, shared.ErrForbidden) {
				shared.RespondError(w, http.StatusForbidden, "forbidden")
				return
			}
			shared.RespondError(w, http.StatusInternalServerError, err.Error())
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}
