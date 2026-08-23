package domain

import "strings"

var severityRank = map[string]int{
	"NORMAL": 1,
	"LOW":    2,
	"MEDIUM": 3,
	"HIGH":   4,
}

func InstructionFromOrchestrator(deviceID string, data OrchestratorEventData) DeviceInstruction {
	action := DeviceActionContinue
	if verdict := strings.TrimSpace(deref(data.FinalVerdict)); verdict != "" {
		action = verdict
	}

	severity := strings.TrimSpace(deref(data.FinalSeverity))
	message := ""
	bestRank := -1
	for _, assessment := range data.Assessments {
		if strings.EqualFold(strings.TrimSpace(assessment.Verdict), DeviceActionStopDanger) {
			action = DeviceActionStopDanger
		}
		rank := severityRank[strings.ToUpper(strings.TrimSpace(assessment.Severity))]
		if rank >= bestRank && strings.TrimSpace(assessment.Message) != "" {
			bestRank = rank
			message = assessment.Message
			if severity == "" {
				severity = assessment.Severity
			}
		}
	}
	if severity == "" {
		severity = "NORMAL"
	}
	if message == "" {
		message = defaultInstructionMessage(action)
	}

	return DeviceInstruction{
		TrackID:   data.TrackID,
		DeviceID:  deviceID,
		ChunkType: data.ChunkType,
		Action:    action,
		Severity:  severity,
		Message:   message,
	}
}

func deref(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func defaultInstructionMessage(action string) string {
	if action == DeviceActionStopDanger {
		return "Danger detected. Stop the active session."
	}
	return "No blocking alerts."
}
