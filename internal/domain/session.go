package domain

import "time"

type SessionState struct {
	TrackID   string
	DeviceID  string
	RTSPURL   string
	StartedAt time.Time
	ExpiresAt time.Time
}
