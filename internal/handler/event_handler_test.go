package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"gateway-microcontroller/internal/client"
	"gateway-microcontroller/internal/domain"
	"gateway-microcontroller/internal/requestctx"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubMQTTMessage struct {
	topic   string
	payload []byte
}

func (m stubMQTTMessage) Duplicate() bool   { return false }
func (m stubMQTTMessage) Qos() byte         { return 1 }
func (m stubMQTTMessage) Retained() bool    { return false }
func (m stubMQTTMessage) Topic() string     { return m.topic }
func (m stubMQTTMessage) MessageID() uint16 { return 1 }
func (m stubMQTTMessage) Payload() []byte   { return m.payload }
func (m stubMQTTMessage) Ack()              {}

type recordingEventService struct {
	mu       sync.Mutex
	presses  []domain.DoorbellPressEvent
	ends     []domain.StreamEndEvent
	captures []domain.CaptureEvent
	pressErr error
	endErr   error
	procErr  error
}

func (s *recordingEventService) ProcessEvent(context.Context, *domain.EventRequest) (*domain.EventResponse, error) {
	if s.procErr != nil {
		return nil, s.procErr
	}
	return &domain.EventResponse{Data: domain.EventData{EventID: "http-event", DeviceID: "device-http"}}, nil
}

func (s *recordingEventService) HandleDoorbellPress(_ context.Context, event domain.DoorbellPressEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.presses = append(s.presses, event)
	return s.pressErr
}
func (s *recordingEventService) HandleStreamEnd(_ context.Context, event domain.StreamEndEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ends = append(s.ends, event)
	return s.endErr
}
func (s *recordingEventService) HandleCapture(_ context.Context, _ domain.MediaType, event domain.CaptureEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.captures = append(s.captures, event)
	return nil
}
func (s *recordingEventService) HandleDeviceMedia(context.Context, domain.MediaType, domain.DeviceMediaEvent) error {
	return nil
}
func (s *recordingEventService) SetDeviceCommander(client.DeviceCommander) {}
func (s *recordingEventService) Close()                                    {}

func (s *recordingEventService) pressCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.presses)
}

func TestEventHandler_PressBindsTopicIdentity(t *testing.T) {
	t.Parallel()

	svc := &recordingEventService{}
	handler := NewEventHandler(svc)
	payload, err := json.Marshal(domain.DoorbellPressEvent{Event: "press"})
	require.NoError(t, err)

	handler.handleHomeEventMessage(nil, stubMQTTMessage{
		topic:   "home/device-a/events/track-1/press",
		payload: payload,
	})

	require.Eventually(t, func() bool { return svc.pressCount() == 1 }, time.Second, 5*time.Millisecond)
	svc.mu.Lock()
	defer svc.mu.Unlock()
	assert.Equal(t, "device-a", svc.presses[0].DeviceID)
	assert.Equal(t, "track-1", svc.presses[0].EventID)
}

func TestEventHandler_IgnoresIdentityFieldsInPressBody(t *testing.T) {
	t.Parallel()

	svc := &recordingEventService{}
	handler := NewEventHandler(svc)
	handler.handleHomeEventMessage(nil, stubMQTTMessage{
		topic:   "home/device-a/events/track-1/press",
		payload: []byte(`{"eventId":"track-other","deviceId":"device-other","event":"press"}`),
	})

	require.Eventually(t, func() bool { return svc.pressCount() == 1 }, time.Second, 5*time.Millisecond)
	svc.mu.Lock()
	defer svc.mu.Unlock()
	assert.Equal(t, "device-a", svc.presses[0].DeviceID)
	assert.Equal(t, "track-1", svc.presses[0].EventID)
}

func TestEventHandler_StreamEndBindsTrackFromTopic(t *testing.T) {
	t.Parallel()

	svc := &recordingEventService{}
	handler := NewEventHandler(svc)
	handler.handleHomeEventMessage(nil, stubMQTTMessage{
		topic:   "home/device-a/events/track-1/stream_end",
		payload: []byte(`{"timestamp":1730000030000}`),
	})

	require.Len(t, svc.ends, 1)
	assert.Equal(t, "device-a", svc.ends[0].DeviceID)
	assert.Equal(t, "track-1", svc.ends[0].TrackID)
	assert.Equal(t, int64(1730000030000), svc.ends[0].Timestamp)
}

func TestEventHandler_StreamEndAcceptsEmptyJSON(t *testing.T) {
	t.Parallel()

	svc := &recordingEventService{}
	handler := NewEventHandler(svc)
	handler.handleHomeEventMessage(nil, stubMQTTMessage{
		topic:   "home/device-a/events/track-1/stream_end",
		payload: []byte(`{}`),
	})

	require.Len(t, svc.ends, 1)
	assert.Equal(t, "device-a", svc.ends[0].DeviceID)
	assert.Equal(t, "track-1", svc.ends[0].TrackID)
}

func TestEventHandler_IgnoresOwnAIResponse(t *testing.T) {
	t.Parallel()

	svc := &recordingEventService{}
	handler := NewEventHandler(svc)
	handler.handleHomeEventMessage(nil, stubMQTTMessage{
		topic:   "home/device-a/events/track-1/ia_response",
		payload: []byte(`{"action":"CONTINUE"}`),
	})

	assert.Equal(t, 0, svc.pressCount())
	assert.Empty(t, svc.ends)
}

func TestEventHandler_DiscardsInvalidPressJSON(t *testing.T) {
	t.Parallel()

	svc := &recordingEventService{}
	handler := NewEventHandler(svc)
	handler.handleHomeEventMessage(nil, stubMQTTMessage{
		topic:   "home/device-a/events/track-1/press",
		payload: []byte("not-json"),
	})

	assert.Equal(t, 0, svc.pressCount())
}

func TestEventHandler_DiscardsInvalidStreamEndJSON(t *testing.T) {
	t.Parallel()

	svc := &recordingEventService{}
	handler := NewEventHandler(svc)
	handler.handleHomeEventMessage(nil, stubMQTTMessage{
		topic:   "home/device-a/events/track-1/stream_end",
		payload: []byte("{"),
	})

	assert.Empty(t, svc.ends)
}

func TestEventHandler_CaptureImageUsesTopicIdentity(t *testing.T) {
	t.Parallel()

	svc := &recordingEventService{}
	handler := NewEventHandler(svc)
	handler.handleHomeEventMessage(nil, stubMQTTMessage{
		topic:   "home/device-a/events/track-1/capture_image",
		payload: []byte(`{}`),
	})

	require.Len(t, svc.captures, 1)
	assert.Equal(t, "device-a", svc.captures[0].DeviceID)
	assert.Equal(t, "track-1", svc.captures[0].TrackID)
}

func TestEventHandler_CaptureAudioUsesTopicIdentity(t *testing.T) {
	t.Parallel()

	svc := &recordingEventService{}
	handler := NewEventHandler(svc)
	handler.handleHomeEventMessage(nil, stubMQTTMessage{
		topic:   "home/device-a/events/track-1/capture_audio",
		payload: []byte(`{"timestamp":1730000010000}`),
	})

	require.Len(t, svc.captures, 1)
	assert.Equal(t, "device-a", svc.captures[0].DeviceID)
	assert.Equal(t, "track-1", svc.captures[0].TrackID)
}

func TestEventHandler_IgnoresUnknownEventAction(t *testing.T) {
	t.Parallel()

	svc := &recordingEventService{}
	handler := NewEventHandler(svc)
	handler.handleHomeEventMessage(nil, stubMQTTMessage{
		topic:   "home/device-a/events/track-1/telemetry",
		payload: []byte(`{}`),
	})

	assert.Equal(t, 0, svc.pressCount())
	assert.Empty(t, svc.ends)
	assert.Empty(t, svc.captures)
}

func TestEventHandler_DiscardsMalformedTopic(t *testing.T) {
	t.Parallel()

	svc := &recordingEventService{}
	handler := NewEventHandler(svc)
	handler.handleHomeEventMessage(nil, stubMQTTMessage{
		topic:   "home/doorbell/events/press",
		payload: []byte(`{"deviceId":"device-a","eventId":"track-1"}`),
	})

	assert.Equal(t, 0, svc.pressCount())
}

func TestEventHandler_SubscribesToHomeEventsWildcard(t *testing.T) {
	t.Parallel()

	client := &stubMQTTClient{token: stubMQTTToken{ok: true}}
	err := NewEventHandler(&recordingEventService{}).RegisterMQTTSubscriptions(client)
	require.NoError(t, err)
	assert.Equal(t, []string{domain.MQTTHomeEventsWildcard}, client.subscribed)
}

func TestEventHandler_PostEventCreatesLegacyHTTPEvent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	h := NewEventHandler(&recordingEventService{})
	router.POST("/api/v1/events", h.PostEvent)

	body, err := json.Marshal(domain.EventRequest{Type: domain.EventTypeMotion, Image: "base64data"})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/events", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)
}

func TestEventHandler_PostEventMapsDetectorFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	h := NewEventHandler(&recordingEventService{procErr: &domain.DetectorError{
		Code: domain.ErrCodeDetectorAnalysis, DetectorDetail: "upstream", StatusCode: http.StatusBadGateway,
	}})
	router.POST("/api/v1/events", h.PostEvent)

	body, err := json.Marshal(domain.EventRequest{Type: domain.EventTypeMotion, Image: "base64data"})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/events", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadGateway, w.Code)
}

func TestDeviceHandler_RegistersDeviceFromAuthenticatedContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := &recordingDeviceService{resp: &domain.DeviceResponse{Data: domain.DeviceData{ID: "device-http", SerialNumber: "SN-1"}}}
	router := gin.New()
	router.Use(func(c *gin.Context) {
		ctx := requestctx.WithDeviceID(c.Request.Context(), "device-http")
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	})
	router.POST("/api/v1/devices", NewDeviceHandler(svc).PostDevice)

	body, err := json.Marshal(domain.DeviceRequest{SerialNumber: "SN-1", LastSeen: "2026-01-01T00:00:00Z", LastBoot: "2026-01-01T00:00:00Z"})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/devices", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)
	assert.Equal(t, "device-http", svc.lastID)
}

type recordingDeviceService struct {
	resp   *domain.DeviceResponse
	lastID string
	err    error
}

func (s *recordingDeviceService) RegisterDevice(_ context.Context, req *domain.DeviceRequest) (*domain.DeviceResponse, error) {
	s.lastID = req.ID
	return s.resp, s.err
}

type stubMQTTToken struct {
	ok  bool
	err error
}

func (t stubMQTTToken) Wait() bool                     { return t.ok }
func (t stubMQTTToken) WaitTimeout(time.Duration) bool { return t.ok }
func (t stubMQTTToken) Done() <-chan struct{} {
	ch := make(chan struct{})
	close(ch)
	return ch
}
func (t stubMQTTToken) Error() error { return t.err }

type stubMQTTClient struct {
	subscribed []string
	published  []struct {
		topic   string
		payload []byte
	}
	token stubMQTTToken
}

func (c *stubMQTTClient) IsConnected() bool      { return true }
func (c *stubMQTTClient) IsConnectionOpen() bool { return true }
func (c *stubMQTTClient) Connect() mqtt.Token    { return c.token }
func (c *stubMQTTClient) Disconnect(uint)        {}
func (c *stubMQTTClient) Publish(topic string, _ byte, _ bool, payload interface{}) mqtt.Token {
	var body []byte
	switch p := payload.(type) {
	case []byte:
		body = p
	case string:
		body = []byte(p)
	}
	c.published = append(c.published, struct {
		topic   string
		payload []byte
	}{topic: topic, payload: body})
	return c.token
}
func (c *stubMQTTClient) Subscribe(topic string, _ byte, _ mqtt.MessageHandler) mqtt.Token {
	c.subscribed = append(c.subscribed, topic)
	return c.token
}
func (c *stubMQTTClient) SubscribeMultiple(map[string]byte, mqtt.MessageHandler) mqtt.Token {
	return c.token
}
func (c *stubMQTTClient) Unsubscribe(...string) mqtt.Token     { return c.token }
func (c *stubMQTTClient) AddRoute(string, mqtt.MessageHandler) {}
func (c *stubMQTTClient) OptionsReader() mqtt.ClientOptionsReader {
	return mqtt.ClientOptionsReader{}
}

var _ mqtt.Message = stubMQTTMessage{}
var _ mqtt.Client = (*stubMQTTClient)(nil)
