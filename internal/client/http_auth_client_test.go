package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHTTPAuthClient_ValidateDevice_Valid(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/internal/devices/validate", r.URL.Path)

		var body validateDeviceRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		assert.Equal(t, "device-abc", body.DeviceID)
		assert.Equal(t, "serial-1", body.Serial)

		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(validateDeviceResponse{Valid: true}))
	}))
	defer server.Close()

	c := NewHTTPAuthClient(server.URL)
	valid, err := c.ValidateDevice(context.Background(), "device-abc", "serial-1")
	require.NoError(t, err)
	assert.True(t, valid)
}

func TestHTTPAuthClient_ValidateDevice_NotValid(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		require.NoError(t, json.NewEncoder(w).Encode(validateDeviceResponse{Valid: false}))
	}))
	defer server.Close()

	c := NewHTTPAuthClient(server.URL)
	valid, err := c.ValidateDevice(context.Background(), "device-abc", "serial-1")
	require.NoError(t, err)
	assert.False(t, valid)
}

func TestHTTPAuthClient_ValidateDevice_NonOKWithMessage(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		require.NoError(t, json.NewEncoder(w).Encode(map[string]string{"message": "forbidden device"}))
	}))
	defer server.Close()

	c := NewHTTPAuthClient(server.URL)
	_, err := c.ValidateDevice(context.Background(), "device-abc", "serial-1")
	require.Error(t, err)
	assert.EqualError(t, err, "forbidden device")
}

func TestHTTPAuthClient_ValidateDevice_NonOKWithoutMessage(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	c := NewHTTPAuthClient(server.URL)
	_, err := c.ValidateDevice(context.Background(), "device-abc", "serial-1")
	require.Error(t, err)
	assert.EqualError(t, err, "auth service returned status 500")
}
