package integration_test

import (
	"net/http"

	"gateway-microcontroller/internal/domain"
)

func GetDeviceSuccessResponseMock() *domain.DeviceResponse {
	return &domain.DeviceResponse{
		Data: domain.DeviceData{
			ID:                 "device-123",
			SerialNumber:       "SN-12345",
			City:               "Buenos Aires",
			LastSeen:           "2026-06-28T15:30:00Z",
			LastBoot:           "2026-06-28T10:00:00Z",
			Status:             "UNCLAIMED",
			Uptime:             3600,
			DetectionThreshold: 85,
			Location:           "Living",
			FirmwareVersion:    "1.2.3",
		},
	}
}

func GetDeviceErrorResponseMock() *domain.DevicesControllerError {
	return &domain.DevicesControllerError{
		StatusCode: http.StatusConflict,
		Body: []byte(`{
			"status": 409,
			"title": "Conflict",
			"detail": "Device with ID device-123 already exists",
			"instance": "/api/detective-manager/v1/devices",
			"type": "about:blank"
		}`),
	}
}
