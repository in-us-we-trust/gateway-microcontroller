package domain

import (
	"encoding/json"
	"time"
)

type EventType string

type MediaType string

const (
	EventTypeBell   EventType = "bell"
	EventTypeMotion EventType = "motion"

	MediaTypeImage MediaType = "IMAGE"
	MediaTypeAudio MediaType = "AUDIO"
	MediaTypeVideo MediaType = "VIDEO"

	DeviceActionContinue   = "CONTINUE"
	DeviceActionStopDanger = "STOP_DANGER"
)

type DoorbellPressEvent struct {
	EventID           string  `json:"-"`
	DeviceID          string  `json:"-"`
	Timestamp         int64   `json:"timestamp"`
	Event             string  `json:"event,omitempty"`
	Image             string  `json:"image,omitempty"`
	RTSPURL           string  `json:"rtspUrl,omitempty"`
	BatteryPercentage float64 `json:"batteryPercentage"`
}

type CaptureEvent struct {
	EventID   string `json:"-"`
	TrackID   string `json:"-"`
	DeviceID  string `json:"-"`
	Timestamp int64  `json:"timestamp,omitempty"`
	Image     string `json:"image,omitempty"`
	RTSPURL   string `json:"rtspUrl,omitempty"`
}

type StreamEndEvent struct {
	EventID   string `json:"-"`
	TrackID   string `json:"-"`
	DeviceID  string `json:"-"`
	Timestamp int64  `json:"timestamp,omitempty"`
}

type MediaEventRequest struct {
	TrackID         string
	DeviceID        string
	MediaType       MediaType
	FileName        string
	Buffer          []byte
	StartedAt       time.Time
	ChunkNumber     int
	ExtractDuration time.Duration
	PostDuration    time.Duration
	Action          string
	DebugKind       string
}

type DeviceMediaEvent struct {
	EventID   string `json:"eventId,omitempty"`
	TrackID   string `json:"trackId,omitempty"`
	DeviceID  string `json:"deviceId"`
	Timestamp int64  `json:"timestamp,omitempty"`
	FileName  string `json:"fileName,omitempty"`
	Data      string `json:"data"`
}

type OrchestratorEventData struct {
	TrackID               string          `json:"trackId"`
	ChunkType             string          `json:"chunkType"`
	FinalVerdict          string          `json:"finalVerdict"`
	FinalSeverity         string          `json:"finalSeverity"`
	Message               string          `json:"message"`
	AdditionalInformation json.RawMessage `json:"additionalInformation,omitempty"`
}

type OrchestratorEventResponse struct {
	Data OrchestratorEventData `json:"data"`
}

type DeviceInstruction struct {
	MessageID             string          `json:"messageId"`
	TrackID               string          `json:"-"`
	DeviceID              string          `json:"-"`
	ChunkType             string          `json:"chunkType"`
	Action                string          `json:"action"`
	Severity              string          `json:"severity"`
	Message               string          `json:"message"`
	AdditionalInformation json.RawMessage `json:"additionalInformation,omitempty"`
}

type EventRequest struct {
	Type  EventType `json:"type" binding:"required,oneof=bell motion" example:"motion"`
	Image string    `json:"image" binding:"required" example:"/9j/4AAQSkZJRgABAQAAAQABAAD..."`
}

type EventData struct {
	EventID         string          `json:"eventId" example:"3fa85f64-5717-4562-b3fc-2c963f66afa6"`
	DeviceID        string          `json:"deviceId" example:"9df4a749-3ec2-43b9-bc42-35d2b6b5b173"`
	Timestamp       string          `json:"timestamp" example:"2026-06-28T21:19:59.466Z"`
	ImageURL        string          `json:"imageUrl" example:"https://storage.example.com/events/3fa85f64-5717-4562-b3fc-2c963f66afa6.jpg"`
	EventType       string          `json:"eventType" example:"motion"`
	FaceRecognition FaceRecognition `json:"faceRecognition"`
	IsIntruder      bool            `json:"isIntruder" example:"true"`
}

type FaceRecognition struct {
	Known           bool     `json:"known" example:"false"`
	Threshold       float64  `json:"threshold" example:"0.6"`
	Distance        *float64 `json:"distance" example:"0.82"`
	DistanceMetric  string   `json:"distance_metric" example:"cosine"`
	ClosestMatch    *string  `json:"closest_match" example:"John Doe"`
	ModelName       string   `json:"model_name" example:"Facenet512"`
	DetectorBackend string   `json:"detector_backend" example:"retinaface"`
}

type EventResponse struct {
	Data EventData `json:"data"`
}
