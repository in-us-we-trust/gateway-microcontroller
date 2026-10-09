package domain

import "strings"

func InstructionFromOrchestrator(deviceID string, data OrchestratorEventData) DeviceInstruction {
	action := strings.TrimSpace(data.FinalVerdict)
	if action == "" {
		action = DeviceActionContinue
	}

	severity := strings.TrimSpace(data.FinalSeverity)
	if severity == "" {
		severity = "NORMAL"
	}

	message := strings.TrimSpace(data.Message)
	if message == "" {
		message = defaultInstructionMessage(action)
	}

	return DeviceInstruction{
		TrackID:               data.TrackID,
		DeviceID:              deviceID,
		ChunkType:             data.ChunkType,
		Action:                action,
		Severity:              severity,
		Message:               message,
		AdditionalInformation: data.AdditionalInformation,
	}
}

func defaultInstructionMessage(action string) string {
	if action == DeviceActionStopDanger {
		return "Danger detected. Stop the active session."
	}
	return "No blocking alerts."
}
