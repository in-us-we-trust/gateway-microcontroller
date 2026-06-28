package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"gateway-microcontroller/internal/domain"
	"gateway-microcontroller/internal/requestctx"
)

type HTTPDetectorClient struct {
	baseURL    string
	httpClient *http.Client
}

func NewHTTPDetectorClient(baseURL string) *HTTPDetectorClient {
	return &HTTPDetectorClient{
		baseURL: baseURL,
		httpClient: &http.Client{
			Transport: &loggingRoundTripper{next: http.DefaultTransport},
		},
	}
}

func (c *HTTPDetectorClient) Analyze(ctx context.Context, req *domain.EventRequest) (*domain.EventData, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	deviceID, ok := requestctx.DeviceID(ctx)
	if !ok {
		return nil, fmt.Errorf("device ID not found in request context")
	}

	url := fmt.Sprintf("%s/v1/devices/%s/events", c.baseURL, deviceID)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		var errBody struct {
			Detail string `json:"detail"`
		}
		detail := fmt.Sprintf("detector service returned status %d", resp.StatusCode)
		if json.NewDecoder(resp.Body).Decode(&errBody) == nil && errBody.Detail != "" {
			detail = errBody.Detail
		}
		return nil, &domain.DetectorError{
			Code:           domain.ErrCodeDetectorAnalysis,
			DetectorDetail: detail,
			StatusCode:     resp.StatusCode,
		}
	}

	var response domain.EventResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, err
	}

	return &response.Data, nil
}
