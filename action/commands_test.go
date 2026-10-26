package main

import "testing"

// resetState resets the global command state before each test.
func resetState() {
	setMode(StateStopped)
	targetAngle = 90
}

func TestProcessCommand_Look(t *testing.T) {
	resetState()
	if err := processCommand("look 135"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if getMode() != StateLooking {
		t.Errorf("mode = %q, want %q", getMode(), StateLooking)
	}
	if targetAngle != 135 {
		t.Errorf("targetAngle = %d, want 135", targetAngle)
	}
}

func TestProcessCommand_LookMissingAngle(t *testing.T) {
	resetState()
	if err := processCommand("look"); err != errAngleRequired {
		t.Errorf("err = %v, want errAngleRequired", err)
	}
}

func TestProcessCommand_LookInvalidAngle(t *testing.T) {
	resetState()
	if err := processCommand("look abc"); err != errInvalidAngle {
		t.Errorf("err = %v, want errInvalidAngle", err)
	}
}

func TestProcessCommand_SlowLook(t *testing.T) {
	resetState()
	if err := processCommand("slowlook 45"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if getMode() != StateSlowLooking {
		t.Errorf("mode = %q, want %q", getMode(), StateSlowLooking)
	}
	if targetAngle != 45 {
		t.Errorf("targetAngle = %d, want 45", targetAngle)
	}
}

func TestProcessCommand_SlowLookMissingAngle(t *testing.T) {
	resetState()
	if err := processCommand("slowlook"); err != errAngleRequired {
		t.Errorf("err = %v, want errAngleRequired", err)
	}
}

func TestProcessCommand_Wait(t *testing.T) {
	resetState()
	if err := processCommand("wait"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if getMode() != StateWaiting {
		t.Errorf("mode = %q, want %q", getMode(), StateWaiting)
	}
}

func TestProcessCommand_Speak(t *testing.T) {
	resetState()
	if err := processCommand("speak"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if getMode() != StateSpeaking {
		t.Errorf("mode = %q, want %q", getMode(), StateSpeaking)
	}
}

func TestProcessCommand_Headshake(t *testing.T) {
	resetState()
	if err := processCommand("headshake"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if getMode() != StateHeadShaking {
		t.Errorf("mode = %q, want %q", getMode(), StateHeadShaking)
	}
}

func TestProcessCommand_Stop(t *testing.T) {
	resetState()
	if err := processCommand("stop"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if getMode() != StateStopped {
		t.Errorf("mode = %q, want %q", getMode(), StateStopped)
	}
}

func TestProcessCommand_UnknownCommand(t *testing.T) {
	resetState()
	if err := processCommand("jump"); err != errUnknownCommand {
		t.Errorf("err = %v, want errUnknownCommand", err)
	}
}

func TestProcessCommand_TrimsWhitespace(t *testing.T) {
	resetState()
	if err := processCommand("  look   90  "); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if targetAngle != 90 {
		t.Errorf("targetAngle = %d, want 90", targetAngle)
	}
}

func TestMovement_TowardTargetLargeGap(t *testing.T) {
	result := movement(90, 130)
	if result != 90+maxMovement {
		t.Errorf("movement(90, 130) = %d, want %d", result, 90+maxMovement)
	}
}

func TestMovement_TowardTargetSmallGap(t *testing.T) {
	result := movement(90, 95)
	if result != 95 {
		t.Errorf("movement(90, 95) = %d, want 95", result)
	}
}

func TestMovement_AwayFromTargetLargeGap(t *testing.T) {
	result := movement(90, 50)
	if result != 90-maxMovement {
		t.Errorf("movement(90, 50) = %d, want %d", result, 90-maxMovement)
	}
}

func TestMovement_AwayFromTargetSmallGap(t *testing.T) {
	result := movement(90, 85)
	if result != 85 {
		t.Errorf("movement(90, 85) = %d, want 85", result)
	}
}

func TestMovement_AtTarget(t *testing.T) {
	result := movement(90, 90)
	if result != 90 {
		t.Errorf("movement(90, 90) = %d, want 90", result)
	}
}

func TestProcessCommand_Theme(t *testing.T) {
	resetState()
	prev := PersonalityColor
	defer func() { PersonalityColor = prev }()
	if err := processCommand("theme green"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if PersonalityColor != "green" {
		t.Errorf("PersonalityColor = %q, want %q", PersonalityColor, "green")
	}
}

func TestProcessCommand_ThemeMixedCase(t *testing.T) {
	resetState()
	prev := PersonalityColor
	defer func() { PersonalityColor = prev }()
	if err := processCommand("theme  Purple "); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if PersonalityColor != "purple" {
		t.Errorf("PersonalityColor = %q, want %q", PersonalityColor, "purple")
	}
}

func TestProcessCommand_ThemeMissingColor(t *testing.T) {
	resetState()
	if err := processCommand("theme"); err != errColorRequired {
		t.Errorf("err = %v, want errColorRequired", err)
	}
}

func TestProcessCommand_ThemeInvalidColor(t *testing.T) {
	resetState()
	if err := processCommand("theme chartreuse"); err != errInvalidColor {
		t.Errorf("err = %v, want errInvalidColor", err)
	}
}

func TestProcessCommand_DoubleTake(t *testing.T) {
	resetState()
	if err := processCommand("doubletake 30"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if getMode() != StateDoubleTaking {
		t.Errorf("mode = %q, want %q", getMode(), StateDoubleTaking)
	}
	if targetAngle != 30 {
		t.Errorf("targetAngle = %d, want 30", targetAngle)
	}
}

func TestProcessCommand_DoubleTakeMissingAngle(t *testing.T) {
	resetState()
	if err := processCommand("doubletake"); err != errAngleRequired {
		t.Errorf("err = %v, want errAngleRequired", err)
	}
}

func TestProcessCommand_Dance(t *testing.T) {
	resetState()
	if err := processCommand("dance 60"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if getMode() != StateDancing {
		t.Errorf("mode = %q, want %q", getMode(), StateDancing)
	}
	if targetAngle != 60 {
		t.Errorf("targetAngle = %d, want 60", targetAngle)
	}
}

func TestProcessCommand_DanceMissingAngle(t *testing.T) {
	resetState()
	if err := processCommand("dance"); err != errAngleRequired {
		t.Errorf("err = %v, want errAngleRequired", err)
	}
}

func TestProcessCommand_DanceInvalidAngle(t *testing.T) {
	resetState()
	if err := processCommand("dance left"); err != errInvalidAngle {
		t.Errorf("err = %v, want errInvalidAngle", err)
	}
}
