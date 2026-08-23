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
	topic  string
}

func NewMQTTInstructionPublisher(client mqtt.Client, topic string) *MQTTInstructionPublisher {
	return &MQTTInstructionPublisher{client: client, topic: topic}
}

func (p *MQTTInstructionPublisher) SendInstruction(instruction domain.DeviceInstruction) error {
	if p == nil || p.client == nil {
		return fmt.Errorf("MQTT publisher is not configured")
	}

	payload, err := json.Marshal(instruction)
	if err != nil {
		return fmt.Errorf("marshal device instruction: %w", err)
	}

	token := p.client.Publish(p.topic, 1, false, payload)
	if !token.WaitTimeout(5 * time.Second) {
		return fmt.Errorf("publish device instruction to %s: timeout", p.topic)
	}
	if err := token.Error(); err != nil {
		return fmt.Errorf("publish device instruction to %s: %w", p.topic, err)
	}
	return nil
}
