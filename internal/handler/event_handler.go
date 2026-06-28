package handler

import (
	"errors"
	"log"
	"net/http"

	"gateway-microcontroller/internal/domain"
	"gateway-microcontroller/internal/service"
	"gateway-microcontroller/pkg/apierror"

	"github.com/gin-gonic/gin"
)

type EventHandler struct {
	service service.EventService
}

func NewEventHandler(svc service.EventService) *EventHandler {
	return &EventHandler{service: svc}
}

// PostEvent godoc
// @Summary      Report a security event
// @Description  Receives a security event from an IoT microcontroller and propagates it to the detection service
// @Tags         events
// @Accept       json
// @Produce      json
// @Param        Authorization  header    string               true  "Bearer JWT token"
// @Param        request        body      domain.EventRequest  true  "Event payload"
// @Success      200            {object}  domain.EventResponse
// @Failure      400            {object}  apierror.APIError
// @Failure      401            {object}  apierror.APIError
// @Failure      502            {object}  apierror.APIError
// @Failure      500            {object}  apierror.APIError
// @Security     BearerAuth
// @Router       /api/v1/events [post]
func (h *EventHandler) PostEvent(c *gin.Context) {
	for k, v := range c.Request.Header {
		log.Printf("GCP Header [%s]: %v", k, v)
	}

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

	c.JSON(http.StatusOK, resp)
}
