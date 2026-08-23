package domain

import "testing"

func TestInstructionFromOrchestrator_UsesFinalVerdict(t *testing.T) {
	stop := DeviceActionStopDanger
	high := "HIGH"
	instruction := InstructionFromOrchestrator("device-1", OrchestratorEventData{
		TrackID:       "track-1",
		ChunkType:     "AUDIO",
		FinalVerdict:  &stop,
		FinalSeverity: &high,
		Assessments: []OrchestratorAssessment{
			{Status: "DANGER", Severity: "HIGH", Message: "Sensitive data requested.", Verdict: DeviceActionStopDanger},
		},
	})

	if instruction.Action != DeviceActionStopDanger {
		t.Fatalf("action=%s", instruction.Action)
	}
	if instruction.Message != "Sensitive data requested." {
		t.Fatalf("message=%s", instruction.Message)
	}
	if instruction.DeviceID != "device-1" || instruction.ChunkType != "AUDIO" {
		t.Fatalf("unexpected instruction: %+v", instruction)
	}
}

func TestInstructionFromOrchestrator_DefaultsToContinue(t *testing.T) {
	instruction := InstructionFromOrchestrator("device-1", OrchestratorEventData{
		TrackID:   "track-1",
		ChunkType: "IMAGE",
		Assessments: []OrchestratorAssessment{
			{Status: "SAFE", Severity: "LOW", Message: "No danger.", Verdict: DeviceActionContinue},
		},
	})

	if instruction.Action != DeviceActionContinue {
		t.Fatalf("action=%s", instruction.Action)
	}
	if instruction.Severity != "LOW" {
		t.Fatalf("severity=%s", instruction.Severity)
	}
}
