package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"gateway-microcontroller/internal/client"
	"gateway-microcontroller/internal/domain"
	"gateway-microcontroller/internal/handler"
	"gateway-microcontroller/internal/middleware"
	"gateway-microcontroller/internal/requestctx"
	"gateway-microcontroller/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

/* =========================
   MOCK
========================= */

var _ client.DetectorClient = (*mockDetectorClient)(nil)

/* =========================
   TEST SETUP (ALINEADO A EVENTS)
========================= */

func setupDeviceRouter(c client.DetectorClient) *gin.Engine {
	gin.SetMode(gin.TestMode)

	svc := service.NewDeviceService(c)
	h := handler.NewDeviceHandler(svc)

	r := gin.New()

	api := r.Group("/api/v1")
	api.Use(middleware.Auth(middleware.ExtractDeviceIDFromMTLS))
	api.POST("/devices", h.PostDevice)

	return r
}

/* =========================
   HELPERS
========================= */

/* =========================
   TESTS
========================= */

func TestPostDevice_Success_CreatesDeviceCorrectly(t *testing.T) {
	mockClient := new(mockDetectorClient)

	mockClient.On(
		"RegisterDevice",
		mock.MatchedBy(func(ctx context.Context) bool {
			deviceID, ok := requestctx.DeviceID(ctx)
			return ok && deviceID == "device-123"
		}),
		mock.MatchedBy(func(req *domain.DeviceRequest) bool {
			return req.ID == "device-123" &&
				req.SerialNumber == "SN-12345"
		}),
	).Return(GetDeviceSuccessResponseMock(), nil)

	router := setupDeviceRouter(mockClient)

	body, _ := json.Marshal(domain.DeviceRequest{
		ID:           "device-123",
		SerialNumber: "SN-12345",
		LastSeen:     "2026-06-28T15:30:00Z",
		LastBoot:     "2026-06-28T10:00:00Z",
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/devices", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Client-Cert-Subject", testSubjectHeader("device-123"))

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)

	var resp domain.DeviceResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	assert.NoError(t, err)

	assert.Equal(t, "device-123", resp.Data.ID)
	assert.Equal(t, "UNCLAIMED", resp.Data.Status)

	mockClient.AssertExpectations(t)
}

func TestPostDevice_ErrorPassthrough_ReturnsError(t *testing.T) {
	mockClient := new(mockDetectorClient)

	mockClient.On(
		"RegisterDevice",
		mock.Anything,
		mock.Anything,
	).Return(nil, GetDeviceErrorResponseMock())

	router := setupDeviceRouter(mockClient)

	body, _ := json.Marshal(domain.DeviceRequest{
		ID:           "device-123",
		SerialNumber: "SN-12345",
		LastSeen:     "2026-06-28T15:30:00Z",
		LastBoot:     "2026-06-28T10:00:00Z",
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/devices", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Client-Cert-Subject", testSubjectHeader("device-123"))

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusConflict, w.Code)

	var errResp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &errResp)
	assert.NoError(t, err)

	assert.Equal(t, float64(409), errResp["status"])
	assert.Equal(t, "Conflict", errResp["title"])

	mockClient.AssertExpectations(t)
}

func TestPostDevice_Unauthorized_MissingHeader(t *testing.T) {
	mockClient := new(mockDetectorClient)
	router := setupDeviceRouter(mockClient)

	body, _ := json.Marshal(domain.DeviceRequest{
		ID:           "device-123",
		SerialNumber: "SN-12345",
		LastSeen:     "2026-06-28T15:30:00Z",
		LastBoot:     "2026-06-28T10:00:00Z",
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/devices", bytes.NewReader(body))

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
	mockClient.AssertNotCalled(t, "RegisterDevice")
}

/* =========================
   BAD REQUEST TESTS
========================= */

func TestPostDevice_BadRequest_MissingSerialNumber(t *testing.T) {
	mockClient := new(mockDetectorClient)
	router := setupDeviceRouter(mockClient)

	body := []byte(`{"id":"device-123","lastSeen":"x","lastBoot":"y"}`)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/devices", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Client-Cert-Subject", testSubjectHeader("device-123"))

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	mockClient.AssertNotCalled(t, "RegisterDevice")
}

func TestPostDevice_BadRequest_MissingLastSeen(t *testing.T) {
	mockClient := new(mockDetectorClient)
	router := setupDeviceRouter(mockClient)

	body := []byte(`{"id":"device-123","serialNumber":"SN-123"}`)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/devices", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Client-Cert-Subject", testSubjectHeader("device-123"))

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	mockClient.AssertNotCalled(t, "RegisterDevice")
}
