package testutil

import (
	"context"

	"gateway-microcontroller/internal/client"
	"gateway-microcontroller/internal/domain"

	"github.com/stretchr/testify/mock"
)

type MockDetectorClient struct {
	mock.Mock
}

func (m *MockDetectorClient) Analyze(ctx context.Context, req *domain.EventRequest) (*domain.EventData, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.EventData), args.Error(1)
}

var _ client.DetectorClient = (*MockDetectorClient)(nil)

type MockAuthClient struct {
	mock.Mock
}

func (m *MockAuthClient) ValidateDevice(ctx context.Context, deviceID, serial string) (bool, error) {
	args := m.Called(ctx, deviceID, serial)
	return args.Bool(0), args.Error(1)
}

var _ client.AuthClient = (*MockAuthClient)(nil)
