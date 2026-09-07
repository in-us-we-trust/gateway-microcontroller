package domain

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseHomeEventTopic(t *testing.T) {
	t.Parallel()

	parsed, err := ParseHomeEventTopic("home/device-a/events/track-1/press")
	require.NoError(t, err)
	assert.Equal(t, HomeEventTopic{DeviceID: "device-a", TrackID: "track-1", Action: "press"}, parsed)
}

func TestParseHomeEventTopic_RejectsLegacyGlobalTopic(t *testing.T) {
	t.Parallel()

	_, err := ParseHomeEventTopic("home/doorbell/events/press")
	require.Error(t, err)
}

func TestParseHomeEventTopic_RejectsMalformed(t *testing.T) {
	t.Parallel()

	cases := []string{
		"home/device-a/events/track-1",
		"home/device-a/status",
		"home/device-a/events/track-1/press/extra",
		"home/+/events/track-1/press",
		"",
	}
	for _, topic := range cases {
		_, err := ParseHomeEventTopic(topic)
		assert.Error(t, err, topic)
	}
}

func TestHomeEventTopicPath(t *testing.T) {
	t.Parallel()

	assert.Equal(
		t,
		"home/device-a/events/track-1/ia_response",
		HomeEventTopicPath("device-a", "track-1", MQTTActionAIResponse),
	)
}

func TestBindTopicIdentity_AllowsEmptyJSONFields(t *testing.T) {
	t.Parallel()

	topic := HomeEventTopic{DeviceID: "device-a", TrackID: "track-1", Action: MQTTActionPress}
	require.NoError(t, BindTopicIdentity(topic, "", "", ""))
	require.NoError(t, BindTopicIdentity(topic, "device-a", "track-1", "track-1"))
}

func TestBindTopicIdentity_RejectsMismatch(t *testing.T) {
	t.Parallel()

	topic := HomeEventTopic{DeviceID: "device-a", TrackID: "track-1", Action: MQTTActionPress}
	require.Error(t, BindTopicIdentity(topic, "device-b", "", "track-1"))
	require.Error(t, BindTopicIdentity(topic, "device-a", "", "track-2"))
	require.Error(t, BindTopicIdentity(topic, "device-a", "track-2", ""))
}
