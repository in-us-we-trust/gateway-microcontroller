package client

import (
	"encoding/json"
	"testing"
	"time"

	"gateway-microcontroller/internal/domain"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubToken struct {
	ok  bool
	err error
}

func (t stubToken) Wait() bool                     { return t.ok }
func (t stubToken) WaitTimeout(time.Duration) bool { return t.ok }
func (t stubToken) Done() <-chan struct{} {
	ch := make(chan struct{})
	close(ch)
	return ch
}
func (t stubToken) Error() error { return t.err }

type stubPublishClient struct {
	topic   string
	payload []byte
	token   stubToken
}

func (c *stubPublishClient) IsConnected() bool      { return true }
func (c *stubPublishClient) IsConnectionOpen() bool { return true }
func (c *stubPublishClient) Connect() mqtt.Token    { return c.token }
func (c *stubPublishClient) Disconnect(uint)        {}
func (c *stubPublishClient) Publish(topic string, _ byte, _ bool, payload interface{}) mqtt.Token {
	c.topic = topic
	if body, ok := payload.([]byte); ok {
		c.payload = body
	}
	return c.token
}
func (c *stubPublishClient) Subscribe(string, byte, mqtt.MessageHandler) mqtt.Token { return c.token }
func (c *stubPublishClient) SubscribeMultiple(map[string]byte, mqtt.MessageHandler) mqtt.Token {
	return c.token
}
func (c *stubPublishClient) Unsubscribe(...string) mqtt.Token     { return c.token }
func (c *stubPublishClient) AddRoute(string, mqtt.MessageHandler) {}
func (c *stubPublishClient) OptionsReader() mqtt.ClientOptionsReader {
	return mqtt.ClientOptionsReader{}
}

func TestMQTTInstructionPublisher_PublishesOnCallTopic(t *testing.T) {
	t.Parallel()

	broker := &stubPublishClient{token: stubToken{ok: true}}
	publisher := NewMQTTInstructionPublisher(broker)

	instruction := domain.DeviceInstruction{
		TrackID:   "track-1",
		DeviceID:  "device-a",
		ChunkType: "AUDIO",
		Action:    domain.DeviceActionContinue,
		Severity:  "LOW",
		Message:   "No blocking alerts.",
	}
	require.NoError(t, publisher.SendInstruction(instruction))

	assert.Equal(t, "home/device-a/events/track-1/ia_response", broker.topic)
	var got map[string]string
	require.NoError(t, json.Unmarshal(broker.payload, &got))
	assert.NotEmpty(t, got["messageId"])
	assert.Equal(t, "AUDIO", got["chunkType"])
	assert.Equal(t, domain.DeviceActionContinue, got["action"])
	assert.Equal(t, "LOW", got["severity"])
	assert.Equal(t, "No blocking alerts.", got["message"])
	_, hasDeviceID := got["deviceId"]
	_, hasTrackID := got["trackId"]
	assert.False(t, hasDeviceID)
	assert.False(t, hasTrackID)
}

func TestMQTTInstructionPublisher_RequiresConfiguredClient(t *testing.T) {
	t.Parallel()

	var publisher *MQTTInstructionPublisher
	err := publisher.SendInstruction(domain.DeviceInstruction{DeviceID: "d", TrackID: "t"})
	require.Error(t, err)
}

var _ mqtt.Client = (*stubPublishClient)(nil)
