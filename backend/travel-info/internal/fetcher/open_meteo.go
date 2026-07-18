package fetcher

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

type DailyForecast struct {
	Date            string  `json:"date"`
	TempMax         float64 `json:"tempMax"`
	TempMin         float64 `json:"tempMin"`
	PrecipitationMm float64 `json:"precipitationMm"`
	WeatherCode     int     `json:"weatherCode"`
	Description     string  `json:"description"`
}

type WeatherResponse struct {
	Latitude  float64         `json:"latitude"`
	Longitude float64         `json:"longitude"`
	Forecast  []DailyForecast `json:"forecast"`
	UpdatedAt time.Time       `json:"updatedAt"`
}

type openMeteoResponse struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Daily     struct {
		Time            []string  `json:"time"`
		TempMax         []float64 `json:"temperature_2m_max"`
		TempMin         []float64 `json:"temperature_2m_min"`
		PrecipitationMm []float64 `json:"precipitation_sum"`
		WeatherCode     []int     `json:"weathercode"`
	} `json:"daily"`
}

type OpenMeteoClient struct {
	httpClient   *http.Client
	baseURL      string
	forecastDays int
}

func NewOpenMeteoClient(baseURL string, forecastDays int) *OpenMeteoClient {
	return &OpenMeteoClient{
		httpClient:   &http.Client{Timeout: 10 * time.Second},
		baseURL:      baseURL,
		forecastDays: forecastDays,
	}
}

func (c *OpenMeteoClient) FetchForecast(ctx context.Context, lat, lng float64, startDate string) (*WeatherResponse, error) {
	params := url.Values{}
	params.Set("latitude", strconv.FormatFloat(lat, 'f', 2, 64))
	params.Set("longitude", strconv.FormatFloat(lng, 'f', 2, 64))
	params.Set("daily", "temperature_2m_max,temperature_2m_min,precipitation_sum,weathercode")
	params.Set("timezone", "auto")
	params.Set("forecast_days", strconv.Itoa(c.forecastDays))

	if startDate != "" {
		params.Set("start_date", startDate)
		params.Set("end_date", startDate)
		params.Del("forecast_days")
		t, err := time.Parse("2006-01-02", startDate)
		if err == nil {
			params.Set("end_date", t.AddDate(0, 0, 2).Format("2006-01-02"))
		}
	}

	reqURL := fmt.Sprintf("%s?%s", c.baseURL, params.Encode())

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch forecast: %w", err)
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			log.Printf("failed to close response body: %v", err)
		}
	}(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	var raw openMeteoResponse
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	forecast := make([]DailyForecast, len(raw.Daily.Time))
	for i, date := range raw.Daily.Time {
		forecast[i] = DailyForecast{
			Date:            date,
			TempMax:         raw.Daily.TempMax[i],
			TempMin:         raw.Daily.TempMin[i],
			PrecipitationMm: raw.Daily.PrecipitationMm[i],
			WeatherCode:     raw.Daily.WeatherCode[i],
			Description:     weatherCodeDescription(raw.Daily.WeatherCode[i]),
		}
	}

	return &WeatherResponse{
		Latitude:  raw.Latitude,
		Longitude: raw.Longitude,
		Forecast:  forecast,
		UpdatedAt: time.Now(),
	}, nil
}

func weatherCodeDescription(code int) string {
	switch {
	case code == 0:
		return "Klarer Himmel"
	case code <= 2:
		return "Überwiegend klar"
	case code == 3:
		return "Bewölkt"
	case code <= 49:
		return "Nebel"
	case code <= 59:
		return "Nieselregen"
	case code <= 69:
		return "Regen"
	case code <= 79:
		return "Schnee"
	case code <= 84:
		return "Schauer"
	case code <= 94:
		return "Gewitter"
	default:
		return "Starkes Gewitter"
	}
}
