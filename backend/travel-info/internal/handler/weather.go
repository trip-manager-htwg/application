package handler

import (
	"log"
	"net/http"
	"strconv"

	"github.com/trip-manager-htwg/application/backend/travel-info/internal/cache"
	"github.com/trip-manager-htwg/application/backend/travel-info/internal/fetcher"
)

type WeatherHandler struct {
	cache   *cache.Cache
	fetcher *fetcher.OpenMeteoClient
}

func NewWeatherHandler(cache *cache.Cache, fetcher *fetcher.OpenMeteoClient) *WeatherHandler {
	return &WeatherHandler{
		cache:   cache,
		fetcher: fetcher,
	}
}

func (h *WeatherHandler) GetWeather(w http.ResponseWriter, r *http.Request) {
	latStr := r.URL.Query().Get("lat")
	lngStr := r.URL.Query().Get("lng")

	if latStr == "" || lngStr == "" {
		respondError(w, http.StatusBadRequest, "lat and lng are required")
		return
	}

	lat, err := strconv.ParseFloat(latStr, 64)
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid lat")
		return
	}

	lng, err := strconv.ParseFloat(lngStr, 64)
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid lng")
		return
	}
	date := r.URL.Query().Get("date")

	// Nutzt die neue spezifische Methode des kombinierten Caches
	weather, err := h.cache.GetWeather(r.Context(), lat, lng)
	if err != nil {
		log.Printf("cache get error: %v", err)
	}

	if weather != nil {
		respondJSON(w, http.StatusOK, weather)
		return
	}

	log.Printf("cache miss for lat=%.2f lng=%.2f – fetching from Open-Meteo", lat, lng)
	weather, err = h.fetcher.FetchForecast(r.Context(), lat, lng, date)
	if err != nil {
		log.Printf("fetch forecast error: %v", err)
		respondError(w, http.StatusServiceUnavailable, "failed to fetch weather data")
		return
	}

	// Nutzt die neue spezifische Methode des kombinierten Caches
	if err := h.cache.SetWeather(r.Context(), weather); err != nil {
		log.Printf("cache set error: %v", err)
	}

	respondJSON(w, http.StatusOK, weather)
}
