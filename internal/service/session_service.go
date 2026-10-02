package service

import (
	"context"
	"sync"
	"time"

	"gateway-microcontroller/internal/domain"
)

type SessionRegistry interface {
	StartSession(deviceID, trackID, rtspURL string) (domain.SessionState, context.Context)
	GetActiveSession(deviceID string) (domain.SessionState, bool)
	EndSession(deviceID, trackID string) bool
	// SessionContext devuelve el contexto de la sesion activa de ese track. Se
	// cancela con stream_end, STOP_DANGER, un press nuevo o el TTL.
	SessionContext(deviceID, trackID string) (context.Context, bool)
	Close()
}

type sessionEntry struct {
	state  domain.SessionState
	ctx    context.Context
	cancel context.CancelFunc
}

type sessionService struct {
	mu       sync.RWMutex
	sessions map[string]*sessionEntry
	ttl      time.Duration
	now      func() time.Time
}

func NewSessionService(ttl time.Duration) SessionRegistry {
	if ttl <= 0 {
		ttl = 30 * time.Second
	}

	return &sessionService{
		sessions: make(map[string]*sessionEntry),
		ttl:      ttl,
		now:      time.Now,
	}
}

func (s *sessionService) StartSession(deviceID, trackID, rtspURL string) (domain.SessionState, context.Context) {
	now := s.now()
	state := domain.SessionState{
		TrackID:   trackID,
		DeviceID:  deviceID,
		RTSPURL:   rtspURL,
		StartedAt: now,
		ExpiresAt: now.Add(s.ttl),
	}
	ctx, cancel := context.WithDeadline(context.Background(), state.ExpiresAt)
	entry := &sessionEntry{state: state, ctx: ctx, cancel: cancel}

	s.mu.Lock()
	previous := s.sessions[deviceID]
	s.sessions[deviceID] = entry
	s.mu.Unlock()

	if previous != nil {
		previous.cancel()
	}

	go s.removeWhenDone(deviceID, entry)

	return state, ctx
}

func (s *sessionService) GetActiveSession(deviceID string) (domain.SessionState, bool) {
	s.mu.RLock()
	entry, ok := s.sessions[deviceID]
	if ok && s.now().Before(entry.state.ExpiresAt) {
		state := entry.state
		s.mu.RUnlock()
		return state, true
	}
	s.mu.RUnlock()

	if ok {
		s.EndSession(deviceID, entry.state.TrackID)
	}

	return domain.SessionState{}, false
}

func (s *sessionService) SessionContext(deviceID, trackID string) (context.Context, bool) {
	s.mu.RLock()
	entry, ok := s.sessions[deviceID]
	s.mu.RUnlock()
	if !ok || entry.state.TrackID != trackID || !s.now().Before(entry.state.ExpiresAt) {
		return nil, false
	}
	return entry.ctx, true
}

func (s *sessionService) EndSession(deviceID, trackID string) bool {
	s.mu.Lock()
	entry, ok := s.sessions[deviceID]
	if !ok || (trackID != "" && entry.state.TrackID != trackID) {
		s.mu.Unlock()
		return false
	}
	delete(s.sessions, deviceID)
	s.mu.Unlock()

	entry.cancel()
	return true
}

func (s *sessionService) Close() {
	s.mu.Lock()
	entries := make([]*sessionEntry, 0, len(s.sessions))
	for deviceID, entry := range s.sessions {
		entries = append(entries, entry)
		delete(s.sessions, deviceID)
	}
	s.mu.Unlock()

	for _, entry := range entries {
		entry.cancel()
	}
}

func (s *sessionService) removeWhenDone(deviceID string, entry *sessionEntry) {
	<-entry.ctx.Done()

	s.mu.Lock()
	current, ok := s.sessions[deviceID]
	if ok && current == entry {
		delete(s.sessions, deviceID)
	}
	s.mu.Unlock()
}
