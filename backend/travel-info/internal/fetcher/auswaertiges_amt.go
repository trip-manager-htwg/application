package fetcher

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"
)

type WarningLevel int

const (
	NONE   WarningLevel = iota // 0
	LOW                        // 1
	MEDIUM                     // 2
	HIGH                       // 3
)

type WarningEntry struct {
	LastModified      int64  `json:"lastModified"`
	Effective         int64  `json:"effective"`
	Title             string `json:"title"`
	CountryCode       string `json:"CountryCode"`
	ISO3CountryCode   string `json:"iso3CountryCode"`
	CountryName       string `json:"CountryName"`
	Warning           bool   `json:"warning"`
	PartialWarning    bool   `json:"partialWarning"`
	SituationWarning  bool   `json:"situationWarning"`
	SituationPartWarn bool   `json:"situationPartWarning"`
}

func (w *WarningEntry) Level() WarningLevel {
	if w.Warning {
		return HIGH
	}
	if w.PartialWarning || w.SituationWarning {
		return MEDIUM
	}
	if w.SituationPartWarn {
		return LOW
	}
	return NONE
}

type WarningClient struct {
	httpClient *http.Client
	apiUrl     string
}

func NewWarningClient(apiURL string) *WarningClient {
	return &WarningClient{
		httpClient: &http.Client{Timeout: 30 * time.Second},
		apiUrl:     apiURL,
	}
}

func (c *WarningClient) FetchAll(ctx context.Context) (map[string]*WarningEntry, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.apiUrl, nil)
	if err != nil {
		return nil, fmt.Errorf("error creating request: %w", err)
	}
	req.Header.Set("Accept", "text/json;charset=UTF-8")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error executing request: %w", err)
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			log.Printf("error closing response body: %v", err)
		}
	}(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("error executing request, status code: %d", resp.StatusCode)
	}

	var raw struct {
		Response map[string]json.RawMessage `json:"response"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("error decoding response: %w", err)
	}

	result := make(map[string]*WarningEntry)
	for _, v := range raw.Response {
		var entry WarningEntry
		if err := json.Unmarshal(v, &entry); err != nil {
			continue
		}
		if entry.CountryCode == "" {
			continue
		}
		result[entry.CountryCode] = &entry
	}
	return result, nil
}
