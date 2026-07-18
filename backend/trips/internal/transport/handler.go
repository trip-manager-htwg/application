package transport

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/google/uuid"
	openapitypes "github.com/oapi-codegen/runtime/types"
	"github.com/trip-manager-htwg/application/backend/shared/userclient"
	"github.com/trip-manager-htwg/application/backend/trips/generated"
	utils "github.com/trip-manager-htwg/application/backend/trips/internal/shared"
)

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
		Id: id,
		CreatedBy: generated.UserSummary{
			Id:    creatorID,
			Name:  t.CreatedBy.Name,
			Email: openapitypes.Email(t.CreatedBy.Email),
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
			utils.RespondError(w, http.StatusBadRequest, "tripId is required")
			return
		}
		limit := utils.GetIntQuery(r, "limit", 10)
		offset := utils.GetIntQuery(r, "offset", 0)

		transports, total, err := svc.ListByTrip(r.Context(), tripID, limit, offset)
		if err != nil {
			utils.RespondError(w, http.StatusInternalServerError, err.Error())
			return
		}
		data := make([]generated.TransportResponse, len(transports))
		for i, t := range transports {
			data[i] = toResponse(t)
		}
		utils.RespondJSON(w, http.StatusOK, generated.TransportListResponse{
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

		var req generated.CreateTransportRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			utils.RespondError(w, http.StatusBadRequest, "invalid request body")
			return
		}

		t, err := svc.Create(r.Context(), &req, tripID, user.ID, user.Name, user.Email)
		if err != nil {
			if errors.Is(err, ErrInvalidInput) {
				utils.RespondError(w, http.StatusBadRequest, err.Error())
				return
			}
			utils.RespondError(w, http.StatusInternalServerError, err.Error())
			return
		}
		utils.RespondJSON(w, http.StatusCreated, toResponse(t))
	}
}

func UpdateHandler(svc Service, usersClient *userclient.UsersClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		transportID := r.PathValue("transportId")
		if transportID == "" {
			utils.RespondError(w, http.StatusBadRequest, "transportId is required")
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

		var req generated.UpdateTransportRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			utils.RespondError(w, http.StatusBadRequest, "invalid request body")
			return
		}

		t, err := svc.Update(r.Context(), &req, transportID, user.ID)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				utils.RespondError(w, http.StatusNotFound, "transport not found")
				return
			}
			utils.RespondError(w, http.StatusInternalServerError, err.Error())
			return
		}
		utils.RespondJSON(w, http.StatusOK, toResponse(t))
	}
}

func DeleteHandler(svc Service, usersClient *userclient.UsersClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		transportID := r.PathValue("transportId")
		if transportID == "" {
			utils.RespondError(w, http.StatusBadRequest, "transportId is required")
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

		if err := svc.Delete(r.Context(), transportID, user.ID); err != nil {
			if errors.Is(err, ErrNotFound) {
				utils.RespondError(w, http.StatusNotFound, "transport not found")
				return
			}
			utils.RespondError(w, http.StatusInternalServerError, err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
