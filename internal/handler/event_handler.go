package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"

	"gateway-microcontroller/internal/domain"
	"gateway-microcontroller/internal/service"
	"gateway-microcontroller/pkg/apierror"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/gin-gonic/gin"
)

const (
	TopicDoorbellStatus       = "home/doorbell/status"
	TopicDoorbellPress        = "home/doorbell/events/press"
	TopicDoorbellStreamEnd    = "home/doorbell/events/stream_end"
	TopicDoorbellImage        = "home/doorbell/events/image"
	TopicDoorbellAudio        = "home/doorbell/events/audio"
	TopicDoorbellVideo        = "home/doorbell/events/video"
	TopicDoorbellCaptureImage = "home/doorbell/events/capture_image"
	TopicDoorbellCaptureAudio = "home/doorbell/events/capture_audio"
	TopicDoorbellTelemetry    = "home/doorbell/telemetry"
	TopicDoorbellAIResponse   = "home/doorbell/events/ia_response"
)

type EventHandler struct {
	service service.EventService
}

func NewEventHandler(svc service.EventService) *EventHandler {
	return &EventHandler{service: svc}
}

func (h *EventHandler) RegisterMQTTSubscriptions(client mqtt.Client) error {
	subscriptions := []struct {
		topic   string
		handler mqtt.MessageHandler
	}{
		{topic: TopicDoorbellPress, handler: h.handleDoorbellPressMessage},
		{topic: TopicDoorbellStreamEnd, handler: h.handleStreamEndMessage},
		{topic: TopicDoorbellCaptureImage, handler: h.handleCapture(domain.MediaTypeImage)},
		{topic: TopicDoorbellCaptureAudio, handler: h.handleCapture(domain.MediaTypeAudio)},
	}

	for _, subscription := range subscriptions {
		token := client.Subscribe(subscription.topic, 1, subscription.handler)
		if !token.WaitTimeout(10 * time.Second) {
			return fmt.Errorf("subscribe to MQTT topic %s: timeout", subscription.topic)
		}
		if err := token.Error(); err != nil {
			return fmt.Errorf("subscribe to MQTT topic %s: %w", subscription.topic, err)
		}
		log.Printf("subscribed to MQTT topic %s with QoS 1", subscription.topic)
	}

	return nil
}

func (h *EventHandler) handleDoorbellPressMessage(_ mqtt.Client, message mqtt.Message) {
	rawPayload := string(message.Payload())
	loggedPayload := rawPayload
	if len(loggedPayload) > 240 {
		loggedPayload = loggedPayload[:120] + "... (" + fmt.Sprintf("%d", len(rawPayload)) + " chars)"
	}

	log.Printf(
		"MQTT RECEIVED topic=%s payload=%q",
		message.Topic(),
		loggedPayload,
	)

	var event domain.DoorbellPressEvent
	if err := json.Unmarshal(message.Payload(), &event); err != nil {
		log.Printf(
			"MQTT INVALID JSON error=%v payload=%q",
			err,
			rawPayload,
		)
		return
	}

	log.Printf(
		"MQTT JSON OK eventId=%s deviceId=%s event=%s timestamp=%d imageBytes=%d rtspUrl=%s",
		event.EventID,
		event.DeviceID,
		event.Event,
		event.Timestamp,
		len(event.Image),
		event.RTSPURL,
	)

	if err := h.service.HandleDoorbellPress(context.Background(), event); err != nil {
		log.Printf("MQTT PROCESSING ERROR: %v", err)
	}
}

func (h *EventHandler) handleStreamEndMessage(_ mqtt.Client, message mqtt.Message) {
	rawPayload := string(message.Payload())

	log.Printf(
		"received MQTT message topic=%s qos=%d retained=%t payload=%q",
		message.Topic(),
		message.Qos(),
		message.Retained(),
		rawPayload,
	)

	var event domain.StreamEndEvent
	if err := json.Unmarshal(message.Payload(), &event); err != nil {
		log.Printf(
			"discarding invalid MQTT payload topic=%s error=%v payload=%q",
			message.Topic(),
			err,
			rawPayload,
		)
		return
	}

	if err := h.service.HandleStreamEnd(context.Background(), event); err != nil {
		log.Printf(
			"failed to process MQTT message topic=%s error=%v",
			message.Topic(),
			err,
		)
		return
	}

	log.Printf("successfully processed stream_end event")
}

func (h *EventHandler) handleCapture(mediaType domain.MediaType) mqtt.MessageHandler {
	return func(_ mqtt.Client, message mqtt.Message) {
		rawPayload := string(message.Payload())
		log.Printf("MQTT RECEIVED topic=%s payload=%q", message.Topic(), rawPayload)

		var event domain.CaptureEvent
		if err := json.Unmarshal(message.Payload(), &event); err != nil {
			log.Printf("MQTT INVALID JSON topic=%s error=%v payload=%q", message.Topic(), err, rawPayload)
			return
		}

		if err := h.service.HandleCapture(context.Background(), mediaType, event); err != nil {
			log.Printf("MQTT PROCESSING ERROR topic=%s error=%v", message.Topic(), err)
		}
	}
}

func (h *EventHandler) handleDeviceMedia(mediaType domain.MediaType) mqtt.MessageHandler {
	return func(_ mqtt.Client, message mqtt.Message) {
		rawPayload := string(message.Payload())
		log.Printf("MQTT RECEIVED topic=%s payloadLength=%d", message.Topic(), len(message.Payload()))

		var event domain.DeviceMediaEvent
		if err := json.Unmarshal(message.Payload(), &event); err != nil {
			log.Printf("MQTT INVALID JSON topic=%s error=%v payload=%q", message.Topic(), err, rawPayload)
			return
		}

		if err := h.service.HandleDeviceMedia(context.Background(), mediaType, event); err != nil {
			log.Printf("MQTT PROCESSING ERROR topic=%s error=%v", message.Topic(), err)
		}
	}
}

// PostEvent godoc
// @Summary      Report a security event
// @Description  Receives a security event from an IoT microcontroller and propagates it to the detection service
// @Tags         events
// @Accept       json
// @Produce      json
// @Param        X-Client-Cert-Subject  header    string               false  "Should send certs and extracted from GCP load balancer (only if mtls enabled)"
// @Param        X-Device-ID            header    string               false  "Device identifier (only if mtls disabled)"
// @Param        request        body      domain.EventRequest  true  "Event payload"
// @Success      201            {object}  domain.EventResponse
// @Failure      400            {object}  apierror.APIError
// @Failure      401            {object}  apierror.APIError
// @Failure      502            {object}  apierror.APIError
// @Failure      500            {object}  apierror.APIError
// @Security     BearerAuth
// @Router       /api/v1/events [post]
func (h *EventHandler) PostEvent(c *gin.Context) {
	var req domain.EventRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apierror.Respond(c, apierror.BadRequest(err.Error(), c.Request.URL.Path))
		return
	}

	resp, err := h.service.ProcessEvent(c.Request.Context(), &req)
	if err != nil {
		var detErr *domain.DetectorError
		if errors.As(err, &detErr) {
			apierror.Respond(c, apierror.FromDetectorError(detErr.DetectorDetail, c.Request.URL.Path, detErr.Code, detErr.StatusCode))
			return
		}
		apierror.Respond(c, apierror.InternalServerError("failed to process event", c.Request.URL.Path))
		return
	}

	c.JSON(http.StatusCreated, resp)
}
