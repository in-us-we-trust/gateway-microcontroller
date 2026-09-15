package service

import (
	"context"
	"testing"

	"gateway-microcontroller/internal/domain"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeviceService_RegisterDeviceForwardsToDetector(t *testing.T) {
	t.Parallel()

	detector := &recordingDetectorClient{mediaEvents: make(chan domain.MediaEventRequest, 1)}
	svc := NewDeviceService(detector)

	resp, err := svc.RegisterDevice(context.Background(), &domain.DeviceRequest{
		ID:           "device-1",
		SerialNumber: "SN-1",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "device-1", resp.Data.ID)
}
