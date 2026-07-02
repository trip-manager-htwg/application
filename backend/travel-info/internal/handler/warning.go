package handler

import (
	"net/http"

	"github.com/trip-manager-htwg/application//backend/travel-info/internal/cache"
)

type WarningHandler struct {
	cache *cache.Cache
}

func NewWarningHandler(cache *cache.Cache) *WarningHandler {
	return &WarningHandler{cache: cache}
}

func (h *WarningHandler) GetWarning(w http.ResponseWriter, r *http.Request) {
	countryCode := r.PathValue("countryCode")
	if countryCode == "" {
		respondError(w, http.StatusBadRequest, "invalid country code")
		return
	}

	// Nutzt die neue spezifische Methode des kombinierten Caches
	warning, err := h.cache.GetWarning(r.Context(), countryCode)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to get warning")
		return
	}

	if warning == nil {
		respondError(w, http.StatusNotFound, "warning not found")
		return
	}

	respondJSON(w, http.StatusOK, warning)
}

func (h *WarningHandler) GetWarnings(w http.ResponseWriter, r *http.Request) {
	respondError(w, http.StatusNotImplemented, "not yet implemented")
}
