package locations

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"
	"github.com/trip-manager-htwg/application/backend/shared/userclient"
	"github.com/trip-manager-htwg/application/backend/trips/generated"
)

// ── Helpers ───────────────────────────────────────────────────────────────────

func respondJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func respondError(w http.ResponseWriter, status int, msg string) {
	respondJSON(w, status, map[string]string{"error": msg})
}

func getToken(r *http.Request) string {
	return strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
}

func getIntQuery(r *http.Request, key string, defaultVal int) int {
	val := r.URL.Query().Get(key)
	if val == "" {
		return defaultVal
	}
	n, err := strconv.Atoi(val)
	if err != nil {
		return defaultVal
	}
	return n
}

func buildImageURL(s3Endpoint, s3Bucket, key string) string {
	return fmt.Sprintf("%s/%s/%s", s3Endpoint, s3Bucket, key)
}

// ── Mappers ───────────────────────────────────────────────────────────────────

func toImageResponse(img LocationImage, s3Endpoint, s3Bucket string) generated.LocationImageResponse {
	return generated.LocationImageResponse{
		Id:         openapi_types.UUID(uuid.MustParse(img.ID)),
		LocationId: openapi_types.UUID(uuid.MustParse(img.LocationID)),
		ImageUrl:   img.ImageKey,
		Sequence:   &img.Sequence,
		CreatedAt:  &img.CreatedAt,
	}
}

func toResponse(l *Location, s3Endpoint, s3Bucket string) generated.LocationResponse {
	images := make([]generated.LocationImageResponse, len(l.Images))
	for i, img := range l.Images {
		images[i] = toImageResponse(img, s3Endpoint, s3Bucket)
	}

	var avatarUrl *string
	if l.CreatedBy.AvatarKey != nil {
		url := buildImageURL(s3Endpoint, s3Bucket, *l.CreatedBy.AvatarKey)
		avatarUrl = &url
	}

	var lat, lon *float32
	if l.Latitude != nil {
		v := float32(*l.Latitude)
		lat = &v
	}
	if l.Longitude != nil {
		v := float32(*l.Longitude)
		lon = &v
	}

	return generated.LocationResponse{
		Id: openapi_types.UUID(uuid.MustParse(l.ID)),
		CreatedBy: generated.UserSummary{
			Id:        openapi_types.UUID(uuid.MustParse(l.CreatedBy.ID)),
			Name:      l.CreatedBy.Name,
			Email:     openapi_types.Email(l.CreatedBy.Email),
			AvatarUrl: avatarUrl,
		},
		CreatedAt:        l.CreatedAt,
		UpdatedAt:        l.UpdatedAt,
		Name:             l.Name,
		City:             l.City,
		Country:          l.Country,
		CountryCode:      l.CountryCode,
		ShortDescription: l.ShortDescription,
		DateFrom:         openapi_types.Date{Time: l.DateFrom},
		DateTo:           openapi_types.Date{Time: l.DateTo},
		Latitude:         lat,
		Longitude:        lon,
		Notes:            l.Notes,
		Sequence:         l.Sequence,
		Images:           &images,
	}
}

// ── Handlers ──────────────────────────────────────────────────────────────────

func ListHandler(svc Service, s3Endpoint, s3Bucket string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tripID := r.PathValue("tripId")
		if tripID == "" {
			respondError(w, http.StatusBadRequest, "tripId is required")
			return
		}
		limit := getIntQuery(r, "limit", 10)
		offset := getIntQuery(r, "offset", 0)

		locations, total, err := svc.ListByTrip(r.Context(), tripID, limit, offset)
		if err != nil {
			respondError(w, http.StatusInternalServerError, err.Error())
			return
		}
		data := make([]generated.LocationResponse, len(locations))
		for i, l := range locations {
			data[i] = toResponse(l, s3Endpoint, s3Bucket)
		}
		respondJSON(w, http.StatusOK, generated.LocationListResponse{
			Data:   data,
			Total:  total,
			Limit:  limit,
			Offset: offset,
		})
	}
}

func CreateHandler(svc Service, usersClient *userclient.UsersClient, s3Endpoint, s3Bucket string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tripID := r.PathValue("tripId")
		if tripID == "" {
			respondError(w, http.StatusBadRequest, "tripId is required")
			return
		}
		token := getToken(r)
		if token == "" {
			respondError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		user, err := usersClient.GetMe(r.Context(), token)
		if err != nil {
			respondError(w, http.StatusInternalServerError, "failed to get user")
			return
		}

		var req generated.CreateLocationRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			respondError(w, http.StatusBadRequest, "invalid request body")
			return
		}

		var lat, lon *float64
		if req.Latitude != nil {
			v := float64(*req.Latitude)
			lat = &v
		}
		if req.Longitude != nil {
			v := float64(*req.Longitude)
			lon = &v
		}

		l, err := svc.Create(r.Context(), CreateInput{
			TripID:           tripID,
			UserID:           user.ID,
			UserName:         user.Name,
			UserEmail:        user.Email,
			UserAvatarKey:    &user.AvatarUrl,
			Name:             req.Name,
			City:             req.City,
			Country:          req.Country,
			CountryCode:      *req.CountryCode,
			ShortDescription: req.ShortDescription,
			DateFrom:         req.DateFrom.Time,
			DateTo:           req.DateTo.Time,
			Latitude:         lat,
			Longitude:        lon,
			Notes:            req.Notes,
			Sequence:         req.Sequence,
		})
		if err != nil {
			if errors.Is(err, ErrInvalidInput) {
				respondError(w, http.StatusBadRequest, err.Error())
				return
			}
			respondError(w, http.StatusInternalServerError, err.Error())
			return
		}
		respondJSON(w, http.StatusCreated, toResponse(l, s3Endpoint, s3Bucket))
	}
}

func UpdateHandler(svc Service, s3Endpoint, s3Bucket string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		locationID := r.PathValue("locationId")
		if locationID == "" {
			respondError(w, http.StatusBadRequest, "locationId is required")
			return
		}

		var req generated.UpdateLocationRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			respondError(w, http.StatusBadRequest, "invalid request body")
			return
		}

		input := UpdateInput{
			ID:               locationID,
			Name:             req.Name,
			City:             req.City,
			Country:          req.Country,
			CountryCode:      req.CountryCode,
			ShortDescription: req.ShortDescription,
			Notes:            req.Notes,
			Sequence:         req.Sequence,
		}
		if req.DateFrom != nil {
			t := req.DateFrom.Time
			input.DateFrom = &t
		}
		if req.DateTo != nil {
			t := req.DateTo.Time
			input.DateTo = &t
		}
		if req.Latitude != nil {
			v := float64(*req.Latitude)
			input.Latitude = &v
		}
		if req.Longitude != nil {
			v := float64(*req.Longitude)
			input.Longitude = &v
		}

		l, err := svc.Update(r.Context(), input)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				respondError(w, http.StatusNotFound, "location not found")
				return
			}
			respondError(w, http.StatusInternalServerError, err.Error())
			return
		}
		respondJSON(w, http.StatusOK, toResponse(l, s3Endpoint, s3Bucket))
	}
}

func DeleteHandler(svc Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		locationID := r.PathValue("locationId")
		if locationID == "" {
			respondError(w, http.StatusBadRequest, "locationId is required")
			return
		}
		if err := svc.Delete(r.Context(), locationID); err != nil {
			if errors.Is(err, ErrNotFound) {
				respondError(w, http.StatusNotFound, "location not found")
				return
			}
			respondError(w, http.StatusInternalServerError, err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func AddImageHandler(svc Service, s3Endpoint, s3Bucket string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		locationID := r.PathValue("locationId")
		if locationID == "" {
			respondError(w, http.StatusBadRequest, "locationId is required")
			return
		}
		var req generated.AddLocationImageRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			respondError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		seq := 0
		if req.Sequence != nil {
			seq = *req.Sequence
		}
		img, err := svc.AddImage(r.Context(), AddImageInput{
			LocationID: locationID,
			ImageKey:   req.ImageKey,
			Sequence:   seq,
		})
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				respondError(w, http.StatusNotFound, "location not found")
				return
			}
			if errors.Is(err, ErrInvalidInput) {
				respondError(w, http.StatusBadRequest, err.Error())
				return
			}
			respondError(w, http.StatusInternalServerError, err.Error())
			return
		}
		respondJSON(w, http.StatusCreated, toImageResponse(*img, s3Endpoint, s3Bucket))
	}
}

func DeleteImageHandler(svc Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		imageID := r.PathValue("imageId")
		if imageID == "" {
			respondError(w, http.StatusBadRequest, "imageId is required")
			return
		}
		if err := svc.DeleteImage(r.Context(), imageID); err != nil {
			if errors.Is(err, ErrNotFound) {
				respondError(w, http.StatusNotFound, "image not found")
				return
			}
			respondError(w, http.StatusInternalServerError, err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
