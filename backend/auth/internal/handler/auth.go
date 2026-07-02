package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/trip-manager-htwg/application/backend/auth/internal/service"
	"github.com/trip-manager-htwg/application/backend/auth/internal/shared"
)

// Handler handles auth endpoints
type Handler struct {
	service service.Service
}

// NewHandler creates a new auth handler
func NewHandler(svc service.Service) *Handler {
	return &Handler{
		service: svc,
	}
}

// ValidateTokenRequest is the request body for validating a token
type ValidateTokenRequest struct {
	Token string `json:"token" binding:"required"`
}

// ValidateToken handles POST /validate-token
// Validates a Bearer token and returns user info
func (h *Handler) ValidateToken(w http.ResponseWriter, r *http.Request) {
	var req ValidateTokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		shared.RespondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Token == "" {
		shared.RespondError(w, http.StatusBadRequest, "token is required")
		return
	}

	resp, err := h.service.ValidateToken(r.Context(), req.Token)
	if err != nil {
		shared.RespondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if !resp.Valid {
		shared.RespondJSON(w, http.StatusUnauthorized, resp)
		return
	}

	shared.RespondJSON(w, http.StatusOK, resp)
}

// ValidateTokenFromHeader handles GET /validate-token with Authorization header
// Useful when called from other services
func (h *Handler) ValidateTokenFromHeader(w http.ResponseWriter, r *http.Request) {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		shared.RespondError(w, http.StatusBadRequest, "Authorization header is required")
		return
	}

	// Extract token from "Bearer <token>" format
	token := strings.TrimPrefix(authHeader, "Bearer ")
	if token == authHeader { // No Bearer prefix found
		token = authHeader
	}

	resp, err := h.service.ValidateToken(r.Context(), token)
	if err != nil {
		shared.RespondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if !resp.Valid {
		shared.RespondJSON(w, http.StatusUnauthorized, resp)
		return
	}

	shared.RespondJSON(w, http.StatusOK, resp)
}
