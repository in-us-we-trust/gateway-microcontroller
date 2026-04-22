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

func TestHTTPDetectorClient_Analyze_SendsDeviceIDHeader(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/v1/events", r.URL.Path)
		assert.Equal(t, "device-abc", r.Header.Get(requestctx.DeviceIDHeader))

		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(domain.EventData{IsIntruder: true}))
	}))
	defer server.Close()

	c := NewHTTPDetectorClient(server.URL)
	ctx := requestctx.WithDeviceID(context.Background(), "device-abc")

	resp, err := c.Analyze(ctx, &domain.EventRequest{Type: domain.EventTypeMotion, Image: "base64data"})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.IsIntruder)
}
