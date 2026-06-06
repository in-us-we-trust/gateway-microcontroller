package mtls_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"gateway-microcontroller/internal/domain"
	"gateway-microcontroller/internal/requestctx"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

const eventsPath = "/api/v1/events"

func postEvent(t *testing.T, client *http.Client, baseURL string, body []byte) *http.Response {
	t.Helper()

	req, err := http.NewRequest(http.MethodPost, baseURL+eventsPath, bytes.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	require.NoError(t, err)
	return resp
}

func decodeError(t *testing.T, resp *http.Response) map[string]string {
	t.Helper()
	defer resp.Body.Close()

	var payload map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&payload))
	return payload
}

func TestE2E_PostEvent_ValidClientCert(t *testing.T) {
	env := newMTLSServer(t)
	defer env.Server.Close()
	clientCert := env.CA.issueClientCert(t, "device-123", 10)
	httpClient := tlsHTTPClient(env.CA, clientCert.TLS)
	env.Auth.On("ValidateDevice", mock.Anything, "device-123", clientCert.X509.SerialNumber.String()).
		Return(true, nil)
	env.Detector.On("Analyze",
		mock.MatchedBy(func(ctx context.Context) bool {
			device, ok := requestctx.GetDeviceContext(ctx)
			return ok && device.DeviceID == "device-123" && device.Serial == clientCert.X509.SerialNumber.String()
		}),
		mock.Anything,
	).Return(&domain.EventData{IsIntruder: true}, nil)

	body, _ := json.Marshal(domain.EventRequest{Type: domain.EventTypeMotion, Image: "base64data"})
	resp := postEvent(t, httpClient, env.Server.URL, body)

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var eventResp domain.EventResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&eventResp))
	resp.Body.Close()
	assert.True(t, eventResp.Data.IsIntruder)
	env.Auth.AssertExpectations(t)
	env.Detector.AssertExpectations(t)
}

func TestE2E_PostEvent_DeviceNotAuthorized(t *testing.T) {
	env := newMTLSServer(t)
	defer env.Server.Close()
	clientCert := env.CA.issueClientCert(t, "device-123", 11)
	httpClient := tlsHTTPClient(env.CA, clientCert.TLS)
	env.Auth.On("ValidateDevice", mock.Anything, "device-123", clientCert.X509.SerialNumber.String()).
		Return(false, nil)

	body, _ := json.Marshal(domain.EventRequest{Type: domain.EventTypeMotion, Image: "base64data"})
	resp := postEvent(t, httpClient, env.Server.URL, body)

	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	errBody := decodeError(t, resp)
	assert.Equal(t, "device not authorized", errBody["error"])
	env.Detector.AssertNotCalled(t, "Analyze")
}

func TestE2E_PostEvent_MissingDeviceIDInCert(t *testing.T) {
	env := newMTLSServer(t)
	defer env.Server.Close()
	clientCert := env.CA.issueClientCert(t, "", 12)
	httpClient := tlsHTTPClient(env.CA, clientCert.TLS)

	body, _ := json.Marshal(domain.EventRequest{Type: domain.EventTypeMotion, Image: "base64data"})
	resp := postEvent(t, httpClient, env.Server.URL, body)

	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	errBody := decodeError(t, resp)
	assert.Equal(t, "device id not found in certificate", errBody["error"])
	env.Auth.AssertNotCalled(t, "ValidateDevice")
	env.Detector.AssertNotCalled(t, "Analyze")
}

func TestE2E_PostEvent_PlainHTTPRejected(t *testing.T) {
	env := newHTTPServer(t)
	defer env.Server.Close()

	body, _ := json.Marshal(domain.EventRequest{Type: domain.EventTypeMotion, Image: "base64data"})
	resp := postEvent(t, &http.Client{}, env.Server.URL, body)

	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	errBody := decodeError(t, resp)
	assert.Equal(t, "tls required", errBody["error"])
	env.Detector.AssertNotCalled(t, "Analyze")
}

func TestE2E_PostEvent_NoClientCertificate(t *testing.T) {
	env := newMTLSServer(t)
	defer env.Server.Close()
	httpClient := tlsHTTPClientWithoutCert(env.CA)
	body, _ := json.Marshal(domain.EventRequest{Type: domain.EventTypeMotion, Image: "base64data"})

	req, err := http.NewRequest(http.MethodPost, env.Server.URL+eventsPath, bytes.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	_, err = httpClient.Do(req)

	assert.Error(t, err)
	env.Detector.AssertNotCalled(t, "Analyze")
}

func TestE2E_PostEvent_WrongCAClientCert(t *testing.T) {
	env := newMTLSServer(t)
	defer env.Server.Close()
	foreignCert := issueClientCertFromOtherCA(t, "device-123")
	httpClient := tlsHTTPClient(env.CA, foreignCert.TLS)

	body, _ := json.Marshal(domain.EventRequest{Type: domain.EventTypeMotion, Image: "base64data"})
	req, err := http.NewRequest(http.MethodPost, env.Server.URL+eventsPath, bytes.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	_, err = httpClient.Do(req)

	assert.Error(t, err)
	env.Detector.AssertNotCalled(t, "Analyze")
}

func TestE2E_PostEvent_AuthServiceFailure(t *testing.T) {
	env := newMTLSServer(t)
	defer env.Server.Close()
	clientCert := env.CA.issueClientCert(t, "device-123", 13)
	httpClient := tlsHTTPClient(env.CA, clientCert.TLS)
	env.Auth.On("ValidateDevice", mock.Anything, "device-123", clientCert.X509.SerialNumber.String()).
		Return(false, assert.AnError)

	body, _ := json.Marshal(domain.EventRequest{Type: domain.EventTypeMotion, Image: "base64data"})
	resp := postEvent(t, httpClient, env.Server.URL, body)

	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	errBody := decodeError(t, resp)
	assert.NotEmpty(t, errBody["error"])
	env.Detector.AssertNotCalled(t, "Analyze")
}
