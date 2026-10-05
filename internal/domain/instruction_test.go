package domain

import (
	"encoding/json"
	"testing"
)

func TestInstructionFromOrchestrator_UsesFinalVerdictAndMessage(t *testing.T) {
	instruction := InstructionFromOrchestrator("device-1", OrchestratorEventData{
		TrackID:       "track-1",
		ChunkType:     "IMAGE",
		FinalVerdict:  DeviceActionStopDanger,
		FinalSeverity: "PELIGROSO",
		Message:       "No se reconoce a la persona",
	})

	if instruction.Action != DeviceActionStopDanger {
		t.Fatalf("action=%s", instruction.Action)
	}
	if instruction.Severity != "PELIGROSO" {
		t.Fatalf("severity=%s", instruction.Severity)
	}
	if instruction.Message != "No se reconoce a la persona" {
		t.Fatalf("message=%s", instruction.Message)
	}
	if instruction.DeviceID != "device-1" || instruction.ChunkType != "IMAGE" || instruction.TrackID != "track-1" {
		t.Fatalf("unexpected instruction: %+v", instruction)
	}
}

func TestInstructionFromOrchestrator_DefaultsToContinue(t *testing.T) {
	instruction := InstructionFromOrchestrator("device-1", OrchestratorEventData{
		TrackID:       "track-1",
		ChunkType:     "IMAGE",
		FinalVerdict:  DeviceActionContinue,
		FinalSeverity: "LOW",
		Message:       "No danger.",
	})

	if instruction.Action != DeviceActionContinue {
		t.Fatalf("action=%s", instruction.Action)
	}
	if instruction.Severity != "LOW" {
		t.Fatalf("severity=%s", instruction.Severity)
	}
	if instruction.Message != "No danger." {
		t.Fatalf("message=%s", instruction.Message)
	}
}

func TestInstructionFromOrchestrator_DefaultMessageWhenEmpty(t *testing.T) {
	instruction := InstructionFromOrchestrator("device-1", OrchestratorEventData{
		TrackID:   "track-1",
		ChunkType: "IMAGE",
	})
	if instruction.Action != DeviceActionContinue {
		t.Fatalf("action=%s", instruction.Action)
	}
	if instruction.Severity != "NORMAL" {
		t.Fatalf("severity=%s", instruction.Severity)
	}
	if instruction.Message != "No blocking alerts." {
		t.Fatalf("message=%s", instruction.Message)
	}

	danger := InstructionFromOrchestrator("device-1", OrchestratorEventData{
		TrackID:      "track-1",
		ChunkType:    "AUDIO",
		FinalVerdict: DeviceActionStopDanger,
	})
	if danger.Message != "Danger detected. Stop the active session." {
		t.Fatalf("message=%s", danger.Message)
	}
}

func TestInstructionFromOrchestrator_PassesAdditionalInformationThrough(t *testing.T) {
	raw := json.RawMessage(`{"personName":"Lionel Messi","score":0.98}`)
	instruction := InstructionFromOrchestrator("device-1", OrchestratorEventData{
		TrackID:               "track-1",
		ChunkType:             "IMAGE",
		FinalVerdict:          "SAFE",
		FinalSeverity:         "NORMAL",
		Message:               "Persona reconocida: Lionel Messi",
		AdditionalInformation: raw,
	})

	if string(instruction.AdditionalInformation) != string(raw) {
		t.Fatalf("additionalInformation=%s", instruction.AdditionalInformation)
	}

	without := InstructionFromOrchestrator("device-1", OrchestratorEventData{
		TrackID:   "track-1",
		ChunkType: "IMAGE",
	})
	if len(without.AdditionalInformation) != 0 {
		t.Fatalf("additionalInformation=%s", without.AdditionalInformation)
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
