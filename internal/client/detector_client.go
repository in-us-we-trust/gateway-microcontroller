package client

import (
	"context"

	"gateway-microcontroller/internal/domain"
)

type DetectorClient interface {
	Analyze(ctx context.Context, req *domain.EventRequest) (*domain.EventData, error)
	RegisterDevice(ctx context.Context, req *domain.DeviceRequest) (*domain.DeviceResponse, error)
}
