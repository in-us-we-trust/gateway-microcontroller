package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSessionService_StartGetAndEnd(t *testing.T) {
	t.Parallel()

	sessions := NewSessionService(time.Minute)
	t.Cleanup(sessions.Close)

	started, _ := sessions.StartSession("device-1", "track-1", "rtsp://camera/stream")
	active, ok := sessions.GetActiveSession("device-1")

	require.True(t, ok)
	assert.Equal(t, started, active)
	assert.True(t, sessions.EndSession("device-1", "track-1"))

	_, ok = sessions.GetActiveSession("device-1")
	assert.False(t, ok)
}

func TestSessionService_ExpiresSessionAfterTTL(t *testing.T) {
	t.Parallel()

	sessions := NewSessionService(20 * time.Millisecond)
	t.Cleanup(sessions.Close)

	_, sessionContext := sessions.StartSession("device-1", "track-1", "rtsp://camera/stream")

	select {
	case <-sessionContext.Done():
	case <-time.After(time.Second):
		t.Fatal("session did not expire")
	}

	require.Eventually(t, func() bool {
		_, ok := sessions.GetActiveSession("device-1")
		return !ok
	}, time.Second, 5*time.Millisecond)
}

func TestSessionService_StaleEndDoesNotCloseReplacementSession(t *testing.T) {
	t.Parallel()

	sessions := NewSessionService(time.Minute)
	t.Cleanup(sessions.Close)

	_, firstContext := sessions.StartSession("device-1", "track-1", "rtsp://camera/first")
	sessions.StartSession("device-1", "track-2", "rtsp://camera/second")

	require.Eventually(t, func() bool {
		return firstContext.Err() != nil
	}, time.Second, 5*time.Millisecond)
	assert.False(t, sessions.EndSession("device-1", "track-1"))

	active, ok := sessions.GetActiveSession("device-1")
	require.True(t, ok)
	assert.Equal(t, "track-2", active.TrackID)
}

func TestSessionService_KeepsIndependentDeviceSessions(t *testing.T) {
	t.Parallel()

	sessions := NewSessionService(time.Minute)
	t.Cleanup(sessions.Close)

	sessions.StartSession("device-a", "track-a", "rtsp://mtx/a/doorbell/track-a")
	sessions.StartSession("device-b", "track-b", "rtsp://mtx/b/doorbell/track-b")

	activeA, okA := sessions.GetActiveSession("device-a")
	activeB, okB := sessions.GetActiveSession("device-b")
	require.True(t, okA)
	require.True(t, okB)
	assert.Equal(t, "track-a", activeA.TrackID)
	assert.Equal(t, "track-b", activeB.TrackID)

	assert.True(t, sessions.EndSession("device-a", "track-a"))
	_, okA = sessions.GetActiveSession("device-a")
	_, okB = sessions.GetActiveSession("device-b")
	assert.False(t, okA)
	assert.True(t, okB)
}
