package integration_test

import (
	"bytes"
	"context"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/base64"
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

type mockDetectorClient struct {
	mock.Mock
}

func (m *mockDetectorClient) Analyze(ctx context.Context, req *domain.EventRequest) (*domain.EventData, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.EventData), args.Error(1)
}

func (m *mockDetectorClient) RegisterDevice(ctx context.Context, req *domain.DeviceRequest) (*domain.DeviceResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.DeviceResponse), args.Error(1)
}

var _ client.DetectorClient = (*mockDetectorClient)(nil)

func testSubjectHeader(deviceID string) string {
	name := pkix.Name{
		CommonName:         deviceID,
		Organization:       []string{"HogarSeguro"},
		OrganizationalUnit: []string{"IoT"},
	}

	rdn := name.ToRDNSequence()

	der, err := asn1.Marshal(rdn)
	if err != nil {
		panic(err)
	}

	return base64.StdEncoding.EncodeToString(der)
}

func setupRouter(c client.DetectorClient) *gin.Engine {
	gin.SetMode(gin.TestMode)

	svc := service.NewEventService(c)
	h := handler.NewEventHandler(svc)

	r := gin.New()
	api := r.Group("/api/v1")
	api.Use(middleware.Auth(middleware.ExtractDeviceIDFromMTLS))
	api.POST("/events", h.PostEvent)

	return r
}

func TestPostEvent_Success_MotionEvent(t *testing.T) {
	mockClient := new(mockDetectorClient)

	mockClient.On(
		"Analyze",
		mock.MatchedBy(func(ctx context.Context) bool {
			deviceID, ok := requestctx.DeviceID(ctx)
			return ok && deviceID == "device-123"
		}),
		mock.MatchedBy(func(req *domain.EventRequest) bool {
			return req.Type == domain.EventTypeMotion &&
				req.Image == "base64data"
		}),
	).Return(&domain.EventData{
		EventID:    "event-123",
		DeviceID:   "device-123",
		EventType:  "motion",
		IsIntruder: true,
	}, nil)

	router := setupRouter(mockClient)

	body, _ := json.Marshal(domain.EventRequest{
		Type:  domain.EventTypeMotion,
		Image: "base64data",
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/events", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Client-Cert-Subject", testSubjectHeader("device-123"))

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)

	var resp domain.EventResponse
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "event-123", resp.Data.EventID)
	assert.Equal(t, "device-123", resp.Data.DeviceID)
	assert.Equal(t, "motion", resp.Data.EventType)
	assert.True(t, resp.Data.IsIntruder)

	mockClient.AssertExpectations(t)
}

func TestPostEvent_Success_BellEvent(t *testing.T) {
	mockClient := new(mockDetectorClient)

	mockClient.On(
		"Analyze",
		mock.Anything,
		mock.MatchedBy(func(req *domain.EventRequest) bool {
			return req.Type == domain.EventTypeBell
		}),
	).Return(&domain.EventData{
		EventID:    "event-456",
		DeviceID:   "device-123",
		EventType:  "bell",
		IsIntruder: false,
	}, nil)

	router := setupRouter(mockClient)

	body, _ := json.Marshal(domain.EventRequest{
		Type:  domain.EventTypeBell,
		Image: "base64data",
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/events", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Client-Cert-Subject", testSubjectHeader("device-123"))

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)

	var resp domain.EventResponse
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "event-456", resp.Data.EventID)
	assert.Equal(t, "device-123", resp.Data.DeviceID)
	assert.Equal(t, "bell", resp.Data.EventType)
	assert.False(t, resp.Data.IsIntruder)

	mockClient.AssertExpectations(t)
}

func TestPostEvent_Unauthorized_MissingHeader(t *testing.T) {
	mockClient := new(mockDetectorClient)
	router := setupRouter(mockClient)

	body, _ := json.Marshal(domain.EventRequest{
		Type:  domain.EventTypeMotion,
		Image: "base64data",
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/events", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)

	var errResp map[string]interface{}
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &errResp))
	assert.Equal(t, float64(http.StatusUnauthorized), errResp["status"])
	assert.Equal(t, "about:blank", errResp["type"])

	mockClient.AssertNotCalled(t, "Analyze")
}

func TestPostEvent_BadRequest_MissingType(t *testing.T) {
	mockClient := new(mockDetectorClient)
	router := setupRouter(mockClient)

	body := []byte(`{"image":"base64data"}`)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/events", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Client-Cert-Subject", testSubjectHeader("device-123"))

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	mockClient.AssertNotCalled(t, "Analyze")
}

func TestPostEvent_BadRequest_MissingImage(t *testing.T) {
	mockClient := new(mockDetectorClient)
	router := setupRouter(mockClient)

	body := []byte(`{"type":"motion"}`)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/events", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Client-Cert-Subject", testSubjectHeader("device-123"))

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	mockClient.AssertNotCalled(t, "Analyze")
}

func TestPostEvent_BadRequest_InvalidType(t *testing.T) {
	mockClient := new(mockDetectorClient)
	router := setupRouter(mockClient)

	body := []byte(`{"type":"unknown","image":"base64data"}`)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/events", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Client-Cert-Subject", testSubjectHeader("device-123"))

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	mockClient.AssertNotCalled(t, "Analyze")
}

func TestPostEvent_InternalServerError_DetectorFailure(t *testing.T) {
	mockClient := new(mockDetectorClient)
	mockClient.On("Analyze", mock.Anything, mock.Anything).Return(nil, assert.AnError)

	router := setupRouter(mockClient)

	body, _ := json.Marshal(domain.EventRequest{
		Type:  domain.EventTypeMotion,
		Image: "base64data",
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/events", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Client-Cert-Subject", testSubjectHeader("device-123"))

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	mockClient.AssertExpectations(t)
}

func TestPostEvent_Unauthorized_InvalidClientCertSubject(t *testing.T) {
	mockClient := new(mockDetectorClient)
	router := setupRouter(mockClient)

	body, _ := json.Marshal(domain.EventRequest{
		Type:  domain.EventTypeMotion,
		Image: "base64data",
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/events", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Client-Cert-Subject", "not-base64")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
	mockClient.AssertNotCalled(t, "Analyze")
}
