package service

import (
	"context"
	"encoding/base64"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"gateway-microcontroller/internal/domain"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type recordingDetectorClient struct {
	mediaEvents chan domain.MediaEventRequest
}

func (c *recordingDetectorClient) Analyze(
	context.Context,
	*domain.EventRequest,
) (*domain.EventData, error) {
	return nil, nil
}

func (c *recordingDetectorClient) RegisterDevice(
	context.Context,
	*domain.DeviceRequest,
) (*domain.DeviceResponse, error) {
	return nil, nil
}

func (c *recordingDetectorClient) SendMediaEvent(_ context.Context, req domain.MediaEventRequest) (*domain.OrchestratorEventData, error) {
	c.mediaEvents <- req
	return &domain.OrchestratorEventData{
		TrackID:   req.TrackID,
		ChunkType: string(req.MediaType),
		Assessments: []domain.OrchestratorAssessment{
			{Status: "SAFE", Severity: "LOW", Message: "No danger.", Verdict: domain.DeviceActionContinue},
		},
	}, nil
}

type stubMediaExtractor struct {
	mu         sync.Mutex
	audioCalls int
}

func (e *stubMediaExtractor) ExtractImage(context.Context, string) ([]byte, error) {
	return []byte("image"), nil
}

func (e *stubMediaExtractor) ExtractImageFromFile(context.Context, string, []byte) ([]byte, error) {
	return []byte("image-from-video"), nil
}

func (e *stubMediaExtractor) ExtractAudioChunk(
	ctx context.Context,
	_ string,
	_ time.Duration,
	_ string,
) ([]byte, error) {
	e.mu.Lock()
	e.audioCalls++
	call := e.audioCalls
	e.mu.Unlock()

	if call == 1 {
		return []byte("audio"), nil
	}

	<-ctx.Done()
	return nil, ctx.Err()
}

func (e *stubMediaExtractor) ExtractVideoChunk(
	ctx context.Context,
	_ string,
	_ time.Duration,
) ([]byte, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(5 * time.Millisecond):
		return []byte("video"), nil
	}
}

func TestEventService_ReusesTrackIDForImageAndAudio(t *testing.T) {
	t.Parallel()

	detector := &recordingDetectorClient{mediaEvents: make(chan domain.MediaEventRequest, 8)}
	sessions := NewSessionService(time.Minute)
	eventService := NewGatewayEventService(
		detector,
		sessions,
		&stubMediaExtractor{},
		EventServiceConfig{
			AudioChunkDuration:  10 * time.Millisecond,
			AudioFormat:         "mp3",
			MediaRequestTimeout: 100 * time.Millisecond,
		},
	)

	snapshot := []byte("press-jpeg")
	err := eventService.HandleDoorbellPress(context.Background(), domain.DoorbellPressEvent{
		EventID:  "track-123",
		DeviceID: "device-abc",
		Event:    "press",
		Image:    base64.StdEncoding.EncodeToString(snapshot),
		RTSPURL:  "rtsp://camera/stream",
	})
	require.NoError(t, err)

	received := make(map[domain.MediaType]domain.MediaEventRequest)
	for len(received) < 3 {
		select {
		case mediaEvent := <-detector.mediaEvents:
			received[mediaEvent.MediaType] = mediaEvent
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for media events")
		}
	}

	for _, mediaType := range []domain.MediaType{domain.MediaTypeImage, domain.MediaTypeAudio, domain.MediaTypeVideo} {
		assert.Equal(t, "track-123", received[mediaType].TrackID)
		assert.Equal(t, "device-abc", received[mediaType].DeviceID)
	}
	assert.Equal(t, snapshot, received[domain.MediaTypeImage].Buffer)

	require.NoError(t, eventService.HandleStreamEnd(context.Background(), domain.StreamEndEvent{
		DeviceID: "device-abc",
		TrackID:  "track-123",
	}))
	eventService.Close()

	_, active := sessions.GetActiveSession("device-abc")
	assert.False(t, active)
}

func TestEventService_InterleavesVideoThenAudio(t *testing.T) {
	t.Parallel()

	detector := &recordingDetectorClient{mediaEvents: make(chan domain.MediaEventRequest, 8)}
	sessions := NewSessionService(time.Minute)
	eventService := NewGatewayEventService(
		detector,
		sessions,
		&stubMediaExtractor{},
		EventServiceConfig{
			AudioChunkDuration:  10 * time.Millisecond,
			VideoChunkDuration:  10 * time.Millisecond,
			AudioFormat:         "wav",
			MediaRequestTimeout: 100 * time.Millisecond,
		},
	)

	err := eventService.HandleDoorbellPress(context.Background(), domain.DoorbellPressEvent{
		EventID:  "track-seq",
		DeviceID: "device-seq",
		Event:    "press",
		Image:    base64.StdEncoding.EncodeToString([]byte("press-jpeg")),
		RTSPURL:  "rtsp://camera/stream",
	})
	require.NoError(t, err)

	var order []domain.MediaType
	for len(order) < 4 {
		select {
		case mediaEvent := <-detector.mediaEvents:
			order = append(order, mediaEvent.MediaType)
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for interleaved chunks, got %v", order)
		}
	}

	assert.Equal(t, []domain.MediaType{
		domain.MediaTypeImage,
		domain.MediaTypeVideo,
		domain.MediaTypeAudio,
		domain.MediaTypeVideo,
	}, order)

	require.NoError(t, eventService.HandleStreamEnd(context.Background(), domain.StreamEndEvent{
		DeviceID: "device-seq",
		TrackID:  "track-seq",
	}))
	eventService.Close()
}

func TestEventService_WritesDebugChunks(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	detector := &recordingDetectorClient{mediaEvents: make(chan domain.MediaEventRequest, 8)}
	sessions := NewSessionService(time.Minute)
	eventService := NewGatewayEventService(
		detector,
		sessions,
		&stubMediaExtractor{},
		EventServiceConfig{
			AudioChunkDuration:  10 * time.Millisecond,
			VideoChunkDuration:  10 * time.Millisecond,
			AudioFormat:         "wav",
			MediaRequestTimeout: 100 * time.Millisecond,
			DebugMedia:          NewDebugMediaWriter(true, dir, 20),
		},
	)

	err := eventService.HandleDoorbellPress(context.Background(), domain.DoorbellPressEvent{
		EventID:  "123",
		DeviceID: "device-abc",
		Event:    "press",
		Image:    base64.StdEncoding.EncodeToString([]byte("press-jpeg")),
		RTSPURL:  "rtsp://camera/stream",
	})
	require.NoError(t, err)

	deadline := time.Now().Add(time.Second)
	var files []string
	for time.Now().Before(deadline) {
		entries, readErr := os.ReadDir(dir)
		require.NoError(t, readErr)
		files = files[:0]
		for _, entry := range entries {
			files = append(files, entry.Name())
		}
		if len(files) >= 3 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	require.NoError(t, eventService.HandleStreamEnd(context.Background(), domain.StreamEndEvent{
		DeviceID: "device-abc",
		TrackID:  "123",
	}))
	eventService.Close()

	joined := strings.Join(files, " ")
	assert.Contains(t, joined, "event-123_")
	assert.Contains(t, joined, "_image_chunk-001.jpg")
	assert.Contains(t, joined, "_audio_chunk-001.wav")
	assert.Contains(t, joined, "_video_chunk-001.mp4")
}
