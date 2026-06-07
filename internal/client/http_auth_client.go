package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

type HTTPAuthClient struct {
	baseURL    string
	httpClient *http.Client
}

func NewHTTPAuthClient(baseURL string) *HTTPAuthClient {
	return &HTTPAuthClient{
		baseURL: baseURL,
		httpClient: &http.Client{
			Transport: &loggingRoundTripper{next: http.DefaultTransport},
		},
	}
}

type validateDeviceRequest struct {
	DeviceID string `json:"deviceId"`
	Serial   string `json:"serial"`
}

type validateDeviceResponse struct {
	Valid bool `json:"valid"`
}

func (c *HTTPAuthClient) ValidateDevice(
	ctx context.Context,
	deviceID string,
	serial string,
) (bool, error) {
	body, err := json.Marshal(validateDeviceRequest{
		DeviceID: deviceID,
		Serial:   serial,
	})
	if err != nil {
		return false, err
	}

	httpReq, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.baseURL+"/internal/devices/validate",
		bytes.NewReader(body),
	)
	if err != nil {
		return false, err
	}

	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {

		var errBody struct {
			Message string `json:"message"`
		}

		detail := fmt.Sprintf(
			"auth service returned status %d",
			resp.StatusCode,
		)

		if json.NewDecoder(resp.Body).Decode(&errBody) == nil &&
			errBody.Message != "" {
			detail = errBody.Message
		}

		return false, fmt.Errorf(detail)
	}

	var result validateDeviceResponse

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return false, err
	}

	return result.Valid, nil
}
