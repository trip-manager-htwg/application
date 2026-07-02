package transport

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"
	"github.com/trip-manager-htwg/application/backend/shared/userclient"
	generated "github.com/trip-manager-htwg/application/backend/trips/generated"
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

// ── Mapper ────────────────────────────────────────────────────────────────────

func toPlaceSummary(p Place) generated.PlaceSummary {
	return generated.PlaceSummary{
		Name:    p.Name,
		City:    p.City,
		Country: p.Country,
		Lat:     p.Lat,
		Lng:     p.Lng,
	}
}

func toResponse(t *Transport) generated.TransportResponse {
	id, _ := uuid.Parse(t.ID)
	creatorID, _ := uuid.Parse(t.CreatedBy.ID)

	var notes *string
	if t.Notes != "" {
		notes = &t.Notes
	}

	from := toPlaceSummary(t.From)
	to := toPlaceSummary(t.To)

	return generated.TransportResponse{
		Id: openapi_types.UUID(id),
		CreatedBy: generated.UserSummary{
			Id:    openapi_types.UUID(creatorID),
			Name:  t.CreatedBy.Name,
			Email: openapi_types.Email(t.CreatedBy.Email),
		},
		CreatedAt:     t.CreatedAt,
		UpdatedAt:     t.UpdatedAt,
		From:          from,
		To:            to,
		DepartureTime: t.DepartureTime,
		ArrivalTime:   t.ArrivalTime,
		Type:          generated.TransportResponseType(t.Type),
		Notes:         notes,
	}
}

// ── Handlers ──────────────────────────────────────────────────────────────────

func ListHandler(svc Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tripID := r.PathValue("tripId")
		if tripID == "" {
			respondError(w, http.StatusBadRequest, "tripId is required")
			return
		}
		limit := getIntQuery(r, "limit", 10)
		offset := getIntQuery(r, "offset", 0)

		transports, total, err := svc.ListByTrip(r.Context(), tripID, limit, offset)
		if err != nil {
			respondError(w, http.StatusInternalServerError, err.Error())
			return
		}
		data := make([]generated.TransportResponse, len(transports))
		for i, t := range transports {
			data[i] = toResponse(t)
		}
		respondJSON(w, http.StatusOK, generated.TransportListResponse{
			Data:   data,
			Total:  total,
			Limit:  limit,
			Offset: offset,
		})
	}
}

func CreateHandler(svc Service, usersClient *userclient.UsersClient) http.HandlerFunc {
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

		var req generated.CreateTransportRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			respondError(w, http.StatusBadRequest, "invalid request body")
			return
		}

		t, err := svc.Create(r.Context(), &req, tripID, user.ID, user.Name, user.Email)
		if err != nil {
			if errors.Is(err, ErrInvalidInput) {
				respondError(w, http.StatusBadRequest, err.Error())
				return
			}
			respondError(w, http.StatusInternalServerError, err.Error())
			return
		}
		respondJSON(w, http.StatusCreated, toResponse(t))
	}
}

func UpdateHandler(svc Service, usersClient *userclient.UsersClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		transportID := r.PathValue("transportId")
		if transportID == "" {
			respondError(w, http.StatusBadRequest, "transportId is required")
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

		var req generated.UpdateTransportRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			respondError(w, http.StatusBadRequest, "invalid request body")
			return
		}

		t, err := svc.Update(r.Context(), &req, transportID, user.ID)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				respondError(w, http.StatusNotFound, "transport not found")
				return
			}
			respondError(w, http.StatusInternalServerError, err.Error())
			return
		}
		respondJSON(w, http.StatusOK, toResponse(t))
	}
}

func DeleteHandler(svc Service, usersClient *userclient.UsersClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		transportID := r.PathValue("transportId")
		if transportID == "" {
			respondError(w, http.StatusBadRequest, "transportId is required")
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

		if err := svc.Delete(r.Context(), transportID, user.ID); err != nil {
			if errors.Is(err, ErrNotFound) {
				respondError(w, http.StatusNotFound, "transport not found")
				return
			}
			respondError(w, http.StatusInternalServerError, err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
