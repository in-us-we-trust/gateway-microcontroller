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

func TestNewMQTTMessageID_IsUniqueUUID(t *testing.T) {
	t.Parallel()

	a := NewMQTTMessageID()
	b := NewMQTTMessageID()
	assert.NotEmpty(t, a)
	assert.NotEqual(t, a, b)
	assert.Equal(t, 36, len(a))
}
