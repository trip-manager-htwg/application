package tenant

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	monitoring "cloud.google.com/go/monitoring/apiv3/v2"
	"cloud.google.com/go/monitoring/apiv3/v2/monitoringpb"
	"google.golang.org/api/iterator"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type GCMMetricsClient struct {
	ProjectID string
}

func NewGCMMetricsClient(projectID string) *GCMMetricsClient {
	return &GCMMetricsClient{ProjectID: projectID}
}

func (c *GCMMetricsClient) QueryAPICallsByService(ctx context.Context, tenantID string) (map[string]int64, error) {
	client, err := monitoring.NewMetricClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create monitoring client: %w", err)
	}
	defer func(client *monitoring.MetricClient) {
		err := client.Close()
		if err != nil {
			log.Printf("failed to close monitoring client: %v", err)
		}
	}(client)

	now := time.Now()
	startOfMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)

	req := &monitoringpb.ListTimeSeriesRequest{
		Name:   fmt.Sprintf("projects/%s", c.ProjectID),
		Filter: fmt.Sprintf(`metric.type="custom.googleapis.com/trip_manager/api_calls_total" AND metric.labels.tenant_id="%s"`, tenantID),
		Interval: &monitoringpb.TimeInterval{
			StartTime: timestamppb.New(startOfMonth),
			EndTime:   timestamppb.New(now),
		},
		View: monitoringpb.ListTimeSeriesRequest_FULL,
	}

	services := map[string]int64{}
	it := client.ListTimeSeries(ctx, req)
	for {
		ts, err := it.Next()
		if errors.Is(err, iterator.Done) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to query metrics: %w", err)
		}
		svc := ts.Metric.Labels["service"]

		var lastValue int64
		for _, point := range ts.Points {
			v := point.Value.GetInt64Value()
			if v > lastValue {
				lastValue = v
			}
		}
		services[svc] += lastValue
	}
	return services, nil
}

type DailyAPICall struct {
	Date  string `json:"date"`
	Calls int64  `json:"calls"`
}

func (c *GCMMetricsClient) QueryAPICallsTimeSeries(ctx context.Context, tenantID string, days int) ([]DailyAPICall, error) {
	client, err := monitoring.NewMetricClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create monitoring client: %w", err)
	}
	defer func(client *monitoring.MetricClient) {
		err := client.Close()
		if err != nil {
			log.Printf("failed to close monitoring client: %v", err)
		}
	}(client)

	now := time.Now()
	start := now.AddDate(0, 0, -days)

	req := &monitoringpb.ListTimeSeriesRequest{
		Name:   fmt.Sprintf("projects/%s", c.ProjectID),
		Filter: fmt.Sprintf(`metric.type="custom.googleapis.com/trip_manager/api_calls_total" AND metric.labels.tenant_id="%s"`, tenantID),
		Interval: &monitoringpb.TimeInterval{
			StartTime: timestamppb.New(start),
			EndTime:   timestamppb.New(now),
		},
		Aggregation: &monitoringpb.Aggregation{
			AlignmentPeriod:    &durationpb.Duration{Seconds: 86400}, // 1 Tag
			PerSeriesAligner:   monitoringpb.Aggregation_ALIGN_DELTA,
			CrossSeriesReducer: monitoringpb.Aggregation_REDUCE_SUM,
		},
		View: monitoringpb.ListTimeSeriesRequest_FULL,
	}

	dailyMap := map[string]int64{}
	it := client.ListTimeSeries(ctx, req)
	for {
		ts, err := it.Next()
		if errors.Is(err, iterator.Done) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to query time series: %w", err)
		}
		for _, point := range ts.Points {
			date := point.Interval.StartTime.AsTime().Format("2006-01-02")
			dailyMap[date] += point.Value.GetInt64Value()
		}
	}

	// Lücken mit 0 füllen
	var result []DailyAPICall
	for i := days; i >= 0; i-- {
		date := now.AddDate(0, 0, -i).Format("2006-01-02")
		result = append(result, DailyAPICall{
			Date:  date,
			Calls: dailyMap[date],
		})
	}
	return result, nil
}
