package domain

type EventType string

const (
	EventTypeBell   EventType = "bell"
	EventTypeMotion EventType = "motion"
)

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
