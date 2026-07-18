package handler

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"github.com/google/uuid"
	openapitypes "github.com/oapi-codegen/runtime/types"
	"github.com/trip-manager-htwg/application/backend/shared/authclient"
	"github.com/trip-manager-htwg/application/backend/users/generated"
	"github.com/trip-manager-htwg/application/backend/users/repository"
	"github.com/trip-manager-htwg/application/backend/users/service"
)

// ── Helpers ───────────────────────────────────────────────────────────────────

func respondJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	err := json.NewEncoder(w).Encode(data)
	if err != nil {
		log.Printf("Failed to encode JSON response %v", err)
		return
	}
}

func respondError(w http.ResponseWriter, status int, msg string) {
	respondJSON(w, status, map[string]string{"error": msg})
}

func toResponse(u *service.User) generated.UserResponse {
	var avatarUrl *string
	if u.AvatarKey != "" {
		avatarUrl = &u.AvatarKey
	}
	id := openapitypes.UUID{}
	if u.ID != "" {
		parsed, err := uuid.Parse(u.ID)
		if err == nil {
			id = parsed
		}
	}
	return generated.UserResponse{
		Id:        &id,
		Email:     openapitypes.Email(u.Email),
		Name:      u.Name,
		Bio:       toPtr(u.Bio),
		AvatarUrl: avatarUrl,
	}
}

// ── Handlers ──────────────────────────────────────────────────────────────────

func ProvisionHandler(svc service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		firebaseUID, ok := authclient.GetUserID(r)
		if !ok {
			respondError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		var req struct {
			Name     *string `json:"name"`
			TenantID string  `json:"tenantId"`
		}
		if r.ContentLength > 0 {
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				respondError(w, http.StatusBadRequest, "invalid request body")
				return
			}
		}

		email, _ := authclient.GetUserEmail(r)
		name := ""
		if req.Name != nil {
			name = *req.Name
		}

		// tenantId aus Body oder aus JWT Claims
		tenantID := req.TenantID
		if tenantID == "" {
			tenantID = authclient.GetTenantID(r)
		}

		user, created, err := svc.Provision(r.Context(), service.ProvisionInput{
			FirebaseUID: firebaseUID,
			Email:       email,
			Name:        name,
			TenantID:    tenantID,
		})
		if err != nil {
			respondError(w, http.StatusInternalServerError, err.Error())
			return
		}

		status := http.StatusOK
		if created {
			status = http.StatusCreated
		}
		respondJSON(w, status, toResponse(user))
	}
}

func GetMeHandler(svc service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		firebaseUID, ok := authclient.GetUserID(r)
		if !ok {
			respondError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		user, err := svc.GetByFirebaseUID(r.Context(), firebaseUID)
		if err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				respondError(w, http.StatusNotFound, "user not found")
				return
			}
			respondError(w, http.StatusInternalServerError, err.Error())
			return
		}

		respondJSON(w, http.StatusOK, toResponse(user))
	}
}

func UpdateMeHandler(svc service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := authclient.GetUserID(r)
		if !ok {
			respondError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		var req generated.UpdateUserRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			respondError(w, http.StatusBadRequest, "invalid request body")
			return
		}

		var email *string
		if req.Email != nil {
			s := string(*req.Email)
			email = &s
		}

		user, err := svc.Update(r.Context(), service.UpdateInput{
			ID:        userID,
			Name:      req.Name,
			Email:     email,
			Bio:       req.Bio,
			AvatarKey: req.AvatarKey,
		})
		if err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				respondError(w, http.StatusNotFound, "user not found")
				return
			}
			respondError(w, http.StatusInternalServerError, err.Error())
			return
		}

		respondJSON(w, http.StatusOK, toResponse(user))
	}
}

func GetByIDHandler(svc service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if id == "" {
			respondError(w, http.StatusBadRequest, "id is required")
			return
		}
		user, err := svc.GetByID(r.Context(), id)
		if err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				respondError(w, http.StatusNotFound, "user not found")
				return
			}
			respondError(w, http.StatusInternalServerError, err.Error())
			return
		}
		respondJSON(w, http.StatusOK, toResponse(user))
	}
}

func toPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
