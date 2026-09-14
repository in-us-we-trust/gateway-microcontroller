package service

import (
	"context"
	"encoding/base64"
	"fmt"
	"log"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"gateway-microcontroller/internal/client"
	"gateway-microcontroller/internal/domain"
)

type EventService interface {
	ProcessEvent(ctx context.Context, req *domain.EventRequest) (*domain.EventResponse, error)
	HandleDoorbellPress(ctx context.Context, event domain.DoorbellPressEvent) error
	HandleStreamEnd(ctx context.Context, event domain.StreamEndEvent) error
	HandleCapture(ctx context.Context, mediaType domain.MediaType, event domain.CaptureEvent) error
	HandleDeviceMedia(ctx context.Context, mediaType domain.MediaType, event domain.DeviceMediaEvent) error
	SetDeviceCommander(commander client.DeviceCommander)
	Close()
}

type EventServiceConfig struct {
	AudioChunkDuration  time.Duration
	VideoChunkDuration  time.Duration
	AudioFormat         string
	MediaRequestTimeout time.Duration
	MediaChunkBatchSize int
	DebugMedia          DebugMediaWriter
	RTSPBase            string
}

type eventService struct {
	detector   client.DetectorClient
	sessions   SessionRegistry
	extractor  MediaExtractor
	config     EventServiceConfig
	workers    sync.WaitGroup
	commanderM sync.RWMutex
	commander  client.DeviceCommander
	debugMedia DebugMediaWriter
}

func NewEventService(detector client.DetectorClient) EventService {
	return NewGatewayEventService(
		detector,
		NewSessionService(30*time.Second),
		NewFFmpegMediaExtractor("ffmpeg"),
		EventServiceConfig{},
	)
}

func NewGatewayEventService(
	detector client.DetectorClient,
	sessions SessionRegistry,
	extractor MediaExtractor,
	config EventServiceConfig,
) EventService {
	if config.AudioChunkDuration <= 0 {
		config.AudioChunkDuration = 10 * time.Second
	}
	if config.VideoChunkDuration <= 0 {
		config.VideoChunkDuration = 5 * time.Second
	}
	config.AudioFormat = strings.ToLower(strings.TrimSpace(config.AudioFormat))
	if config.AudioFormat == "" {
		config.AudioFormat = "mp3"
	}
	if config.MediaRequestTimeout <= 0 {
		config.MediaRequestTimeout = 10 * time.Second
	}
	if config.MediaChunkBatchSize <= 0 {
		config.MediaChunkBatchSize = 1
	}
	config.RTSPBase = strings.TrimRight(strings.TrimSpace(config.RTSPBase), "/")
	if config.RTSPBase == "" {
		config.RTSPBase = defaultRTSPBase
	}
	debugMedia := config.DebugMedia
	if debugMedia == nil {
		debugMedia = NewDebugMediaWriter(false, "", 0)
	}

	return &eventService{
		detector:   detector,
		sessions:   sessions,
		extractor:  extractor,
		config:     config,
		debugMedia: debugMedia,
	}
}

func (s *eventService) ProcessEvent(ctx context.Context, req *domain.EventRequest) (*domain.EventResponse, error) {
	data, err := s.detector.Analyze(ctx, req)
	if err != nil {
		return nil, err
	}

	return &domain.EventResponse{Data: *data}, nil
}

func (s *eventService) HandleDoorbellPress(_ context.Context, event domain.DoorbellPressEvent) error {
	event.EventID = strings.TrimSpace(event.EventID)
	event.DeviceID = strings.TrimSpace(event.DeviceID)
	if event.EventID == "" {
		return fmt.Errorf("doorbell press event ID is required")
	}
	if event.DeviceID == "" {
		return fmt.Errorf("doorbell press device ID is required")
	}
	rtspURL, err := BuildDoorbellRTSPURL(s.config.RTSPBase, event.DeviceID, event.EventID)
	if err != nil {
		return err
	}
	if s.config.AudioFormat != "mp3" && s.config.AudioFormat != "wav" {
		return fmt.Errorf("unsupported audio format %q", s.config.AudioFormat)
	}
	if active, ok := s.sessions.GetActiveSession(event.DeviceID); ok && active.TrackID == event.EventID {
		return nil
	}

	session, sessionCtx := s.sessions.StartSession(event.DeviceID, event.EventID, rtspURL)
	log.Printf("started session device=%s trackId=%s rtsp=%s", session.DeviceID, session.TrackID, redactRTSPUserinfo(session.RTSPURL))

	if err := s.dispatchPressSnapshot(sessionCtx, session, event); err != nil {
		if sessionCtx.Err() == nil {
			log.Printf("failed to dispatch doorbell snapshot for device %s: %v", session.DeviceID, err)
		}
		return nil
	}
	if sessionCtx.Err() != nil || !s.sessionIsCurrent(session) {
		return nil
	}

	s.workers.Add(1)
	go func() {
		defer s.workers.Done()
		s.dispatchRTSPChunks(sessionCtx, session)
	}()

	return nil
}

func (s *eventService) SetDeviceCommander(commander client.DeviceCommander) {
	s.commanderM.Lock()
	s.commander = commander
	s.commanderM.Unlock()
}

func (s *eventService) HandleStreamEnd(_ context.Context, event domain.StreamEndEvent) error {
	deviceID := strings.TrimSpace(event.DeviceID)
	if deviceID == "" {
		return fmt.Errorf("stream end device ID is required")
	}

	trackID := strings.TrimSpace(event.TrackID)
	if trackID == "" {
		trackID = strings.TrimSpace(event.EventID)
	}

	s.sessions.EndSession(deviceID, trackID)
	return nil
}

func (s *eventService) HandleCapture(ctx context.Context, mediaType domain.MediaType, event domain.CaptureEvent) error {
	deviceID := strings.TrimSpace(event.DeviceID)
	if deviceID == "" {
		return fmt.Errorf("device ID is required")
	}

	trackID := strings.TrimSpace(event.TrackID)
	if trackID == "" {
		trackID = strings.TrimSpace(event.EventID)
	}

	var rtspURL string
	if session, ok := s.sessions.GetActiveSession(deviceID); ok {
		if trackID == "" {
			trackID = session.TrackID
		}
		if trackID == session.TrackID {
			rtspURL = session.RTSPURL
		}
	}
	if trackID == "" {
		return fmt.Errorf("track ID is required when no active session exists")
	}
	if rtspURL == "" {
		built, err := BuildDoorbellRTSPURL(s.config.RTSPBase, deviceID, trackID)
		if err != nil {
			return err
		}
		rtspURL = built
	}

	session := domain.SessionState{
		TrackID:  trackID,
		DeviceID: deviceID,
		RTSPURL:  rtspURL,
	}

	switch mediaType {
	case domain.MediaTypeImage:
		extractCtx, cancelExtract := context.WithTimeout(ctx, s.config.MediaRequestTimeout)
		extractStart := time.Now()
		image, err := s.extractor.ExtractImage(extractCtx, session.RTSPURL)
		extractDuration := time.Since(extractStart)
		cancelExtract()
		if err != nil {
			return fmt.Errorf("extract image: %w", err)
		}
		requestCtx, cancelRequest := context.WithTimeout(ctx, s.config.MediaRequestTimeout)
		defer cancelRequest()
		return s.dispatchPreparedMedia(requestCtx, domain.MediaEventRequest{
			TrackID:         session.TrackID,
			DeviceID:        session.DeviceID,
			MediaType:       domain.MediaTypeImage,
			FileName:        "snapshot.jpeg",
			Buffer:          image,
			StartedAt:       session.StartedAt,
			ChunkNumber:     1,
			ExtractDuration: extractDuration,
		})
	case domain.MediaTypeAudio:
		extractTimeout := s.config.AudioChunkDuration + s.config.MediaRequestTimeout
		extractCtx, cancelExtract := context.WithTimeout(ctx, extractTimeout)
		extractStart := time.Now()
		audio, err := s.extractor.ExtractAudioChunk(
			extractCtx,
			session.RTSPURL,
			s.config.AudioChunkDuration,
			s.config.AudioFormat,
		)
		extractDuration := time.Since(extractStart)
		cancelExtract()
		if err != nil {
			return fmt.Errorf("extract audio: %w", err)
		}
		requestCtx, cancelRequest := context.WithTimeout(ctx, s.config.MediaRequestTimeout)
		defer cancelRequest()
		return s.dispatchPreparedMedia(requestCtx, domain.MediaEventRequest{
			TrackID:         session.TrackID,
			DeviceID:        session.DeviceID,
			MediaType:       domain.MediaTypeAudio,
			FileName:        "chunk_ondemand." + s.config.AudioFormat,
			Buffer:          audio,
			StartedAt:       session.StartedAt,
			ChunkNumber:     1,
			ExtractDuration: extractDuration,
		})
	default:
		return fmt.Errorf("unsupported capture media type %q", mediaType)
	}
}

func (s *eventService) HandleDeviceMedia(ctx context.Context, mediaType domain.MediaType, event domain.DeviceMediaEvent) error {
	if strings.TrimSpace(event.DeviceID) == "" {
		return fmt.Errorf("device ID is required")
	}

	trackID := strings.TrimSpace(event.TrackID)
	if trackID == "" {
		trackID = strings.TrimSpace(event.EventID)
	}
	if trackID == "" {
		if session, ok := s.sessions.GetActiveSession(event.DeviceID); ok {
			trackID = session.TrackID
		}
	}
	if trackID == "" {
		return fmt.Errorf("track ID is required when no active session exists")
	}

	buffer, err := base64.StdEncoding.DecodeString(strings.TrimSpace(event.Data))
	if err != nil {
		return fmt.Errorf("decode media payload: %w", err)
	}

	fileName := strings.TrimSpace(event.FileName)
	orchestratorType := mediaType
	extractDuration := time.Duration(0)
	switch mediaType {
	case domain.MediaTypeImage:
		if fileName == "" {
			fileName = "snapshot.jpeg"
		}
	case domain.MediaTypeAudio:
		if fileName == "" {
			fileName = "chunk.mp3"
		}
	case domain.MediaTypeVideo:
		if fileName == "" {
			fileName = "clip.mp4"
		}
		extractStart := time.Now()
		image, err := s.extractor.ExtractImageFromFile(ctx, fileName, buffer)
		extractDuration = time.Since(extractStart)
		if err != nil {
			return fmt.Errorf("extract image from video: %w", err)
		}
		buffer = image
		fileName = "snapshot.jpeg"
		orchestratorType = domain.MediaTypeImage
	default:
		return fmt.Errorf("unsupported media type %q", mediaType)
	}

	return s.dispatchPreparedMedia(ctx, domain.MediaEventRequest{
		TrackID:         trackID,
		DeviceID:        event.DeviceID,
		MediaType:       orchestratorType,
		FileName:        fileName,
		Buffer:          buffer,
		ExtractDuration: extractDuration,
	})
}

func (s *eventService) Close() {
	s.sessions.Close()
	s.workers.Wait()
}

func (s *eventService) dispatchPressSnapshot(ctx context.Context, session domain.SessionState, event domain.DoorbellPressEvent) error {
	extractStart := time.Now()
	image, err := decodePressImage(event.Image)
	if err != nil {
		log.Printf("press snapshot missing or invalid for device %s, falling back to RTSP: %v", session.DeviceID, err)
		extractCtx, cancelExtract := context.WithTimeout(ctx, s.config.MediaRequestTimeout)
		image, err = s.extractor.ExtractImage(extractCtx, session.RTSPURL)
		cancelExtract()
		if err != nil {
			return fmt.Errorf("extract image: %w", err)
		}
	}
	extractDuration := time.Since(extractStart)
	if !s.sessionIsCurrent(session) {
		return context.Canceled
	}

	requestCtx, cancelRequest := context.WithTimeout(ctx, s.config.MediaRequestTimeout)
	defer cancelRequest()

	return s.dispatchPreparedMedia(requestCtx, domain.MediaEventRequest{
		TrackID:         session.TrackID,
		DeviceID:        session.DeviceID,
		MediaType:       domain.MediaTypeImage,
		FileName:        "snapshot.jpeg",
		Buffer:          image,
		StartedAt:       session.StartedAt,
		ChunkNumber:     1,
		ExtractDuration: extractDuration,
	})
}

func (s *eventService) dispatchRTSPChunks(ctx context.Context, session domain.SessionState) {
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		s.dispatchMediaLoop(ctx, session, "video", s.config.VideoChunkDuration, func(extractCtx context.Context) ([]byte, string, domain.MediaType, error) {
			video, err := s.extractor.ExtractVideoChunk(extractCtx, session.RTSPURL, s.config.VideoChunkDuration)
			return video, "mp4", domain.MediaTypeVideo, err
		})
	}()
	go func() {
		defer wg.Done()
		s.dispatchMediaLoop(ctx, session, "audio", s.config.AudioChunkDuration, func(extractCtx context.Context) ([]byte, string, domain.MediaType, error) {
			audio, err := s.extractor.ExtractAudioChunk(
				extractCtx,
				session.RTSPURL,
				s.config.AudioChunkDuration,
				s.config.AudioFormat,
			)
			return audio, s.config.AudioFormat, domain.MediaTypeAudio, err
		})
	}()
	wg.Wait()
}

func (s *eventService) dispatchMediaLoop(
	ctx context.Context,
	session domain.SessionState,
	label string,
	chunkDuration time.Duration,
	extract func(context.Context) ([]byte, string, domain.MediaType, error),
) {
	chunkNumber := 1
	for {
		if ctx.Err() != nil || !s.sessionIsCurrent(session) {
			return
		}
		if !s.extractAndDispatchChunk(ctx, session, label, chunkDuration, chunkNumber, extract) {
			return
		}
		chunkNumber++
	}
}

func (s *eventService) extractAndDispatchChunk(
	ctx context.Context,
	session domain.SessionState,
	label string,
	chunkDuration time.Duration,
	chunkNumber int,
	extract func(context.Context) ([]byte, string, domain.MediaType, error),
) bool {
	for {
		if ctx.Err() != nil || !s.sessionIsCurrent(session) {
			return false
		}

		extractTimeout := chunkDuration + s.config.MediaRequestTimeout
		extractCtx, cancelExtract := context.WithTimeout(ctx, extractTimeout)
		extractStart := time.Now()
		data, ext, mediaType, err := extract(extractCtx)
		extractDuration := time.Since(extractStart)
		cancelExtract()
		if err != nil {
			if ctx.Err() != nil {
				return false
			}
			log.Printf("failed to extract %s chunk %d for device %s: %s", label, chunkNumber, session.DeviceID, redactRTSPUserinfo(err.Error()))
			if !waitForRetry(ctx, time.Second) {
				return false
			}
			continue
		}

		if !s.sessionIsCurrent(session) {
			return false
		}

		requestCtx, cancelRequest := context.WithTimeout(ctx, s.config.MediaRequestTimeout)
		err = s.dispatchPreparedMedia(requestCtx, domain.MediaEventRequest{
			TrackID:         session.TrackID,
			DeviceID:        session.DeviceID,
			MediaType:       mediaType,
			FileName:        fmt.Sprintf("chunk_%03d.%s", chunkNumber, ext),
			Buffer:          data,
			StartedAt:       session.StartedAt,
			ChunkNumber:     chunkNumber,
			ExtractDuration: extractDuration,
		})
		cancelRequest()
		if err != nil && ctx.Err() == nil {
			log.Printf("failed to dispatch %s chunk %d for device %s: %v", label, chunkNumber, session.DeviceID, err)
		}
		return ctx.Err() == nil && s.sessionIsCurrent(session)
	}
}

func (s *eventService) sessionIsCurrent(session domain.SessionState) bool {
	current, ok := s.sessions.GetActiveSession(session.DeviceID)
	return ok && current.TrackID == session.TrackID
}

func (s *eventService) dispatchPreparedMedia(ctx context.Context, req domain.MediaEventRequest) error {
	if req.FileName == "" {
		req.FileName = "payload.bin"
	}

	postStart := time.Now()
	data, err := s.detector.SendMediaEvent(ctx, req)
	req.PostDuration = time.Since(postStart)
	if err != nil {
		return err
	}

	instruction := domain.InstructionFromOrchestrator(req.DeviceID, *data)
	instruction.TrackID = req.TrackID
	req.Action = instruction.Action
	s.debugMedia.Write(req)
	s.publishInstruction(instruction)
	if instruction.Action == domain.DeviceActionStopDanger {
		s.sessions.EndSession(req.DeviceID, req.TrackID)
	}
	return nil
}

func (s *eventService) publishInstruction(instruction domain.DeviceInstruction) {
	s.commanderM.RLock()
	commander := s.commander
	s.commanderM.RUnlock()
	if commander == nil {
		return
	}
	if instruction.MessageID == "" {
		instruction.MessageID = domain.NewMQTTMessageID()
	}
	if err := commander.SendInstruction(instruction); err != nil {
		log.Printf("failed to publish device instruction for device %s: %v", instruction.DeviceID, err)
		return
	}
	log.Printf(
		"published ESP32 instruction messageId=%s device=%s trackId=%s chunkType=%s action=%s severity=%s",
		instruction.MessageID,
		instruction.DeviceID,
		instruction.TrackID,
		instruction.ChunkType,
		instruction.Action,
		instruction.Severity,
	)
}

func waitForRetry(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func decodePressImage(raw string) ([]byte, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("press image is empty")
	}
	if comma := strings.Index(raw, ","); comma >= 0 && strings.Contains(strings.ToLower(raw[:comma]), "base64") {
		raw = raw[comma+1:]
	}

	image, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		image, err = base64.RawStdEncoding.DecodeString(raw)
	}
	if err != nil {
		return nil, fmt.Errorf("decode press image: %w", err)
	}
	if len(image) == 0 {
		return nil, fmt.Errorf("press image decoded to an empty buffer")
	}
	return image, nil
}

const (
	defaultRTSPBase = "rtsp://localhost:8554"
	doorbellRTSPSeg = "doorbell"
)

func BuildDoorbellRTSPURL(base, deviceID, trackID string) (string, error) {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	deviceID = strings.TrimSpace(deviceID)
	trackID = strings.TrimSpace(trackID)
	if base == "" {
		return "", fmt.Errorf("RTSP base URL is required")
	}
	if deviceID == "" {
		return "", fmt.Errorf("device ID is required to build RTSP URL")
	}
	if trackID == "" {
		return "", fmt.Errorf("track ID is required to build RTSP URL")
	}

	rtspURL, err := url.JoinPath(base, deviceID, doorbellRTSPSeg, trackID)
	if err != nil {
		return "", fmt.Errorf("build RTSP URL: %w", err)
	}
	if err := validateRTSPURL(rtspURL); err != nil {
		return "", err
	}
	return rtspURL, nil
}

func validateRTSPURL(rawURL string) error {
	parsed, err := url.ParseRequestURI(rawURL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "rtsp" && parsed.Scheme != "rtsps") {
		return fmt.Errorf("RTSP URL must use the rtsp or rtsps scheme")
	}
	return nil
}

func redactRTSPUserinfo(raw string) string {
	return userinfoInURL.ReplaceAllString(raw, `${1}***@`)
}

var userinfoInURL = regexp.MustCompile(`(?i)((?:rtsp|rtsps|mqtt|mqtts|ssl|tls)://[^:/?#\s]+:)[^@/?#\s]+@`)
