package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"gateway-microcontroller/internal/domain"
	"gateway-microcontroller/internal/requestctx"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHTTPDetectorClient_Analyze_SendsDeviceIDInPath(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/v1/devices/device-abc/events", r.URL.Path)

		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(domain.EventResponse{
			Data: domain.EventData{
				EventID:    "event-123",
				DeviceID:   "device-abc",
				IsIntruder: true,
			},
		}))
	}))
	defer server.Close()

	c := NewHTTPDetectorClient(server.URL)
	ctx := requestctx.WithDeviceID(context.Background(), "device-abc")

	resp, err := c.Analyze(ctx, &domain.EventRequest{
		Type:  domain.EventTypeMotion,
		Image: "base64data",
	})

	require.NoError(t, err)
	require.NotNil(t, resp)

	assert.Equal(t, "event-123", resp.EventID)
	assert.Equal(t, "device-abc", resp.DeviceID)
	assert.True(t, resp.IsIntruder)
}
