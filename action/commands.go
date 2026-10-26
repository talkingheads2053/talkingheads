package main

import (
	"math/rand"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	input  = make([]byte, 0, 64)
	mode   = StateStopped
	modeMu sync.RWMutex

	head        *HeadLED
	matrix      *Matrix
	svo         ServoDevice
	angle       = 90
	targetAngle = 90

	// commandTimeout is the maximum time to wait for a new command before
	// stopping the action firmware as a safety measure.
	commandTimeout = 5 * time.Second

	lastCommandTime   = time.Now()
	lastCommandTimeMu sync.RWMutex
)

const (
	// Command constants for serial input.
	CommandLook       = "look"
	CommandSlowLook   = "slowlook"
	CommandDoubleTake = "doubletake"
	CommandWait       = "wait"
	CommandSpeak      = "speak"
	CommandHeadShake  = "headshake"
	CommandStop       = "stop"
	CommandTheme      = "theme"
	CommandDance      = "dance"

	// State constants for controlling the behavior of the main loop.
	StateLooking      = "looking"
	StateSlowLooking  = "slowlooking"
	StateDoubleTaking = "doubletaking"
	StateWaiting      = "waiting"
	StateSpeaking     = "speaking"
	StateHeadShaking  = "headshaking"
	StateDancing      = "dancing"
	StateStopped      = "stopped"
)

// processCommand parses and dispatches a received serial command.
func processCommand(cmd string) error {
	touchCommandWatchdog()

	parts := strings.SplitN(strings.TrimSpace(cmd), " ", 2)
	command := parts[0]

	switch command {
	case CommandLook, CommandSlowLook, CommandDoubleTake, CommandDance:
		if len(parts) != 2 {
			return errAngleRequired
		}
		a, err := strconv.Atoi(strings.TrimSpace(parts[1]))
		if err != nil {
			return errInvalidAngle
		}
		targetAngle = a
		setMode(stateForCommand(command))
	case CommandTheme:
		if len(parts) != 2 {
			return errColorRequired
		}
		name := strings.ToLower(strings.TrimSpace(parts[1]))
		if !isValidColor(name) {
			return errInvalidColor
		}
		PersonalityColor = name
	case CommandWait, CommandSpeak, CommandHeadShake, CommandStop:
		setMode(stateForCommand(command))
	default:
		setMode(StateStopped)
		return errUnknownCommand
	}
	return nil
}

func getMode() string {
	modeMu.RLock()
	defer modeMu.RUnlock()
	return mode
}

func setMode(m string) {
	modeMu.Lock()
	defer modeMu.Unlock()
	mode = m
}

// touchCommandWatchdog records the time the most recent command was received,
// resetting the command timeout countdown.
func touchCommandWatchdog() {
	lastCommandTimeMu.Lock()
	defer lastCommandTimeMu.Unlock()
	lastCommandTime = time.Now()
}

// timeSinceLastCommand returns the elapsed time since the last command was
// received.
func timeSinceLastCommand() time.Duration {
	lastCommandTimeMu.RLock()
	defer lastCommandTimeMu.RUnlock()
	return time.Since(lastCommandTime)
}

// Returns an int >= min, < max
func randomInt(min, max int) int {
	return min + rand.Intn(max-min)
}

const maxMovement = 15

// keep movement to maxMovement degrees at a time
func movement(current, target int) int {
	if current < target {
		if target-current > maxMovement {
			return current + maxMovement
		}
		return target
	} else if current > target {
		if current-target > maxMovement {
			return current - maxMovement
		}
		return target
	}
	return current
}

func stateForCommand(command string) string {
	switch command {
	case CommandLook:
		return StateLooking
	case CommandSlowLook:
		return StateSlowLooking
	case CommandDoubleTake:
		return StateDoubleTaking
	case CommandWait:
		return StateWaiting
	case CommandSpeak:
		return StateSpeaking
	case CommandHeadShake:
		return StateHeadShaking
	case CommandDance:
		return StateDancing
	case CommandStop:
		return StateStopped
	default:
		return "unknown"
	}
}
