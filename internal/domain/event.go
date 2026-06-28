package domain

type EventType string

const (
	EventTypeBell   EventType = "bell"
	EventTypeMotion EventType = "motion"
)

type EventRequest struct {
	Type  EventType `json:"type"  binding:"required,oneof=bell motion"`
	Image string    `json:"image" binding:"required"`
}

type EventData struct {
	EventID         string          `json:"eventId"`
	DeviceID        string          `json:"deviceId"`
	Timestamp       string          `json:"timestamp"`
	ImageURL        string          `json:"imageUrl"`
	EventType       string          `json:"eventType"`
	FaceRecognition FaceRecognition `json:"faceRecognition"`
	IsIntruder      bool            `json:"isIntruder"`
}

type FaceRecognition struct {
	Known           bool     `json:"known"`
	Threshold       float64  `json:"threshold"`
	Distance        *float64 `json:"distance"`
	DistanceMetric  string   `json:"distance_metric"`
	ClosestMatch    *string  `json:"closest_match"`
	ModelName       string   `json:"model_name"`
	DetectorBackend string   `json:"detector_backend"`
}

type EventResponse struct {
	Data EventData `json:"data"`
}
