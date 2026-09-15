package handler

import (
	"context"
	"sync"
	"testing"
	"time"

	"gateway-microcontroller/internal/client"
	"gateway-microcontroller/internal/domain"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type dispatchPressService struct {
	mu          sync.Mutex
	eventIDs    []string
	deviceIDs   []string
	inFlight    int
	maxInFlight int
	blockFirst  chan struct{}
	firstBegun  chan struct{}
}

func (s *dispatchPressService) ProcessEvent(context.Context, *domain.EventRequest) (*domain.EventResponse, error) {
	return nil, nil
}
func (s *dispatchPressService) HandleDoorbellPress(_ context.Context, event domain.DoorbellPressEvent) error {
	s.mu.Lock()
	s.inFlight++
	if s.inFlight > s.maxInFlight {
		s.maxInFlight = s.inFlight
	}
	isFirst := len(s.eventIDs) == 0
	s.eventIDs = append(s.eventIDs, event.EventID)
	s.deviceIDs = append(s.deviceIDs, event.DeviceID)
	s.mu.Unlock()

	if isFirst && s.blockFirst != nil {
		if s.firstBegun != nil {
			close(s.firstBegun)
		}
		<-s.blockFirst
	} else {
		time.Sleep(40 * time.Millisecond)
	}

	s.mu.Lock()
	s.inFlight--
	s.mu.Unlock()
	return nil
}
func (s *dispatchPressService) HandleStreamEnd(context.Context, domain.StreamEndEvent) error {
	return nil
}
func (s *dispatchPressService) HandleCapture(context.Context, domain.MediaType, domain.CaptureEvent) error {
	return nil
}
func (s *dispatchPressService) HandleDeviceMedia(context.Context, domain.MediaType, domain.DeviceMediaEvent) error {
	return nil
}
func (s *dispatchPressService) SetDeviceCommander(client.DeviceCommander) {}
func (s *dispatchPressService) Close()                                    {}

func (s *dispatchPressService) snapshot() (ids []string, devices []string, max int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ids = append([]string(nil), s.eventIDs...)
	devices = append([]string(nil), s.deviceIDs...)
	return ids, devices, s.maxInFlight
}

func TestPressDispatcher_TwoDevicesRunInParallel(t *testing.T) {
	t.Parallel()

	svc := &dispatchPressService{}
	d := newPressDispatcher(svc, 8)
	defer d.Close()

	d.Submit(domain.DoorbellPressEvent{DeviceID: "device-a", EventID: "track-a"})
	d.Submit(domain.DoorbellPressEvent{DeviceID: "device-b", EventID: "track-b"})
	d.Close()

	ids, devices, max := svc.snapshot()
	assert.ElementsMatch(t, []string{"track-a", "track-b"}, ids)
	assert.ElementsMatch(t, []string{"device-a", "device-b"}, devices)
	assert.GreaterOrEqual(t, max, 2)
}

func TestPressDispatcher_SameDeviceKeepsLatestPending(t *testing.T) {
	t.Parallel()

	svc := &dispatchPressService{
		blockFirst: make(chan struct{}),
		firstBegun: make(chan struct{}),
	}
	d := newPressDispatcher(svc, 8)

	d.Submit(domain.DoorbellPressEvent{DeviceID: "device-a", EventID: "track-1"})
	select {
	case <-svc.firstBegun:
	case <-time.After(time.Second):
		t.Fatal("first press did not start")
	}
	d.Submit(domain.DoorbellPressEvent{DeviceID: "device-a", EventID: "track-2"})
	d.Submit(domain.DoorbellPressEvent{DeviceID: "device-a", EventID: "track-3"})
	close(svc.blockFirst)
	d.Close()

	ids, _, max := svc.snapshot()
	require.Equal(t, []string{"track-1", "track-3"}, ids)
	assert.Equal(t, 1, max)
}

func TestPressDispatcher_GlobalInflightCap(t *testing.T) {
	t.Parallel()

	svc := &dispatchPressService{}
	d := newPressDispatcher(svc, 1)
	d.Submit(domain.DoorbellPressEvent{DeviceID: "device-a", EventID: "track-a"})
	d.Submit(domain.DoorbellPressEvent{DeviceID: "device-b", EventID: "track-b"})
	d.Close()

	_, _, max := svc.snapshot()
	assert.Equal(t, 1, max)
}
