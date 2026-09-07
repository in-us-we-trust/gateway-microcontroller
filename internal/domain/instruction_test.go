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

func TestInstructionFromOrchestrator_DefaultMessageWhenAssessmentsEmpty(t *testing.T) {
	instruction := InstructionFromOrchestrator("device-1", OrchestratorEventData{
		TrackID:   "track-1",
		ChunkType: "IMAGE",
	})
	if instruction.Action != DeviceActionContinue {
		t.Fatalf("action=%s", instruction.Action)
	}
	if instruction.Message != "No blocking alerts." {
		t.Fatalf("message=%s", instruction.Message)
	}

	stop := DeviceActionStopDanger
	danger := InstructionFromOrchestrator("device-1", OrchestratorEventData{
		TrackID:      "track-1",
		ChunkType:    "AUDIO",
		FinalVerdict: &stop,
	})
	if danger.Message != "Danger detected. Stop the active session." {
		t.Fatalf("message=%s", danger.Message)
	}
}

func TestDetectorError_ErrorIncludesCode(t *testing.T) {
	err := &DetectorError{Code: ErrCodeDetectorAnalysis, DetectorDetail: "upstream failed"}
	if err.Error() != "[DTE-001] upstream failed" {
		t.Fatalf("error=%s", err.Error())
	}
}

func TestDevicesControllerError_Error(t *testing.T) {
	err := &DevicesControllerError{StatusCode: 502, Body: []byte("nope")}
	if err.Error() != "devices controller error" {
		t.Fatalf("error=%s", err.Error())
	}
}
