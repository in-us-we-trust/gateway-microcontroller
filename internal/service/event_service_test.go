package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
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
	return &domain.EventData{EventID: "legacy-event"}, nil
}

func (c *recordingDetectorClient) RegisterDevice(
	_ context.Context,
	req *domain.DeviceRequest,
) (*domain.DeviceResponse, error) {
	id := ""
	if req != nil {
		id = req.ID
	}
	return &domain.DeviceResponse{Data: domain.DeviceData{ID: id}}, nil
}

func (c *recordingDetectorClient) SendMediaEvent(ctx context.Context, req domain.MediaEventRequest) (*domain.OrchestratorEventData, error) {
	select {
	case c.mediaEvents <- req:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return continueOrchestratorResponse(req), nil
}

type delayedAudioDetectorClient struct {
	recordingDetectorClient
	audioDelay time.Duration
}

func (c *delayedAudioDetectorClient) SendMediaEvent(ctx context.Context, req domain.MediaEventRequest) (*domain.OrchestratorEventData, error) {
	if req.MediaType == domain.MediaTypeAudio && c.audioDelay > 0 {
		timer := time.NewTimer(c.audioDelay)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return c.recordingDetectorClient.SendMediaEvent(ctx, req)
}

type stopDangerDetectorClient struct {
	mediaEvents chan domain.MediaEventRequest
	stopOn      domain.MediaType
}

func (c *stopDangerDetectorClient) Analyze(context.Context, *domain.EventRequest) (*domain.EventData, error) {
	return nil, nil
}

func (c *stopDangerDetectorClient) RegisterDevice(context.Context, *domain.DeviceRequest) (*domain.DeviceResponse, error) {
	return nil, nil
}

func (c *stopDangerDetectorClient) SendMediaEvent(ctx context.Context, req domain.MediaEventRequest) (*domain.OrchestratorEventData, error) {
	select {
	case c.mediaEvents <- req:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if req.MediaType == c.stopOn {
		return &domain.OrchestratorEventData{
			TrackID:       req.TrackID,
			ChunkType:     string(req.MediaType),
			FinalVerdict:  domain.DeviceActionStopDanger,
			FinalSeverity: "HIGH",
			Message:       "Stop.",
		}, nil
	}
	return continueOrchestratorResponse(req), nil
}

func continueOrchestratorResponse(req domain.MediaEventRequest) *domain.OrchestratorEventData {
	return &domain.OrchestratorEventData{
		TrackID:       req.TrackID,
		ChunkType:     string(req.MediaType),
		FinalVerdict:  domain.DeviceActionContinue,
		FinalSeverity: "LOW",
		Message:       "No danger.",
	}
}

type orchestratorTrackOverrideClient struct {
	recordingDetectorClient
	trackID string
}

func (c *orchestratorTrackOverrideClient) SendMediaEvent(ctx context.Context, req domain.MediaEventRequest) (*domain.OrchestratorEventData, error) {
	data, err := c.recordingDetectorClient.SendMediaEvent(ctx, req)
	if err != nil {
		return nil, err
	}
	data.TrackID = c.trackID
	return data, nil
}

type stubMediaExtractor struct {
	mu         sync.Mutex
	audioCalls int
	videoCalls int
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
	e.mu.Lock()
	e.videoCalls++
	e.mu.Unlock()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(5 * time.Millisecond):
		return []byte("video"), nil
	}
}

func TestEventService_ReusesTrackIDForImageAndAudio(t *testing.T) {
	t.Parallel()

	detector := &recordingDetectorClient{mediaEvents: make(chan domain.MediaEventRequest, 32)}
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

func TestEventService_DispatchesImageVideoAndAudio(t *testing.T) {
	t.Parallel()

	detector := &recordingDetectorClient{mediaEvents: make(chan domain.MediaEventRequest, 32)}
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
	seen := map[domain.MediaType]bool{}
	for !seen[domain.MediaTypeImage] || !seen[domain.MediaTypeVideo] || !seen[domain.MediaTypeAudio] {
		select {
		case mediaEvent := <-detector.mediaEvents:
			order = append(order, mediaEvent.MediaType)
			seen[mediaEvent.MediaType] = true
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for image, video and audio, got %v", order)
		}
	}

	// El snapshot se envia en paralelo al streaming: no hay orden garantizado.
	assert.True(t, seen[domain.MediaTypeImage])
	assert.True(t, seen[domain.MediaTypeVideo])
	assert.True(t, seen[domain.MediaTypeAudio])

	require.NoError(t, eventService.HandleStreamEnd(context.Background(), domain.StreamEndEvent{
		DeviceID: "device-seq",
		TrackID:  "track-seq",
	}))
	eventService.Close()
}

func TestEventService_KeepsExtractingVideoWhileAudioPostIsSlow(t *testing.T) {
	t.Parallel()

	detector := &delayedAudioDetectorClient{
		recordingDetectorClient: recordingDetectorClient{mediaEvents: make(chan domain.MediaEventRequest, 64)},
		audioDelay:              200 * time.Millisecond,
	}
	sessions := NewSessionService(time.Minute)
	eventService := NewGatewayEventService(
		detector,
		sessions,
		&stubMediaExtractor{},
		EventServiceConfig{
			AudioChunkDuration:  10 * time.Millisecond,
			VideoChunkDuration:  10 * time.Millisecond,
			AudioFormat:         "wav",
			MediaRequestTimeout: 500 * time.Millisecond,
		},
	)

	err := eventService.HandleDoorbellPress(context.Background(), domain.DoorbellPressEvent{
		EventID:  "track-parallel",
		DeviceID: "device-parallel",
		Event:    "press",
		Image:    base64.StdEncoding.EncodeToString([]byte("press-jpeg")),
		RTSPURL:  "rtsp://camera/stream",
	})
	require.NoError(t, err)

	var events []domain.MediaEventRequest
	gotAudio := false
	deadline := time.After(2 * time.Second)
	for !gotAudio {
		select {
		case mediaEvent := <-detector.mediaEvents:
			events = append(events, mediaEvent)
			if mediaEvent.MediaType == domain.MediaTypeAudio {
				gotAudio = true
			}
		case <-deadline:
			t.Fatalf("timed out waiting for audio while video continued, got %v", mediaTypes(events))
		}
	}

	videos := 0
	for _, mediaEvent := range events {
		if mediaEvent.MediaType == domain.MediaTypeVideo {
			videos++
		}
	}
	assert.GreaterOrEqual(t, videos, 2, "expected video chunks during the slow audio POST, got %v", mediaTypes(events))

	require.NoError(t, eventService.HandleStreamEnd(context.Background(), domain.StreamEndEvent{
		DeviceID: "device-parallel",
		TrackID:  "track-parallel",
	}))
	eventService.Close()
}

func TestEventService_KeepsExtractingVideoWhileVideoPostIsSlow(t *testing.T) {
	t.Parallel()

	extractor := &stubMediaExtractor{}
	detector := &delayedAudioDetectorClient{
		recordingDetectorClient: recordingDetectorClient{mediaEvents: make(chan domain.MediaEventRequest, 64)},
		audioDelay:              300 * time.Millisecond,
	}
	sessions := NewSessionService(time.Minute)
	eventService := NewGatewayEventService(
		&videoDelayDetector{delayedAudioDetectorClient: detector},
		sessions,
		extractor,
		EventServiceConfig{
			AudioChunkDuration:  10 * time.Millisecond,
			VideoChunkDuration:  10 * time.Millisecond,
			AudioFormat:         "wav",
			MediaRequestTimeout: time.Second,
		},
	)

	err := eventService.HandleDoorbellPress(context.Background(), domain.DoorbellPressEvent{
		EventID:  "track-video-queue",
		DeviceID: "device-video-queue",
		Event:    "press",
		Image:    base64.StdEncoding.EncodeToString([]byte("press-jpeg")),
		RTSPURL:  "rtsp://camera/stream",
	})
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		extractor.mu.Lock()
		defer extractor.mu.Unlock()
		return extractor.videoCalls >= 2
	}, time.Second, 5*time.Millisecond)

	require.NoError(t, eventService.HandleStreamEnd(context.Background(), domain.StreamEndEvent{
		DeviceID: "device-video-queue",
		TrackID:  "track-video-queue",
	}))
	eventService.Close()
}

type videoDelayDetector struct {
	*delayedAudioDetectorClient
}

func (c *videoDelayDetector) SendMediaEvent(ctx context.Context, req domain.MediaEventRequest) (*domain.OrchestratorEventData, error) {
	if req.MediaType == domain.MediaTypeVideo && c.audioDelay > 0 {
		timer := time.NewTimer(c.audioDelay)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return c.recordingDetectorClient.SendMediaEvent(ctx, req)
}

func TestPostQueueDepthCoversOneSlowPost(t *testing.T) {
	t.Parallel()

	assert.Equal(t, 3, postQueueDepth(5*time.Second, 15*time.Second))
	assert.Equal(t, 2, postQueueDepth(10*time.Second, 15*time.Second))
	assert.Equal(t, 1, postQueueDepth(30*time.Second, 15*time.Second))
	assert.Equal(t, 1, postQueueDepth(0, 15*time.Second))
}

func TestEventService_StopDangerEndsBothWorkers(t *testing.T) {
	t.Parallel()

	detector := &stopDangerDetectorClient{
		mediaEvents: make(chan domain.MediaEventRequest, 32),
		stopOn:      domain.MediaTypeAudio,
	}
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
		EventID:  "track-stop",
		DeviceID: "device-stop",
		Event:    "press",
		Image:    base64.StdEncoding.EncodeToString([]byte("press-jpeg")),
		RTSPURL:  "rtsp://camera/stream",
	})
	require.NoError(t, err)

	gotAudio := false
	deadline := time.After(time.Second)
	for !gotAudio {
		select {
		case mediaEvent := <-detector.mediaEvents:
			if mediaEvent.MediaType == domain.MediaTypeAudio {
				gotAudio = true
			}
		case <-deadline:
			t.Fatal("timed out waiting for STOP_DANGER audio chunk")
		}
	}

	require.Eventually(t, func() bool {
		_, active := sessions.GetActiveSession("device-stop")
		return !active
	}, time.Second, 5*time.Millisecond)

	drainUntil := time.Now().Add(80 * time.Millisecond)
	for time.Now().Before(drainUntil) {
		select {
		case <-detector.mediaEvents:
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}

	select {
	case mediaEvent := <-detector.mediaEvents:
		t.Fatalf("worker kept dispatching after STOP_DANGER: %s", mediaEvent.MediaType)
	case <-time.After(150 * time.Millisecond):
	}

	eventService.Close()
}

func TestEventService_ImageStopDangerEndsSessionAndStopsStreaming(t *testing.T) {
	t.Parallel()

	detector := &stopDangerDetectorClient{
		mediaEvents: make(chan domain.MediaEventRequest, 8),
		stopOn:      domain.MediaTypeImage,
	}
	sessions := NewSessionService(time.Minute)
	eventService := NewGatewayEventService(
		detector,
		sessions,
		&blockingStreamExtractor{},
		EventServiceConfig{
			AudioChunkDuration:  10 * time.Millisecond,
			VideoChunkDuration:  10 * time.Millisecond,
			AudioFormat:         "wav",
			MediaRequestTimeout: 100 * time.Millisecond,
		},
	)

	err := eventService.HandleDoorbellPress(context.Background(), domain.DoorbellPressEvent{
		EventID:  "track-image-stop",
		DeviceID: "device-image-stop",
		Event:    "press",
		Image:    base64.StdEncoding.EncodeToString([]byte("press-jpeg")),
	})
	require.NoError(t, err)

	select {
	case mediaEvent := <-detector.mediaEvents:
		assert.Equal(t, domain.MediaTypeImage, mediaEvent.MediaType)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for IMAGE")
	}

	require.Eventually(t, func() bool {
		_, active := sessions.GetActiveSession("device-image-stop")
		return !active
	}, time.Second, 5*time.Millisecond)

	// Los lectores de video/audio quedan bloqueados en ffmpeg hasta que se
	// cancela la sesion: si Close retorna, el STOP_DANGER corto el streaming.
	closed := make(chan struct{})
	go func() {
		eventService.Close()
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("streaming kept running after IMAGE STOP_DANGER")
	}
}

// blockingStreamExtractor simula un RTSP que todavia no entrega chunks.
type blockingStreamExtractor struct {
	stubMediaExtractor
}

func (e *blockingStreamExtractor) ExtractAudioChunk(ctx context.Context, _ string, _ time.Duration, _ string) ([]byte, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func (e *blockingStreamExtractor) ExtractVideoChunk(ctx context.Context, _ string, _ time.Duration) ([]byte, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func mediaTypes(events []domain.MediaEventRequest) []domain.MediaType {
	order := make([]domain.MediaType, 0, len(events))
	for _, event := range events {
		order = append(order, event.MediaType)
	}
	return order
}

func TestEventService_WritesDebugChunks(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	detector := &recordingDetectorClient{mediaEvents: make(chan domain.MediaEventRequest, 32)}
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

func TestEventService_BuildsRTSPURLFromBaseAndIgnoresPayloadURL(t *testing.T) {
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
			AudioFormat:         "mp3",
			MediaRequestTimeout: 100 * time.Millisecond,
			RTSPBase:            "rtsp://mediamtx:8554",
		},
	)
	t.Cleanup(eventService.Close)

	err := eventService.HandleDoorbellPress(context.Background(), domain.DoorbellPressEvent{
		EventID:  "track-url",
		DeviceID: "device-url",
		Event:    "press",
		Image:    base64.StdEncoding.EncodeToString([]byte("press-jpeg")),
		RTSPURL:  "rtsp://esp32.local:554/stream/v1",
	})
	require.NoError(t, err)

	session, ok := sessions.GetActiveSession("device-url")
	require.True(t, ok)
	assert.Equal(t, "rtsp://mediamtx:8554/device-url/doorbell/track-url", session.RTSPURL)

	require.NoError(t, eventService.HandleStreamEnd(context.Background(), domain.StreamEndEvent{
		DeviceID: "device-url",
		TrackID:  "track-url",
	}))
}

func TestEventService_RequiresDeviceAndEventIDsWithoutRTSPURL(t *testing.T) {
	t.Parallel()

	eventService := NewGatewayEventService(
		&recordingDetectorClient{mediaEvents: make(chan domain.MediaEventRequest, 1)},
		NewSessionService(time.Minute),
		&stubMediaExtractor{},
		EventServiceConfig{AudioFormat: "mp3", RTSPBase: "rtsp://mediamtx:8554"},
	)
	t.Cleanup(eventService.Close)

	err := eventService.HandleDoorbellPress(context.Background(), domain.DoorbellPressEvent{
		DeviceID: "device-url",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "event ID")

	err = eventService.HandleDoorbellPress(context.Background(), domain.DoorbellPressEvent{
		EventID: "track-url",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "device ID")
}

func TestEventService_KeepsConcurrentSessionsForDifferentDevices(t *testing.T) {
	t.Parallel()

	detector := &recordingDetectorClient{mediaEvents: make(chan domain.MediaEventRequest, 64)}
	sessions := NewSessionService(time.Minute)
	eventService := NewGatewayEventService(
		detector,
		sessions,
		&stubMediaExtractor{},
		EventServiceConfig{
			AudioChunkDuration:  10 * time.Millisecond,
			VideoChunkDuration:  10 * time.Millisecond,
			AudioFormat:         "mp3",
			MediaRequestTimeout: 100 * time.Millisecond,
			RTSPBase:            "rtsp://mediamtx:8554",
		},
	)
	t.Cleanup(eventService.Close)

	require.NoError(t, eventService.HandleDoorbellPress(context.Background(), domain.DoorbellPressEvent{
		EventID:  "track-a",
		DeviceID: "device-a",
		Image:    base64.StdEncoding.EncodeToString([]byte("a")),
	}))
	require.NoError(t, eventService.HandleDoorbellPress(context.Background(), domain.DoorbellPressEvent{
		EventID:  "track-b",
		DeviceID: "device-b",
		Image:    base64.StdEncoding.EncodeToString([]byte("b")),
	}))

	sessionA, okA := sessions.GetActiveSession("device-a")
	sessionB, okB := sessions.GetActiveSession("device-b")
	require.True(t, okA)
	require.True(t, okB)
	assert.Equal(t, "track-a", sessionA.TrackID)
	assert.Equal(t, "track-b", sessionB.TrackID)
	assert.Equal(t, "rtsp://mediamtx:8554/device-a/doorbell/track-a", sessionA.RTSPURL)
	assert.Equal(t, "rtsp://mediamtx:8554/device-b/doorbell/track-b", sessionB.RTSPURL)

	require.NoError(t, eventService.HandleStreamEnd(context.Background(), domain.StreamEndEvent{
		DeviceID: "device-a",
		TrackID:  "track-other",
	}))
	_, okA = sessions.GetActiveSession("device-a")
	_, okB = sessions.GetActiveSession("device-b")
	assert.True(t, okA)
	assert.True(t, okB)

	require.NoError(t, eventService.HandleStreamEnd(context.Background(), domain.StreamEndEvent{
		DeviceID: "device-a",
		TrackID:  "track-a",
	}))
	_, okA = sessions.GetActiveSession("device-a")
	_, okB = sessions.GetActiveSession("device-b")
	assert.False(t, okA)
	assert.True(t, okB)
}

type recordingURLExtractor struct {
	stubMediaExtractor
	urls []string
}

func (e *recordingURLExtractor) ExtractImage(ctx context.Context, rtspURL string) ([]byte, error) {
	e.mu.Lock()
	e.urls = append(e.urls, rtspURL)
	e.mu.Unlock()
	return e.stubMediaExtractor.ExtractImage(ctx, rtspURL)
}

func TestEventService_CaptureImageUsesActiveSessionJPEG(t *testing.T) {
	t.Parallel()

	detector := &recordingDetectorClient{mediaEvents: make(chan domain.MediaEventRequest, 64)}
	sessions := NewSessionService(time.Minute)
	extractor := &recordingURLExtractor{}
	eventService := NewGatewayEventService(
		detector,
		sessions,
		extractor,
		EventServiceConfig{
			AudioFormat:         "mp3",
			MediaRequestTimeout: 100 * time.Millisecond,
			RTSPBase:            "rtsp://mediamtx:8554",
		},
	)
	t.Cleanup(eventService.Close)

	pressJPEG := []byte("press-jpeg")
	captureJPEG := []byte{0xff, 0xd8, 0xff, 0xd9}
	require.NoError(t, eventService.HandleDoorbellPress(context.Background(), domain.DoorbellPressEvent{
		EventID:  "track-cap",
		DeviceID: "device-cap",
		Image:    base64.StdEncoding.EncodeToString(pressJPEG),
	}))

	err := eventService.HandleCapture(context.Background(), domain.MediaTypeImage, domain.CaptureEvent{
		DeviceID: "device-cap",
		TrackID:  "track-cap",
	})
	require.Error(t, err)

	err = eventService.HandleCapture(context.Background(), domain.MediaTypeImage, domain.CaptureEvent{
		DeviceID: "device-cap",
		TrackID:  "track-other",
		Image:    base64.StdEncoding.EncodeToString(captureJPEG),
	})
	require.Error(t, err)

	active, ok := sessions.GetActiveSession("device-cap")
	require.True(t, ok)
	assert.Equal(t, "track-cap", active.TrackID)

	require.NoError(t, eventService.HandleCapture(context.Background(), domain.MediaTypeImage, domain.CaptureEvent{
		DeviceID: "device-cap",
		TrackID:  "track-cap",
		Image:    base64.StdEncoding.EncodeToString(captureJPEG),
	}))

	var gotCapture bool
	deadline := time.After(time.Second)
	for !gotCapture {
		select {
		case mediaEvent := <-detector.mediaEvents:
			if mediaEvent.MediaType == domain.MediaTypeImage && string(mediaEvent.Buffer) == string(captureJPEG) {
				assert.Equal(t, "device-cap", mediaEvent.DeviceID)
				assert.Equal(t, "track-cap", mediaEvent.TrackID)
				assert.Equal(t, "snapshot.jpeg", mediaEvent.FileName)
				assert.Equal(t, active.StartedAt, mediaEvent.StartedAt)
				gotCapture = true
			}
		case <-deadline:
			t.Fatal("timed out waiting for capture image")
		}
	}

	extractor.mu.Lock()
	assert.Empty(t, extractor.urls)
	extractor.mu.Unlock()

	active, ok = sessions.GetActiveSession("device-cap")
	require.True(t, ok)
	assert.Equal(t, "track-cap", active.TrackID)

	require.NoError(t, eventService.HandleStreamEnd(context.Background(), domain.StreamEndEvent{
		DeviceID: "device-cap",
		TrackID:  "track-cap",
	}))
	err = eventService.HandleCapture(context.Background(), domain.MediaTypeImage, domain.CaptureEvent{
		DeviceID: "device-cap",
		TrackID:  "track-cap",
		Image:    base64.StdEncoding.EncodeToString(captureJPEG),
	})
	require.Error(t, err)
	_, ok = sessions.GetActiveSession("device-cap")
	assert.False(t, ok)
}

func TestBuildDoorbellRTSPURL(t *testing.T) {
	t.Parallel()

	got, err := BuildDoorbellRTSPURL("rtsp://mediamtx:8554/", "dev-1", "track-1")
	require.NoError(t, err)
	assert.Equal(t, "rtsp://mediamtx:8554/dev-1/doorbell/track-1", got)

	got, err = BuildDoorbellRTSPURL("rtsps://gateway:gw-lab@mediamtx:8322", "dev-1", "track-1")
	require.NoError(t, err)
	assert.Equal(t, "rtsps://gateway:gw-lab@mediamtx:8322/dev-1/doorbell/track-1", got)

	_, err = BuildDoorbellRTSPURL("http://mediamtx:8554", "dev-1", "track-1")
	require.Error(t, err)
}

func TestRedactRTSPUserinfo(t *testing.T) {
	t.Parallel()

	assert.Equal(t,
		"rtsps://gateway:***@mediamtx:8322/dev-1/doorbell/track-1",
		redactRTSPUserinfo("rtsps://gateway:gw-lab@mediamtx:8322/dev-1/doorbell/track-1"),
	)
	assert.Equal(t,
		"rtsp://mediamtx:8554/dev-1/doorbell/track-1",
		redactRTSPUserinfo("rtsp://mediamtx:8554/dev-1/doorbell/track-1"),
	)
	assert.Contains(t, redactRTSPUserinfo("run FFmpeg: rtsps://gateway:super-secret@host:8322/path"), "gateway:***@")
	assert.NotContains(t, redactRTSPUserinfo("run FFmpeg: rtsps://gateway:super-secret@host:8322/path"), "super-secret")
}

type recordingCommander struct {
	mu           sync.Mutex
	instructions []domain.DeviceInstruction
	err          error
}

func (c *recordingCommander) SendInstruction(instruction domain.DeviceInstruction) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.instructions = append(c.instructions, instruction)
	return c.err
}

func TestEventService_DuplicatePressDoesNotRestartCall(t *testing.T) {
	t.Parallel()

	detector := &recordingDetectorClient{mediaEvents: make(chan domain.MediaEventRequest, 32)}
	sessions := NewSessionService(time.Minute)
	eventService := NewGatewayEventService(detector, sessions, &stubMediaExtractor{}, EventServiceConfig{
		AudioFormat: "mp3", RTSPBase: "rtsp://mediamtx:8554", MediaRequestTimeout: 100 * time.Millisecond,
	})
	t.Cleanup(eventService.Close)

	press := domain.DoorbellPressEvent{
		EventID:  "track-dup",
		DeviceID: "device-dup",
		Image:    base64.StdEncoding.EncodeToString([]byte("jpeg")),
	}
	require.NoError(t, eventService.HandleDoorbellPress(context.Background(), press))
	require.NoError(t, eventService.HandleDoorbellPress(context.Background(), press))

	session, ok := sessions.GetActiveSession("device-dup")
	require.True(t, ok)
	assert.Equal(t, "track-dup", session.TrackID)

	require.NoError(t, eventService.HandleStreamEnd(context.Background(), domain.StreamEndEvent{
		DeviceID: "device-dup", TrackID: "track-dup",
	}))
}

func TestEventService_NewPressReplacesPreviousCallForSameDevice(t *testing.T) {
	t.Parallel()

	detector := &recordingDetectorClient{mediaEvents: make(chan domain.MediaEventRequest, 32)}
	sessions := NewSessionService(time.Minute)
	eventService := NewGatewayEventService(detector, sessions, &stubMediaExtractor{}, EventServiceConfig{
		AudioFormat: "mp3", RTSPBase: "rtsp://mediamtx:8554", MediaRequestTimeout: 100 * time.Millisecond,
	})
	t.Cleanup(eventService.Close)

	require.NoError(t, eventService.HandleDoorbellPress(context.Background(), domain.DoorbellPressEvent{
		EventID: "track-old", DeviceID: "device-rep", Image: base64.StdEncoding.EncodeToString([]byte("a")),
	}))
	require.NoError(t, eventService.HandleDoorbellPress(context.Background(), domain.DoorbellPressEvent{
		EventID: "track-new", DeviceID: "device-rep", Image: base64.StdEncoding.EncodeToString([]byte("b")),
	}))

	session, ok := sessions.GetActiveSession("device-rep")
	require.True(t, ok)
	assert.Equal(t, "track-new", session.TrackID)
}

func TestEventService_PressWithoutImageSkipsSnapshotAndStartsStreaming(t *testing.T) {
	t.Parallel()

	var nullImage domain.DoorbellPressEvent
	require.NoError(t, json.Unmarshal([]byte(`{"event":"press","image":null}`), &nullImage))

	cases := map[string]string{
		"missing":        "",
		"null":           nullImage.Image,
		"blank":          "   ",
		"invalid base64": "%%%not-base64%%%",
	}

	for name, image := range cases {
		image := image
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			detector := &recordingDetectorClient{mediaEvents: make(chan domain.MediaEventRequest, 32)}
			extractor := &recordingURLExtractor{}
			sessions := NewSessionService(time.Minute)
			eventService := NewGatewayEventService(detector, sessions, extractor, EventServiceConfig{
				AudioChunkDuration:  10 * time.Millisecond,
				VideoChunkDuration:  10 * time.Millisecond,
				AudioFormat:         "mp3",
				RTSPBase:            "rtsp://mediamtx:8554",
				MediaRequestTimeout: 100 * time.Millisecond,
			})

			require.NoError(t, eventService.HandleDoorbellPress(context.Background(), domain.DoorbellPressEvent{
				EventID: "track-noimg", DeviceID: "device-noimg", Image: image,
			}))

			seen := map[domain.MediaType]bool{}
			for !seen[domain.MediaTypeAudio] || !seen[domain.MediaTypeVideo] {
				select {
				case mediaEvent := <-detector.mediaEvents:
					seen[mediaEvent.MediaType] = true
				case <-time.After(time.Second):
					t.Fatalf("timed out waiting for AUDIO and VIDEO, got %v", seen)
				}
			}

			require.NoError(t, eventService.HandleStreamEnd(context.Background(), domain.StreamEndEvent{
				DeviceID: "device-noimg", TrackID: "track-noimg",
			}))
			eventService.Close()

			for drained := false; !drained; {
				select {
				case mediaEvent := <-detector.mediaEvents:
					seen[mediaEvent.MediaType] = true
				default:
					drained = true
				}
			}
			assert.False(t, seen[domain.MediaTypeImage], "press without a valid image must not send a snapshot")

			extractor.mu.Lock()
			assert.Empty(t, extractor.urls, "press without image must not fall back to an RTSP snapshot")
			extractor.mu.Unlock()
		})
	}
}

func TestEventService_SnapshotFailureDoesNotBlockStreaming(t *testing.T) {
	t.Parallel()

	detector := &failingImageDetectorClient{
		recordingDetectorClient: recordingDetectorClient{mediaEvents: make(chan domain.MediaEventRequest, 32)},
	}
	sessions := NewSessionService(time.Minute)
	eventService := NewGatewayEventService(detector, sessions, &stubMediaExtractor{}, EventServiceConfig{
		AudioChunkDuration:  10 * time.Millisecond,
		VideoChunkDuration:  10 * time.Millisecond,
		AudioFormat:         "mp3",
		MediaRequestTimeout: 100 * time.Millisecond,
	})

	require.NoError(t, eventService.HandleDoorbellPress(context.Background(), domain.DoorbellPressEvent{
		EventID: "track-snapfail", DeviceID: "device-snapfail", Image: base64.StdEncoding.EncodeToString([]byte("jpeg")),
	}))

	seen := map[domain.MediaType]bool{}
	for !seen[domain.MediaTypeAudio] || !seen[domain.MediaTypeVideo] {
		select {
		case mediaEvent := <-detector.mediaEvents:
			seen[mediaEvent.MediaType] = true
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for AUDIO and VIDEO after snapshot failure, got %v", seen)
		}
	}

	require.NoError(t, eventService.HandleStreamEnd(context.Background(), domain.StreamEndEvent{
		DeviceID: "device-snapfail", TrackID: "track-snapfail",
	}))
	eventService.Close()
}

type failingImageDetectorClient struct {
	recordingDetectorClient
}

func (c *failingImageDetectorClient) SendMediaEvent(ctx context.Context, req domain.MediaEventRequest) (*domain.OrchestratorEventData, error) {
	if req.MediaType == domain.MediaTypeImage {
		return nil, fmt.Errorf("detective-manager unavailable")
	}
	return c.recordingDetectorClient.SendMediaEvent(ctx, req)
}

func TestEventService_PublishesContinueInstructionAfterChunk(t *testing.T) {
	t.Parallel()

	detector := &recordingDetectorClient{mediaEvents: make(chan domain.MediaEventRequest, 32)}
	commander := &recordingCommander{}
	sessions := NewSessionService(time.Minute)
	eventService := NewGatewayEventService(detector, sessions, &stubMediaExtractor{}, EventServiceConfig{
		AudioFormat: "mp3", RTSPBase: "rtsp://mediamtx:8554", MediaRequestTimeout: 100 * time.Millisecond,
	})
	eventService.SetDeviceCommander(commander)
	t.Cleanup(eventService.Close)

	require.NoError(t, eventService.HandleDoorbellPress(context.Background(), domain.DoorbellPressEvent{
		EventID: "track-cmd", DeviceID: "device-cmd", Image: base64.StdEncoding.EncodeToString([]byte("jpeg")),
	}))

	var instruction domain.DeviceInstruction
	require.Eventually(t, func() bool {
		commander.mu.Lock()
		defer commander.mu.Unlock()
		for _, candidate := range commander.instructions {
			if candidate.ChunkType == "IMAGE" {
				instruction = candidate
				return true
			}
		}
		return false
	}, time.Second, 10*time.Millisecond)

	assert.NotEmpty(t, instruction.MessageID)
	assert.Equal(t, "device-cmd", instruction.DeviceID)
	assert.Equal(t, "track-cmd", instruction.TrackID)
	assert.Equal(t, "IMAGE", instruction.ChunkType)
	assert.Equal(t, domain.DeviceActionContinue, instruction.Action)
	assert.Equal(t, "LOW", instruction.Severity)
	assert.Equal(t, "No danger.", instruction.Message)

	require.NoError(t, eventService.HandleStreamEnd(context.Background(), domain.StreamEndEvent{
		DeviceID: "device-cmd", TrackID: "track-cmd",
	}))
}

func TestEventService_KeepsLocalTrackIDWhenOrchestratorReturnsAnother(t *testing.T) {
	t.Parallel()

	detector := &orchestratorTrackOverrideClient{
		recordingDetectorClient: recordingDetectorClient{mediaEvents: make(chan domain.MediaEventRequest, 8)},
		trackID:                 "track-from-dm",
	}
	commander := &recordingCommander{}
	sessions := NewSessionService(time.Minute)
	eventService := NewGatewayEventService(detector, sessions, &stubMediaExtractor{}, EventServiceConfig{
		AudioFormat: "mp3", RTSPBase: "rtsp://mediamtx:8554", MediaRequestTimeout: 100 * time.Millisecond,
	})
	eventService.SetDeviceCommander(commander)
	t.Cleanup(eventService.Close)

	require.NoError(t, eventService.HandleDoorbellPress(context.Background(), domain.DoorbellPressEvent{
		EventID: "track-local", DeviceID: "device-keep", Image: base64.StdEncoding.EncodeToString([]byte("jpeg")),
	}))

	require.Eventually(t, func() bool {
		commander.mu.Lock()
		defer commander.mu.Unlock()
		return len(commander.instructions) > 0
	}, time.Second, 10*time.Millisecond)

	commander.mu.Lock()
	instruction := commander.instructions[0]
	commander.mu.Unlock()
	assert.Equal(t, "track-local", instruction.TrackID)
	assert.Equal(t, "device-keep", instruction.DeviceID)

	require.NoError(t, eventService.HandleStreamEnd(context.Background(), domain.StreamEndEvent{
		DeviceID: "device-keep", TrackID: "track-local",
	}))
}

func TestEventService_DeviceJPEGIsForwardedAsImage(t *testing.T) {
	t.Parallel()

	detector := &recordingDetectorClient{mediaEvents: make(chan domain.MediaEventRequest, 4)}
	eventService := NewGatewayEventService(detector, NewSessionService(time.Minute), &stubMediaExtractor{}, EventServiceConfig{
		AudioFormat: "mp3", MediaRequestTimeout: 100 * time.Millisecond,
	})
	t.Cleanup(eventService.Close)

	jpeg := []byte("still")
	require.NoError(t, eventService.HandleDeviceMedia(context.Background(), domain.MediaTypeImage, domain.DeviceMediaEvent{
		DeviceID: "device-m",
		TrackID:  "track-m",
		Data:     base64.StdEncoding.EncodeToString(jpeg),
	}))

	select {
	case mediaEvent := <-detector.mediaEvents:
		assert.Equal(t, domain.MediaTypeImage, mediaEvent.MediaType)
		assert.Equal(t, jpeg, mediaEvent.Buffer)
		assert.Equal(t, "snapshot.jpeg", mediaEvent.FileName)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for device IMAGE")
	}
}

func TestEventService_DeviceVideoClipIsForwardedAsStill(t *testing.T) {
	t.Parallel()

	detector := &recordingDetectorClient{mediaEvents: make(chan domain.MediaEventRequest, 4)}
	eventService := NewGatewayEventService(detector, NewSessionService(time.Minute), &stubMediaExtractor{}, EventServiceConfig{
		AudioFormat: "mp3", MediaRequestTimeout: 100 * time.Millisecond,
	})
	t.Cleanup(eventService.Close)

	require.NoError(t, eventService.HandleDeviceMedia(context.Background(), domain.MediaTypeVideo, domain.DeviceMediaEvent{
		DeviceID: "device-v",
		EventID:  "track-v",
		Data:     base64.StdEncoding.EncodeToString([]byte("mp4-bytes")),
	}))

	select {
	case mediaEvent := <-detector.mediaEvents:
		assert.Equal(t, domain.MediaTypeImage, mediaEvent.MediaType)
		assert.Equal(t, []byte("image-from-video"), mediaEvent.Buffer)
		assert.Equal(t, "snapshot.jpeg", mediaEvent.FileName)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for still extracted from device video")
	}
}

func TestEventService_OnDemandAudioCaptureUsesSession(t *testing.T) {
	t.Parallel()

	detector := &recordingDetectorClient{mediaEvents: make(chan domain.MediaEventRequest, 32)}
	sessions := NewSessionService(time.Minute)
	// El streaming de la sesion arranca en paralelo y tambien pide audio, asi
	// que el extractor tiene que entregar audio en todas las llamadas.
	eventService := NewGatewayEventService(detector, sessions, &steadyAudioExtractor{}, EventServiceConfig{
		AudioChunkDuration:  10 * time.Millisecond,
		AudioFormat:         "mp3",
		MediaRequestTimeout: 100 * time.Millisecond,
		RTSPBase:            "rtsp://mediamtx:8554",
	})
	t.Cleanup(eventService.Close)

	require.NoError(t, eventService.HandleDoorbellPress(context.Background(), domain.DoorbellPressEvent{
		EventID: "track-aud", DeviceID: "device-aud", Image: base64.StdEncoding.EncodeToString([]byte("j")),
	}))
	require.NoError(t, eventService.HandleCapture(context.Background(), domain.MediaTypeAudio, domain.CaptureEvent{
		DeviceID: "device-aud",
	}))

	gotAudio := false
	deadline := time.After(time.Second)
	for !gotAudio {
		select {
		case mediaEvent := <-detector.mediaEvents:
			if mediaEvent.MediaType == domain.MediaTypeAudio && mediaEvent.FileName == "chunk_ondemand.mp3" {
				gotAudio = true
			}
		case <-deadline:
			t.Fatal("timed out waiting for on-demand AUDIO")
		}
	}

	require.NoError(t, eventService.HandleStreamEnd(context.Background(), domain.StreamEndEvent{
		DeviceID: "device-aud", TrackID: "track-aud",
	}))
}

type steadyAudioExtractor struct {
	stubMediaExtractor
}

func (e *steadyAudioExtractor) ExtractAudioChunk(ctx context.Context, _ string, _ time.Duration, _ string) ([]byte, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(5 * time.Millisecond):
		return []byte("audio"), nil
	}
}

func TestEventService_LegacyHTTPEventReachesDetector(t *testing.T) {
	t.Parallel()

	eventService := NewEventService(&recordingDetectorClient{mediaEvents: make(chan domain.MediaEventRequest, 1)})
	t.Cleanup(eventService.Close)

	resp, err := eventService.ProcessEvent(context.Background(), &domain.EventRequest{
		Type:  domain.EventTypeBell,
		Image: "base64",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "legacy-event", resp.Data.EventID)
}

func TestEventService_RejectsUnsupportedAudioFormat(t *testing.T) {
	t.Parallel()

	eventService := NewGatewayEventService(
		&recordingDetectorClient{mediaEvents: make(chan domain.MediaEventRequest, 1)},
		NewSessionService(time.Minute),
		&stubMediaExtractor{},
		EventServiceConfig{AudioFormat: "aac", RTSPBase: "rtsp://mediamtx:8554"},
	)
	t.Cleanup(eventService.Close)

	err := eventService.HandleDoorbellPress(context.Background(), domain.DoorbellPressEvent{
		EventID: "t", DeviceID: "d", Image: base64.StdEncoding.EncodeToString([]byte("j")),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported audio format")
}

// syncBuffer evita la carrera entre los workers que loguean y el test que lee.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func TestEventService_LogsChunkMetadataOnDispatch(t *testing.T) {
	var buf syncBuffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	detector := &recordingDetectorClient{mediaEvents: make(chan domain.MediaEventRequest, 32)}
	sessions := NewSessionService(time.Minute)
	eventService := NewGatewayEventService(detector, sessions, &stubMediaExtractor{}, EventServiceConfig{
		AudioFormat: "mp3", RTSPBase: "rtsp://mediamtx:8554", MediaRequestTimeout: 100 * time.Millisecond,
	})
	t.Cleanup(eventService.Close)

	require.NoError(t, eventService.HandleDoorbellPress(context.Background(), domain.DoorbellPressEvent{
		EventID: "track-log", DeviceID: "device-log", Image: base64.StdEncoding.EncodeToString([]byte("press-jpeg")),
	}))

	require.Eventually(t, func() bool {
		return strings.Contains(buf.String(), "trackId=track-log") &&
			strings.Contains(buf.String(), "deviceId=device-log") &&
			strings.Contains(buf.String(), "mediaType=IMAGE") &&
			strings.Contains(buf.String(), "fileName=snapshot.jpeg") &&
			strings.Contains(buf.String(), "action=CONTINUE")
	}, time.Second, 10*time.Millisecond)

	logged := buf.String()
	assert.Contains(t, logged, "bytes=")
	assert.Contains(t, logged, "extractMs=")
	assert.Contains(t, logged, "postMs=")
	assert.NotContains(t, logged, "press-jpeg")

	require.NoError(t, eventService.HandleStreamEnd(context.Background(), domain.StreamEndEvent{
		DeviceID: "device-log", TrackID: "track-log",
	}))
}

type onceFailingVideoExtractor struct {
	stubMediaExtractor
	videoCalls int
}

func (e *onceFailingVideoExtractor) ExtractVideoChunk(ctx context.Context, url string, d time.Duration) ([]byte, error) {
	e.mu.Lock()
	e.videoCalls++
	n := e.videoCalls
	e.mu.Unlock()
	if n == 1 {
		return nil, fmt.Errorf("rtsp blip")
	}
	return e.stubMediaExtractor.ExtractVideoChunk(ctx, url, d)
}

func TestEventService_RecoversAfterRTSPExtractBlip(t *testing.T) {
	t.Parallel()

	detector := &recordingDetectorClient{mediaEvents: make(chan domain.MediaEventRequest, 32)}
	sessions := NewSessionService(time.Minute)
	eventService := NewGatewayEventService(detector, sessions, &onceFailingVideoExtractor{}, EventServiceConfig{
		AudioChunkDuration:  10 * time.Millisecond,
		VideoChunkDuration:  10 * time.Millisecond,
		AudioFormat:         "mp3",
		MediaRequestTimeout: 100 * time.Millisecond,
		RTSPBase:            "rtsp://mediamtx:8554",
	})
	t.Cleanup(eventService.Close)

	require.NoError(t, eventService.HandleDoorbellPress(context.Background(), domain.DoorbellPressEvent{
		EventID: "track-blip", DeviceID: "device-blip", Image: base64.StdEncoding.EncodeToString([]byte("j")),
	}))

	gotVideo := false
	deadline := time.After(3 * time.Second)
	for !gotVideo {
		select {
		case mediaEvent := <-detector.mediaEvents:
			if mediaEvent.MediaType == domain.MediaTypeVideo {
				gotVideo = true
			}
		case <-deadline:
			t.Fatal("timed out waiting for VIDEO after RTSP blip")
		}
	}

	require.NoError(t, eventService.HandleStreamEnd(context.Background(), domain.StreamEndEvent{
		DeviceID: "device-blip", TrackID: "track-blip",
	}))
}

func TestEventService_StreamEndCancelsRunningCapture(t *testing.T) {
	t.Parallel()

	detector := &recordingDetectorClient{mediaEvents: make(chan domain.MediaEventRequest, 8)}
	sessions := NewSessionService(time.Minute)
	eventService := NewGatewayEventService(detector, sessions, &blockingStreamExtractor{}, EventServiceConfig{
		AudioChunkDuration:  time.Minute,
		VideoChunkDuration:  time.Minute,
		AudioFormat:         "mp3",
		MediaRequestTimeout: time.Minute,
		RTSPBase:            "rtsp://mediamtx:8554",
	})
	t.Cleanup(eventService.Close)

	require.NoError(t, eventService.HandleDoorbellPress(context.Background(), domain.DoorbellPressEvent{
		EventID: "track-cancel", DeviceID: "device-cancel",
	}))

	// El ffmpeg del capture queda bloqueado (1 min de chunk + 1 min de timeout).
	captureErr := make(chan error, 1)
	go func() {
		captureErr <- eventService.HandleCapture(context.Background(), domain.MediaTypeAudio, domain.CaptureEvent{
			DeviceID: "device-cancel", TrackID: "track-cancel",
		})
	}()

	time.Sleep(20 * time.Millisecond)
	require.NoError(t, eventService.HandleStreamEnd(context.Background(), domain.StreamEndEvent{
		DeviceID: "device-cancel", TrackID: "track-cancel",
	}))

	select {
	case err := <-captureErr:
		require.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("stream_end did not cancel the running capture")
	}
}
