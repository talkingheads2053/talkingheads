//go:build tinygo

package main

import (
	"machine"
	"time"
)

func main() {
	time.Sleep(2 * time.Second) // Give the system time to initialize before we start.

	uart := machine.Serial
	uart.Configure(machine.UARTConfig{})
	head = NewHeadLED()
	matrix = NewMatrix()
	svo, _ = NewServo()

	touchCommandWatchdog()

	go lights()
	go action()
	go commandWatchdog()

	for {
		if uart.Buffered() > 0 {
			data, _ := uart.ReadByte()

			switch data {
			case 13:
				// return key
				cmd := string(input)
				input = input[:0]
				if err := processCommand(cmd); err != nil {
					uart.Write([]byte("error: " + err.Error() + "\r\n"))
				}

			default:
				// just capture the character
				input = append(input, data)
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func lights() {
	for {
		switch getMode() {
		case StateSpeaking:
			head.Alternate(personalityColor(), personalityColorAlternate())
			matrix.StartVUMeter()
		case StateWaiting:
			head.SetColor(personalityColor())
			matrix.StartPulse()
		case StateHeadShaking:
			head.Red()
			matrix.StartPulse()
		default:
			head.SetColor(personalityColor())
			matrix.StartPulse()
		}

		time.Sleep(100 * time.Millisecond)
	}
}

func action() {
	var waitCounter, speakCounter int

	for {
		switch getMode() {
		case StateLooking:
			svo.SetAngle(targetAngle)
			angle = targetAngle
			setMode(StateWaiting)

		case StateSlowLooking:
			angle = movement(angle, targetAngle)
			svo.SetAngle(angle)
			if angle == targetAngle {
				setMode(StateStopped)
			}

		case StateWaiting:
			// Move a small amount once every 2-3 seconds.
			waitCounter--
			if waitCounter <= 0 {
				waitCounter = randomInt(8, 12)
				jitter := randomInt(-30, 31)
				svo.SetAngle(angle + jitter)
			}

		case StateSpeaking:
			// Move a small amount once every second or so.
			speakCounter--
			if speakCounter <= 0 {
				speakCounter = randomInt(2, 3)
				jitter := randomInt(-125, 126)
				svo.SetAngle(angle + jitter)
			}

		case StateHeadShaking:
			// Move back and forth 3 times to indicate "No".
			for i := 0; i < 3; i++ {
				svo.SetAngle(60)
				time.Sleep(300 * time.Millisecond)
				svo.SetAngle(120)
				time.Sleep(300 * time.Millisecond)
			}
			svo.SetAngle(90)
			angle = 90
			setMode(StateStopped)

		case StateStopped:
			svo.SetAngle(90)
			angle = 90
		}

		time.Sleep(250 * time.Millisecond)
	}
}

// commandWatchdog stops the firmware if no command has been received within
// commandTimeout. This guards against a stalled host leaving the head in an
// active state (e.g. moving while speaking) indefinitely.
func commandWatchdog() {
	for {
		time.Sleep(500 * time.Millisecond)
		if timeSinceLastCommand() < commandTimeout {
			continue
		}
		if getMode() != StateStopped {
			setMode(StateStopped)
		}
	}
}
