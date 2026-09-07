package client

import (
	"encoding/json"
	"fmt"
	"time"

	"gateway-microcontroller/internal/domain"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

type MQTTInstructionPublisher struct {
	client mqtt.Client
}

func NewMQTTInstructionPublisher(client mqtt.Client) *MQTTInstructionPublisher {
	return &MQTTInstructionPublisher{client: client}
}

func (p *MQTTInstructionPublisher) SendInstruction(instruction domain.DeviceInstruction) error {
	if p == nil || p.client == nil {
		return fmt.Errorf("MQTT publisher is not configured")
	}

	topic := domain.HomeEventTopicPath(instruction.DeviceID, instruction.TrackID, domain.MQTTActionAIResponse)
	payload, err := json.Marshal(instruction)
	if err != nil {
		return fmt.Errorf("marshal device instruction: %w", err)
	}

	token := p.client.Publish(topic, 1, false, payload)
	if !token.WaitTimeout(5 * time.Second) {
		return fmt.Errorf("publish device instruction to %s: timeout", topic)
	}
	if err := token.Error(); err != nil {
		return fmt.Errorf("publish device instruction to %s: %w", topic, err)
	}
	return nil
}
