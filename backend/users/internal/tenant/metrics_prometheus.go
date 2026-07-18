package tenant

import (
	"context"
	"fmt"
)

type PrometheusMetricsClient struct {
	URL string
}

func NewPrometheusMetricsClient(url string) *PrometheusMetricsClient {
	return &PrometheusMetricsClient{URL: url}
}

func (c *PrometheusMetricsClient) QueryAPICallsByService(ctx context.Context, tenantID string) (map[string]int64, error) {
	query := fmt.Sprintf(
		`sum by (service) (trip_manager_api_calls_total{tenant_id="%s"})`,
		tenantID,
	)
	result, err := queryPrometheus(c.URL, query)
	if err != nil {
		return nil, err
	}
	services := map[string]int64{}
	for _, r := range result {
		services[r.Metric["service"]] = r.Value
	}
	return services, nil
}

func (c *PrometheusMetricsClient) QueryAPICallsTimeSeries(ctx context.Context, tenantID string, days int) ([]DailyAPICall, error) {
	//TODO implement me
	panic("implement me")
}
