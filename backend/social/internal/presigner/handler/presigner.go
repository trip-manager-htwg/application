package handler

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/trip-manager-htwg/application//backend/shared/authclient"
	"github.com/trip-manager-htwg/application//backend/social/internal/presigner/service"
	"github.com/trip-manager-htwg/application//backend/social/internal/shared"
)

// GetPresignedUploadURLHandler handles POST /upload-url
// Returns a presigned URL for direct uploads to MinIO/S3 or GCS
func GetPresignedUploadURLHandler(svc service.Service, authClient *authclient.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := authclient.GetUserID(r)
		if !ok {
			shared.RespondError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		var req PresignedURLRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			shared.RespondError(w, http.StatusBadRequest, fmt.Sprintf("invalid request: %v", err))
			return
		}

		if req.FileName == "" {
			shared.RespondError(w, http.StatusBadRequest, "fileName is required")
			return
		}
		if req.MediaType == "" {
			shared.RespondError(w, http.StatusBadRequest, "mediaType is required")
			return
		}

		// Parse media type
		mediaType, err := service.ParseMediaType(req.MediaType)
		if err != nil {
			shared.RespondError(w, http.StatusBadRequest, fmt.Sprintf("invalid media type: %v", err))
			return
		}

		// Get presigned upload URL with file name
		ticket, err := svc.PrepareUpload(r.Context(), userID, mediaType, req.FileName)
		if err != nil {
			shared.RespondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to prepare upload: %v", err))
			return
		}

		shared.RespondJSON(w, http.StatusOK, PresignedURLResponse{
			PresignedUrl: ticket.UploadURL,
			ExpiresIn:    int(ticket.ExpiresIn.Seconds()),
			Key:          ticket.Key,
		})
	}
}

// GetPresignedDownloadURLHandler handles POST /download-url
func GetPresignedDownloadURLHandler(svc service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req PresignedDownloadRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			shared.RespondError(w, http.StatusBadRequest, fmt.Sprintf("invalid request: %v", err))
			return
		}

		if req.Key == "" {
			shared.RespondError(w, http.StatusBadRequest, "key is required")
			return
		}

		// Get presigned download URL
		url, err := svc.GetDownloadURL(r.Context(), req.Key)
		if err != nil {
			shared.RespondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to get download URL: %v", err))
			return
		}

		shared.RespondJSON(w, http.StatusOK, map[string]interface{}{
			"url": url,
		})
	}
}

// PresignedURLRequest Request for a presigned upload URL
type PresignedURLRequest struct {
	FileName  string `json:"fileName"`  // e.g., "avatar.jpg"
	MediaType string `json:"mediaType"` // e.g., "avatar", "trip", "location"
}

// PresignedDownloadRequest Request for a presigned download URL
type PresignedDownloadRequest struct {
	Key string `json:"key"` // Object key
}

// PresignedURLResponse Response with presigned upload URL
type PresignedURLResponse struct {
	PresignedUrl string `json:"presignedUrl"`
	ExpiresIn    int    `json:"expiresIn"` // in seconds
	Key          string `json:"key"`       // Object key for DB
}
