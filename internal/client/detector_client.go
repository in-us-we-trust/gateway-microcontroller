package client

import (
	"context"

	"gateway-microcontroller/internal/domain"
)

type DetectorClient interface {
	Analyze(ctx context.Context, req *domain.EventRequest) (*domain.EventData, error)
	RegisterDevice(ctx context.Context, req *domain.DeviceRequest) (*domain.DeviceResponse, error)
	SendMediaEvent(ctx context.Context, req domain.MediaEventRequest) (*domain.OrchestratorEventData, error)
}

type DeviceCommander interface {
	SendInstruction(instruction domain.DeviceInstruction) error
}
