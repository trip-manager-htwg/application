package tenant

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"tenantdb"
	"time"

	"github.com/trip-manager-htwg/application/backend/shared/authclient"
	"github.com/trip-manager-htwg/application/backend/users/internal/platform"
)

type UsageResponse struct {
	TenantID  string         `json:"tenantId"`
	Period    string         `json:"period"`
	APICalls  int64          `json:"apiCalls"`
	Breakdown []ServiceUsage `json:"breakdown"`
	Pricing   PricingInfo    `json:"pricing"`
}

type ServiceUsage struct {
	Service string `json:"service"`
	Calls   int64  `json:"calls"`
}

type PricingInfo struct {
	Tier        string  `json:"tier"`
	BasePrice   float64 `json:"basePrice"`
	APICallCost float64 `json:"apiCallCost"`
	TotalCost   float64 `json:"totalCost"`
	Currency    string  `json:"currency"`
}

func GetUsageHandler(repo Repository, metricsClient MetricsClient, platformRepo platform.Repository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID := authclient.GetTenantID(r)
		if tenantID == "" || tenantID == "default" {
			respondError(w, http.StatusNotFound, "no tenant found")
			return
		}

		serviceMap, err := metricsClient.QueryAPICallsByService(r.Context(), tenantID)
		if err != nil {
			respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to query metrics: %v", err))
			return
		}

		var totalCalls int64
		var breakdown []ServiceUsage
		for svc, calls := range serviceMap {
			totalCalls += calls
			breakdown = append(breakdown, ServiceUsage{Service: svc, Calls: calls})
		}

		tenantCtx := tenantdb.WithTenantID(r.Context(), tenantID)
		tenant, err := repo.GetByID(tenantCtx, tenantID)
		if err != nil {
			respondError(w, http.StatusNotFound, "tenant not found")
			return
		}

		// Platform Config laden
		platformCfg, err := platformRepo.GetConfig(r.Context())
		if err != nil {
			// Fallback auf defaults
			platformCfg = &platform.PlatformConfig{
				Free:       platform.PricingConfig{BasePrice: 0, FreeAPICalls: 0, PricePerCall: 0},
				Standard:   platform.PricingConfig{BasePrice: 29, FreeAPICalls: 10000, PricePerCall: 0.001},
				Enterprise: platform.PricingConfig{BasePrice: 99, FreeAPICalls: 100000, PricePerCall: 0.0005},
			}
		}

		pricing := calculatePricing(tenant.Tier, totalCalls, *platformCfg)
		respondJSON(w, http.StatusOK, UsageResponse{
			TenantID:  tenantID,
			Period:    time.Now().Format("2006-01"),
			APICalls:  totalCalls,
			Breakdown: breakdown,
			Pricing:   pricing,
		})
	}
}

func GetUsageTimeSeriesHandler(metricsClient MetricsClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID := authclient.GetTenantID(r)
		role := authclient.GetUserRole(r)

		// Platform-Admin kann tenantId als Query-Parameter übergeben
		if role == "platform_admin" {
			if qTenantID := r.URL.Query().Get("tenantId"); qTenantID != "" {
				tenantID = qTenantID
			}
		}

		if tenantID == "" || tenantID == "default" {
			respondError(w, http.StatusNotFound, "no tenant found")
			return
		}

		days := 30
		data, err := metricsClient.QueryAPICallsTimeSeries(r.Context(), tenantID, days)
		if err != nil {
			respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to query time series: %v", err))
			return
		}

		respondJSON(w, http.StatusOK, data)
	}
}

type prometheusResult struct {
	Metric map[string]string
	Value  int64
}

func queryPrometheus(baseURL, query string) ([]prometheusResult, error) {
	params := url.Values{}
	params.Set("query", query)

	resp, err := http.Get(fmt.Sprintf("%s/api/v1/query?%s", baseURL, params.Encode()))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	var result struct {
		Data struct {
			Result []struct {
				Metric map[string]string `json:"metric"`
				Value  []interface{}     `json:"value"`
			} `json:"result"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}

	var out []prometheusResult
	for _, r := range result.Data.Result {
		var calls int64
		if len(r.Value) > 1 {
			fmt.Sscanf(fmt.Sprintf("%v", r.Value[1]), "%d", &calls)
		}
		out = append(out, prometheusResult{
			Metric: r.Metric,
			Value:  calls,
		})
	}
	return out, nil
}

// usage.go - calculatePricing anpassen
func calculatePricing(tier string, apiCalls int64, cfg platform.PlatformConfig) PricingInfo {
	var pricingTier platform.PricingConfig
	switch tier {
	case "standard":
		pricingTier = cfg.Standard
	case "enterprise":
		pricingTier = cfg.Enterprise
	default:
		pricingTier = cfg.Free
	}

	var apiCallCost float64
	if apiCalls > pricingTier.FreeAPICalls {
		apiCallCost = float64(apiCalls-pricingTier.FreeAPICalls) * pricingTier.PricePerCall
	}

	return PricingInfo{
		Tier:        tier,
		BasePrice:   pricingTier.BasePrice,
		APICallCost: apiCallCost,
		TotalCost:   pricingTier.BasePrice + apiCallCost,
		Currency:    "EUR",
	}
}
