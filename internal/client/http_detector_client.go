package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"path/filepath"
	"strings"
	"time"

	"gateway-microcontroller/internal/domain"
	"gateway-microcontroller/internal/requestctx"
	"gateway-microcontroller/internal/util"
)

const orchestratorEventsPath = "/v1/orchestrator/events"

type HTTPDetectorClient struct {
	baseURL    string
	httpClient *http.Client
}

func NewHTTPDetectorClient(baseURL string) *HTTPDetectorClient {
	return NewHTTPDetectorClientWithTimeout(baseURL, 15*time.Second)
}

func NewHTTPDetectorClientWithTimeout(baseURL string, timeout time.Duration) *HTTPDetectorClient {
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	return &HTTPDetectorClient{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout:   timeout,
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

	if !util.Is2xx(resp.StatusCode) {
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

func (c *HTTPDetectorClient) RegisterDevice(ctx context.Context, req *domain.DeviceRequest) (*domain.DeviceResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/v1/devices", c.baseURL)

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

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if !util.Is2xx(resp.StatusCode) {
		return nil, &domain.DevicesControllerError{
			StatusCode: resp.StatusCode,
			Body:       respBody,
		}
	}

	var response domain.DeviceResponse
	if err := json.Unmarshal(respBody, &response); err != nil {
		return nil, err
	}

	return &response, nil
}

func (c *HTTPDetectorClient) SendMediaEvent(ctx context.Context, req domain.MediaEventRequest) (*domain.OrchestratorEventData, error) {
	contentType, err := mediaContentType(req)
	if err != nil {
		return nil, err
	}
	if req.TrackID == "" || req.DeviceID == "" {
		return nil, fmt.Errorf("track ID and device ID are required")
	}
	if len(req.Buffer) == 0 {
		return nil, fmt.Errorf("media buffer is empty")
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	fields := []struct {
		name  string
		value string
	}{
		{name: "trackId", value: req.TrackID},
		{name: "deviceId", value: req.DeviceID},
		{name: "mediaType", value: string(req.MediaType)},
	}
	for _, field := range fields {
		if err := writer.WriteField(field.name, field.value); err != nil {
			return nil, fmt.Errorf("write multipart field %s: %w", field.name, err)
		}
	}

	fileHeader := make(textproto.MIMEHeader)
	fileHeader.Set("Content-Disposition", mime.FormatMediaType("form-data", map[string]string{
		"name":     "file",
		"filename": req.FileName,
	}))
	fileHeader.Set("Content-Type", contentType)

	filePart, err := writer.CreatePart(fileHeader)
	if err != nil {
		return nil, fmt.Errorf("create multipart file part: %w", err)
	}
	if _, err := filePart.Write(req.Buffer); err != nil {
		return nil, fmt.Errorf("write multipart file: %w", err)
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("close multipart body: %w", err)
	}

	url := strings.TrimRight(c.baseURL, "/") + orchestratorEventsPath
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, &body)
	if err != nil {
		return nil, fmt.Errorf("create media event request: %w", err)
	}
	httpReq.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send media event: %w", err)
	}
	defer resp.Body.Close()

	responseBody, readErr := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if readErr != nil {
		return nil, fmt.Errorf("read media event response: %w", readErr)
	}

	if util.Is2xx(resp.StatusCode) {
		var envelope domain.OrchestratorEventResponse
		if err := json.Unmarshal(responseBody, &envelope); err != nil {
			return nil, fmt.Errorf("decode orchestrator response: %w", err)
		}
		if envelope.Data.TrackID == "" {
			envelope.Data.TrackID = req.TrackID
		}
		if envelope.Data.ChunkType == "" {
			envelope.Data.ChunkType = string(req.MediaType)
		}
		return &envelope.Data, nil
	}
	detail := strings.TrimSpace(string(responseBody))
	if detail == "" {
		detail = fmt.Sprintf("detective manager returned status %d", resp.StatusCode)
	}

	return nil, &domain.DetectorError{
		Code:           domain.ErrCodeMediaDispatch,
		DetectorDetail: detail,
		StatusCode:     resp.StatusCode,
	}
}

func mediaContentType(req domain.MediaEventRequest) (string, error) {
	switch req.MediaType {
	case domain.MediaTypeImage:
		return "image/jpeg", nil
	case domain.MediaTypeAudio:
		switch strings.ToLower(filepath.Ext(req.FileName)) {
		case ".mp3":
			return "audio/mpeg", nil
		case ".wav":
			return "audio/wav", nil
		default:
			return "", fmt.Errorf("unsupported audio file extension %q", filepath.Ext(req.FileName))
		}
	case domain.MediaTypeVideo:
		return "video/mp4", nil
	default:
		return "", fmt.Errorf("unsupported media type %q", req.MediaType)
	}
}
