package service

import (
	"context"

	"gateway-microcontroller/internal/client"
	"gateway-microcontroller/internal/domain"
)

type EventService interface {
	ProcessEvent(ctx context.Context, req *domain.EventRequest) (*domain.EventResponse, error)
}

type eventService struct {
	detector client.DetectorClient
}

func NewEventService(detector client.DetectorClient) EventService {
	return &eventService{detector: detector}
}

func (s *eventService) ProcessEvent(ctx context.Context, req *domain.EventRequest) (*domain.EventResponse, error) {
	data, err := s.detector.Analyze(ctx, req)
	if err != nil {
		return nil, err
	}

	return &domain.EventResponse{Data: *data}, nil
}
