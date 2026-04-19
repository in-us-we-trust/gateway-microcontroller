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
	IsIntruder bool `json:"is_intruder"`
}

type EventResponse struct {
	Data EventData `json:"data"`
}
