package domain

import (
	"fmt"
	"strings"
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

func BindTopicIdentity(topic HomeEventTopic, jsonDeviceID, jsonTrackID, jsonEventID string) error {
	jsonDeviceID = strings.TrimSpace(jsonDeviceID)
	jsonTrackID = strings.TrimSpace(jsonTrackID)
	jsonEventID = strings.TrimSpace(jsonEventID)

	if jsonDeviceID != "" && jsonDeviceID != topic.DeviceID {
		return fmt.Errorf("deviceId in payload %q does not match topic %q", jsonDeviceID, topic.DeviceID)
	}

	jsonCallID := jsonTrackID
	if jsonCallID == "" {
		jsonCallID = jsonEventID
	}
	if jsonCallID != "" && jsonCallID != topic.TrackID {
		return fmt.Errorf("trackId in payload %q does not match topic %q", jsonCallID, topic.TrackID)
	}

	return nil
}
