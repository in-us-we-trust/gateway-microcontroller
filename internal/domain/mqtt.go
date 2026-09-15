package domain

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

const (
	MQTTHomeEventsWildcard = "home/+/events/#"

	MQTTActionPress        = "press"
	MQTTActionStreamEnd    = "stream_end"
	MQTTActionCaptureImage = "capture_image"
	MQTTActionCaptureAudio = "capture_audio"
	MQTTActionAIResponse   = "ia_response"
)

type HomeEventTopic struct {
	DeviceID string
	TrackID  string
	Action   string
}

func HomeEventTopicPath(deviceID, trackID, action string) string {
	return "home/" + deviceID + "/events/" + trackID + "/" + action
}

func NewMQTTMessageID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	h := hex.EncodeToString(b[:])
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}

func ParseHomeEventTopic(topic string) (HomeEventTopic, error) {
	parts := strings.Split(topic, "/")
	if len(parts) != 5 || parts[0] != "home" || parts[2] != "events" {
		return HomeEventTopic{}, fmt.Errorf("expected home/{deviceId}/events/{trackId}/{action}")
	}

	deviceID := strings.TrimSpace(parts[1])
	trackID := strings.TrimSpace(parts[3])
	action := strings.TrimSpace(parts[4])
	if deviceID == "" || trackID == "" || action == "" {
		return HomeEventTopic{}, fmt.Errorf("deviceId, trackId and action are required in MQTT topic")
	}
	if strings.ContainsAny(deviceID, "+#") || strings.ContainsAny(trackID, "+#") {
		return HomeEventTopic{}, fmt.Errorf("MQTT topic wildcards are not valid identity values")
	}

	return HomeEventTopic{DeviceID: deviceID, TrackID: trackID, Action: action}, nil
}
