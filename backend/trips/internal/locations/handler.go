package locations

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	openapitypes "github.com/oapi-codegen/runtime/types"
	"github.com/trip-manager-htwg/application/backend/shared/userclient"
	"github.com/trip-manager-htwg/application/backend/trips/generated"
	utils "github.com/trip-manager-htwg/application/backend/trips/internal/shared"
)

// ── Helpers ───────────────err────────────────────────────────────────────────────

func buildImageURL(s3Endpoint, s3Bucket, key string) string {
	return fmt.Sprintf("%s/%s/%s", s3Endpoint, s3Bucket, key)
}

// ── Mappers ───────────────────────────────────────────────────────────────────

func toImageResponse(img LocationImage) generated.LocationImageResponse {
	return generated.LocationImageResponse{
		Id:         uuid.MustParse(img.ID),
		LocationId: uuid.MustParse(img.LocationID),
		ImageUrl:   img.ImageKey,
		Sequence:   &img.Sequence,
		CreatedAt:  &img.CreatedAt,
	}
}

func toResponse(l *Location, s3Endpoint, s3Bucket string) generated.LocationResponse {
	images := make([]generated.LocationImageResponse, len(l.Images))
	for i, img := range l.Images {
		images[i] = toImageResponse(img)
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
		Id: uuid.MustParse(l.ID),
		CreatedBy: generated.UserSummary{
			Id:        uuid.MustParse(l.CreatedBy.ID),
			Name:      l.CreatedBy.Name,
			Email:     openapitypes.Email(l.CreatedBy.Email),
			AvatarUrl: avatarUrl,
		},
		CreatedAt:        l.CreatedAt,
		UpdatedAt:        l.UpdatedAt,
		Name:             l.Name,
		City:             l.City,
		Country:          l.Country,
		CountryCode:      l.CountryCode,
		ShortDescription: l.ShortDescription,
		DateFrom:         openapitypes.Date{Time: l.DateFrom},
		DateTo:           openapitypes.Date{Time: l.DateTo},
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
			utils.RespondError(w, http.StatusBadRequest, "tripId is required")
			return
		}
		limit := utils.GetIntQuery(r, "limit", 10)
		offset := utils.GetIntQuery(r, "offset", 0)

		locations, total, err := svc.ListByTrip(r.Context(), tripID, limit, offset)
		if err != nil {
			utils.RespondError(w, http.StatusInternalServerError, err.Error())
			return
		}
		data := make([]generated.LocationResponse, len(locations))
		for i, l := range locations {
			data[i] = toResponse(l, s3Endpoint, s3Bucket)
		}
		utils.RespondJSON(w, http.StatusOK, generated.LocationListResponse{
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
			utils.RespondError(w, http.StatusBadRequest, "tripId is required")
			return
		}
		token := utils.GetToken(r)
		if token == "" {
			utils.RespondError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		user, err := usersClient.GetMe(r.Context(), token)
		if err != nil {
			utils.RespondError(w, http.StatusInternalServerError, "failed to get user")
			return
		}

		var req generated.CreateLocationRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			utils.RespondError(w, http.StatusBadRequest, "invalid request body")
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
				utils.RespondError(w, http.StatusBadRequest, err.Error())
				return
			}
			utils.RespondError(w, http.StatusInternalServerError, err.Error())
			return
		}
		utils.RespondJSON(w, http.StatusCreated, toResponse(l, s3Endpoint, s3Bucket))
	}
}

func UpdateHandler(svc Service, s3Endpoint, s3Bucket string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		locationID := r.PathValue("locationId")
		if locationID == "" {
			utils.RespondError(w, http.StatusBadRequest, "locationId is required")
			return
		}

		var req generated.UpdateLocationRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			utils.RespondError(w, http.StatusBadRequest, "invalid request body")
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
				utils.RespondError(w, http.StatusNotFound, "location not found")
				return
			}
			utils.RespondError(w, http.StatusInternalServerError, err.Error())
			return
		}
		utils.RespondJSON(w, http.StatusOK, toResponse(l, s3Endpoint, s3Bucket))
	}
}

func DeleteHandler(svc Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		locationID := r.PathValue("locationId")
		if locationID == "" {
			utils.RespondError(w, http.StatusBadRequest, "locationId is required")
			return
		}
		if err := svc.Delete(r.Context(), locationID); err != nil {
			if errors.Is(err, ErrNotFound) {
				utils.RespondError(w, http.StatusNotFound, "location not found")
				return
			}
			utils.RespondError(w, http.StatusInternalServerError, err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func AddImageHandler(svc Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		locationID := r.PathValue("locationId")
		if locationID == "" {
			utils.RespondError(w, http.StatusBadRequest, "locationId is required")
			return
		}
		var req generated.AddLocationImageRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			utils.RespondError(w, http.StatusBadRequest, "invalid request body")
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
				utils.RespondError(w, http.StatusNotFound, "location not found")
				return
			}
			if errors.Is(err, ErrInvalidInput) {
				utils.RespondError(w, http.StatusBadRequest, err.Error())
				return
			}
			utils.RespondError(w, http.StatusInternalServerError, err.Error())
			return
		}
		utils.RespondJSON(w, http.StatusCreated, toImageResponse(*img))
	}
}

func DeleteImageHandler(svc Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		imageID := r.PathValue("imageId")
		if imageID == "" {
			utils.RespondError(w, http.StatusBadRequest, "imageId is required")
			return
		}
		if err := svc.DeleteImage(r.Context(), imageID); err != nil {
			if errors.Is(err, ErrNotFound) {
				utils.RespondError(w, http.StatusNotFound, "image not found")
				return
			}
			utils.RespondError(w, http.StatusInternalServerError, err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
