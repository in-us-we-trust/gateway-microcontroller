package service

import (
	"context"

	"gateway-microcontroller/internal/client"
	"gateway-microcontroller/internal/domain"
)

type DeviceService interface {
	RegisterDevice(ctx context.Context, req *domain.DeviceRequest) (*domain.DeviceResponse, error)
}

type deviceService struct {
	detector client.DetectorClient
}

func NewDeviceService(detector client.DetectorClient) DeviceService {
	return &deviceService{detector: detector}
}

func (s *deviceService) RegisterDevice(ctx context.Context, req *domain.DeviceRequest) (*domain.DeviceResponse, error) {
	return s.detector.RegisterDevice(ctx, req)
}
