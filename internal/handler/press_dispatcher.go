package handler

import (
	"context"
	"log"
	"strings"
	"sync"

	"gateway-microcontroller/internal/domain"
	"gateway-microcontroller/internal/service"
)

const defaultPressMaxInflight = 8

type pressDispatcher struct {
	svc      service.EventService
	inflight chan struct{}

	mu     sync.Mutex
	slots  map[string]*devicePressSlot
	closed bool
	wg     sync.WaitGroup
}

type devicePressSlot struct {
	pending *domain.DoorbellPressEvent
	running bool
}

func newPressDispatcher(svc service.EventService, maxInflight int) *pressDispatcher {
	if maxInflight <= 0 {
		maxInflight = defaultPressMaxInflight
	}
	return &pressDispatcher{
		svc:      svc,
		inflight: make(chan struct{}, maxInflight),
		slots:    make(map[string]*devicePressSlot),
	}
}

func (d *pressDispatcher) Submit(event domain.DoorbellPressEvent) {
	deviceID := strings.TrimSpace(event.DeviceID)
	if deviceID == "" {
		return
	}
	event.DeviceID = deviceID

	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return
	}

	slot := d.slots[deviceID]
	if slot == nil {
		slot = &devicePressSlot{}
		d.slots[deviceID] = slot
	}
	if slot.running {
		copyEvent := event
		slot.pending = &copyEvent
		return
	}

	slot.running = true
	d.wg.Add(1)
	go d.run(deviceID, event)
}

func (d *pressDispatcher) run(deviceID string, event domain.DoorbellPressEvent) {
	defer d.wg.Done()

	for {
		d.inflight <- struct{}{}
		func() {
			defer func() { <-d.inflight }()
			defer func() {
				if recovered := recover(); recovered != nil {
					log.Printf("press worker panic device=%s: %v", deviceID, recovered)
				}
			}()
			if err := d.svc.HandleDoorbellPress(context.Background(), event); err != nil {
				log.Printf("MQTT PROCESSING ERROR: %v", err)
			}
		}()

		d.mu.Lock()
		slot := d.slots[deviceID]
		if slot == nil {
			d.mu.Unlock()
			return
		}
		if slot.pending != nil {
			event = *slot.pending
			slot.pending = nil
			d.mu.Unlock()
			continue
		}
		slot.running = false
		delete(d.slots, deviceID)
		d.mu.Unlock()
		return
	}
}

func (d *pressDispatcher) Close() {
	d.mu.Lock()
	d.closed = true
	d.mu.Unlock()
	d.wg.Wait()
}
