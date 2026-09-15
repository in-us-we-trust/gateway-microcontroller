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

type EventHandler struct {
	service    service.EventService
	dispatcher *pressDispatcher
}

func NewEventHandler(svc service.EventService) *EventHandler {
	return NewEventHandlerWithPressLimit(svc, defaultPressMaxInflight)
}

func NewEventHandlerWithPressLimit(svc service.EventService, maxInflight int) *EventHandler {
	return &EventHandler{
		service:    svc,
		dispatcher: newPressDispatcher(svc, maxInflight),
	}
}

func (h *EventHandler) Close() {
	if h.dispatcher != nil {
		h.dispatcher.Close()
	}
}

func (h *EventHandler) RegisterMQTTSubscriptions(client mqtt.Client) error {
	token := client.Subscribe(domain.MQTTHomeEventsWildcard, 1, h.handleHomeEventMessage)
	if !token.WaitTimeout(10 * time.Second) {
		return fmt.Errorf("subscribe to MQTT topic %s: timeout", domain.MQTTHomeEventsWildcard)
	}
	if err := token.Error(); err != nil {
		return fmt.Errorf("subscribe to MQTT topic %s: %w", domain.MQTTHomeEventsWildcard, err)
	}
	log.Printf("subscribed to MQTT topic %s with QoS 1", domain.MQTTHomeEventsWildcard)
	return nil
}

func (h *EventHandler) handleHomeEventMessage(_ mqtt.Client, message mqtt.Message) {
	rawPayload := string(message.Payload())
	loggedPayload := rawPayload
	if len(loggedPayload) > 240 {
		loggedPayload = loggedPayload[:120] + "... (" + fmt.Sprintf("%d", len(rawPayload)) + " chars)"
	}

	parsed, err := domain.ParseHomeEventTopic(message.Topic())
	if err != nil {
		log.Printf("discarding MQTT message topic=%s error=%v", message.Topic(), err)
		return
	}

	switch parsed.Action {
	case domain.MQTTActionPress:
		h.handleDoorbellPressMessage(message, parsed, loggedPayload)
	case domain.MQTTActionStreamEnd:
		h.handleStreamEndMessage(message, parsed, rawPayload)
	case domain.MQTTActionCaptureImage:
		h.handleCaptureMessage(domain.MediaTypeImage, message, parsed, rawPayload)
	case domain.MQTTActionCaptureAudio:
		h.handleCaptureMessage(domain.MediaTypeAudio, message, parsed, rawPayload)
	case domain.MQTTActionAIResponse:
		return
	default:
		log.Printf("ignoring MQTT topic=%s action=%s", message.Topic(), parsed.Action)
	}
}

func (h *EventHandler) handleDoorbellPressMessage(message mqtt.Message, topic domain.HomeEventTopic, loggedPayload string) {
	log.Printf("MQTT RECEIVED topic=%s payload=%q", message.Topic(), loggedPayload)

	var event domain.DoorbellPressEvent
	if err := json.Unmarshal(message.Payload(), &event); err != nil {
		log.Printf("MQTT INVALID JSON error=%v payload=%q", err, string(message.Payload()))
		return
	}
	event.DeviceID = topic.DeviceID
	event.EventID = topic.TrackID

	log.Printf(
		"MQTT JSON OK eventId=%s deviceId=%s event=%s timestamp=%d imageBytes=%d",
		event.EventID,
		event.DeviceID,
		event.Event,
		event.Timestamp,
		len(event.Image),
	)

	h.dispatcher.Submit(event)
}

func (h *EventHandler) handleStreamEndMessage(message mqtt.Message, topic domain.HomeEventTopic, rawPayload string) {
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
	event.DeviceID = topic.DeviceID
	event.TrackID = topic.TrackID

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

func (h *EventHandler) handleCaptureMessage(mediaType domain.MediaType, message mqtt.Message, topic domain.HomeEventTopic, rawPayload string) {
	log.Printf("MQTT RECEIVED topic=%s payload=%q", message.Topic(), rawPayload)

	var event domain.CaptureEvent
	if err := json.Unmarshal(message.Payload(), &event); err != nil {
		log.Printf("MQTT INVALID JSON topic=%s error=%v payload=%q", message.Topic(), err, rawPayload)
		return
	}
	event.DeviceID = topic.DeviceID
	event.TrackID = topic.TrackID

	if err := h.service.HandleCapture(context.Background(), mediaType, event); err != nil {
		log.Printf("MQTT PROCESSING ERROR topic=%s error=%v", message.Topic(), err)
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
