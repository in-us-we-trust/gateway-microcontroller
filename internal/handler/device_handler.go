package handler

import (
	"errors"
	"fmt"
	"net/http"

	"gateway-microcontroller/internal/domain"
	"gateway-microcontroller/internal/requestctx"
	"gateway-microcontroller/internal/service"
	"gateway-microcontroller/pkg/apierror"

	"github.com/gin-gonic/gin"
)

type DeviceHandler struct {
	service service.DeviceService
}

func NewDeviceHandler(svc service.DeviceService) *DeviceHandler {
	return &DeviceHandler{service: svc}
}

// PostDevice godoc
// @Summary      Register a new device
// @Description  Registers a new IoT device with the devices controller
// @Tags         devices
// @Accept       json
// @Produce      json
// @Param        X-Client-Cert-Subject  header    string                              false  "Should send certs and extracted from GCP load balancer (only if mtls enabled)"
// @Param        X-Device-ID            header    string                              false  "Device identifier (only if mtls disabled)"
// @Param        request                body      domain.DeviceRequest                true   "Device registration payload"
// @Success      201                    {object}  domain.DeviceResponse
// @Failure      400                    {object}  apierror.APIError
// @Failure      401                    {object}  apierror.APIError
// @Failure      502                    {object}  apierror.APIError
// @Failure      500                    {object}  apierror.APIError
// @Security     BearerAuth
// @Router       /devices [post]
func (h *DeviceHandler) PostDevice(c *gin.Context) {
	deviceID, ok := requestctx.DeviceID(c.Request.Context())
	if !ok {
		apierror.Respond(c, apierror.Unauthorized(fmt.Sprintf("device ID not found in context for request to %s", c.Request.URL.Path), c.Request.URL.Path))
		return
	}

	var req domain.DeviceRequest
	if err := c.BindJSON(&req); err != nil {
		apierror.Respond(c, apierror.BadRequest(err.Error(), c.Request.URL.Path))
		return
	}

	req.ID = deviceID

	resp, err := h.service.RegisterDevice(c.Request.Context(), &req)
	if err != nil {
		var devErr *domain.DevicesControllerError
		if errors.As(err, &devErr) {
			c.Data(devErr.StatusCode, "application/json", devErr.Body)
			return
		}
		apierror.Respond(c, apierror.InternalServerError("failed to register device", c.Request.URL.Path))
		return
	}

	c.JSON(http.StatusCreated, resp)
}
