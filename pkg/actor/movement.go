package actor

import (
	"context"
	"fmt"
	"io"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/hybridgroup/yzma/pkg/message"
	"github.com/talkingheads2053/talkingheads/pkg/commands"
	"go.bug.st/serial"
)

// Commander is the interface used by Movement to send action commands.
// Two implementations are provided: LogCommander (console) and SerialCommander.
type Commander interface {
	Send(cmd string) error
}

// LogCommander writes commands to the standard logger.
type LogCommander struct{}

func (l *LogCommander) Send(cmd string) error {
	log.Printf("action: %s", cmd)
	return nil
}

// SerialCommander sends commands to a microcontroller over a serial port.
type SerialCommander struct {
	port io.ReadWriteCloser
}

// NewSerialCommander opens the named serial port at the given baud rate.
func NewSerialCommander(portName string, baudRate int) (*SerialCommander, error) {
	port, err := serial.Open(portName, &serial.Mode{BaudRate: baudRate})
	if err != nil {
		return nil, fmt.Errorf("opening serial port %s: %w", portName, err)
	}
	return &SerialCommander{port: port}, nil
}

// Send writes the command followed by a carriage return to the serial port.
func (s *SerialCommander) Send(cmd string) error {
	_, err := fmt.Fprintf(s.port, "%s\r", cmd)
	return err
}

// Close closes the underlying serial port.
func (s *SerialCommander) Close() error {
	return s.port.Close()
}

// Stage is this actor's name and the left to right order of all actors as
// seen from the audience.
type Stage struct {
	Self      string
	Positions []string
}

// Audience is the target for looking straight out at the audience.
const Audience = "audience"

// AngleFor returns the servo angle for facing target. The audience and unknown
// targets are straight ahead at 90.
func (s Stage) AngleFor(target string) int {
	if angle, ok := s.actorAngle(target); ok {
		return angle
	}
	return 90
}

// actorAngle returns the angle (30–150) for facing another actor. 0 is full
// right, 90 is center and 180 is full left from this actor's view.
func (s Stage) actorAngle(target string) (int, bool) {
	if len(s.Positions) < 2 {
		return 0, false
	}
	myIdx, targetIdx := -1, -1
	for i, name := range s.Positions {
		if name == s.Self {
			myIdx = i
		}
		if strings.EqualFold(name, target) {
			targetIdx = i
		}
	}
	if myIdx < 0 || targetIdx < 0 || myIdx == targetIdx {
		return 0, false
	}
	n := len(s.Positions)
	diff := targetIdx - myIdx
	// Closer actors get a smaller turn.
	spread := abs(diff) * 30 / (n - 1)
	if diff > 0 {
		return min(120+spread, 150), true
	}
	return max(60-spread, 30), true
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// Movement commands.
const (
	CommandTalk       = "talk"
	CommandListen     = "listen"
	CommandLook       = "look"
	CommandSlowLook   = "slowlook"
	CommandDoubleTake = "doubletake"
	CommandFaceCrowd  = "facecrowd"
	CommandHeadShake  = "headshake"
)

// movementCall builds a tool_movement call.
func movementCall(command, target string) message.ToolCall {
	return message.ToolCall{
		Type: "function",
		Function: message.ToolFunction{
			Name:      "tool_movement",
			Arguments: map[string]string{"command": command, "target": target},
		},
	}
}

// interjections are the special movements that run at most once per show.
var interjections = map[string]bool{
	CommandSlowLook:   true,
	CommandDoubleTake: true,
	CommandFaceCrowd:  true,
	CommandHeadShake:  true,
}

const (
	// DefaultInterjectionGap is the shortest time between two interjections.
	DefaultInterjectionGap = 3 * time.Minute
	// keepaliveInterval is how often the current mode is sent again. The
	// firmware stops after 5 seconds without a command.
	keepaliveInterval = 2 * time.Second
	// restDelay is how long a head waits with nothing to do before it rests.
	restDelay = time.Second
	// danceTimeout ends a dance when the beats stop arriving.
	danceTimeout = 3 * time.Second
	// danceLeft and danceRight are the angles a dancing head swings between.
	danceLeft  = 120
	danceRight = 60
)

// MovementOptions configures a Movement.
type MovementOptions struct {
	Stage           Stage
	InterjectionGap time.Duration
}

// Movement is the tool that moves the head. The model picks a mode or gaze
// with a target, and the firmware makes the small motions within a mode.
type Movement struct {
	name      string
	commander Commander
	stage     Stage
	gap       time.Duration
	restAfter time.Duration
	keepEvery time.Duration
	danceFor  time.Duration
	now       func() time.Time

	mu          sync.Mutex
	used        map[string]bool
	lastSpecial time.Time
	mode        string
	rest        *time.Timer
	dancing     bool
	danceEnd    *time.Timer
	stop        chan struct{}
	stopOnce    sync.Once
}

// NewMovement creates a Movement. If commander is nil, a LogCommander is used.
func NewMovement(commander Commander, opts MovementOptions) *Movement {
	if commander == nil {
		commander = &LogCommander{}
	}
	if opts.InterjectionGap <= 0 {
		opts.InterjectionGap = DefaultInterjectionGap
	}
	return &Movement{
		name:      "tool_movement",
		commander: commander,
		stage:     opts.Stage,
		gap:       opts.InterjectionGap,
		restAfter: restDelay,
		keepEvery: keepaliveInterval,
		danceFor:  danceTimeout,
		now:       time.Now,
		used:      make(map[string]bool),
		stop:      make(chan struct{}),
	}
}

// RegisterMovement creates a new instance of the Movement tool and loads it
// into the provided tools map. If commander is nil, a LogCommander is used.
func RegisterMovement(tools map[string]Tool, commander Commander) message.ToolDefinition {
	return NewMovement(commander, MovementOptions{}).Register(tools)
}

// Register loads the Movement into tools and returns its tool document.
func (m *Movement) Register(tools map[string]Tool) message.ToolDefinition {
	tools[m.name] = m
	return m.toolDocument()
}

func (m *Movement) toolDocument() message.ToolDefinition {
	targets := append([]string{Audience}, m.stage.Positions...)
	return message.ToolDefinition{
		Type: "function",
		Function: message.ToolFunctionDefinition{
			Name:        m.name,
			Description: "Move the head of the actor.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"command": map[string]interface{}{
						"type": "string",
						"description": "talk (move while speaking), listen (face the speaker while listening), look (turn to the target). " +
							"Rare special moves, each at most once per show: slowlook, doubletake, facecrowd, headshake.",
						"enum": []string{CommandTalk, CommandListen, CommandLook, CommandSlowLook, CommandDoubleTake, CommandFaceCrowd, CommandHeadShake},
					},
					"target": map[string]interface{}{
						"type":        "string",
						"description": "Who to face: " + strings.Join(targets, ", ") + ".",
					},
				},
				"required": []string{"command"},
			},
		},
	}
}

// Call runs the movement requested by the model.
func (m *Movement) Call(ctx context.Context, toolCall message.ToolCall) (resp string) {
	defer func() {
		if r := recover(); r != nil {
			resp = toolErrorResponse(fmt.Errorf("%s", r))
		}
	}()

	command := strings.ToLower(strings.TrimSpace(toolCall.Function.Arguments["command"]))
	if command == "" {
		return toolErrorResponse(fmt.Errorf("missing required parameter: command"))
	}
	target := strings.ToLower(strings.TrimSpace(toolCall.Function.Arguments["target"]))

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.dancing {
		return toolSuccessResponse("command", command, "skipped", true)
	}
	m.cancelRest()

	if interjections[command] && !m.allowSpecial(command) {
		log.Printf("interjection %s refused", command)
		switch command {
		case CommandHeadShake:
			return toolSuccessResponse("command", command, "skipped", true)
		case CommandFaceCrowd:
			command, target = CommandLook, Audience
		default:
			command = CommandLook
		}
	}

	angle := m.stage.AngleFor(target)
	var cmds []string
	mode := m.mode
	switch command {
	case CommandTalk:
		if target != "" {
			cmds = append(cmds, fmt.Sprintf("look %d", angle))
		}
		cmds = append(cmds, "speak")
		mode = "speak"
	case CommandListen, CommandLook:
		cmds = append(cmds, fmt.Sprintf("look %d", angle))
		if command == CommandListen || mode == "" {
			mode = "wait"
		}
	case CommandSlowLook, CommandDoubleTake:
		cmds = append(cmds, fmt.Sprintf("%s %d", command, angle))
		mode = "wait"
	case CommandFaceCrowd:
		cmds = append(cmds, "slowlook 90")
		mode = "wait"
	case CommandHeadShake:
		cmds = append(cmds, "headshake")
	default:
		return toolErrorResponse(fmt.Errorf("unknown command: %s", command))
	}

	for _, c := range cmds {
		if err := m.commander.Send(c); err != nil {
			return toolErrorResponse(fmt.Errorf("sending command: %w", err))
		}
	}
	m.mode = mode
	if interjections[command] {
		m.used[command] = true
		m.lastSpecial = m.now()
	}
	return toolSuccessResponse("command", command, "target", target)
}

// allowSpecial reports whether an interjection may run now. Each one runs at
// most once per show, with a gap between any two.
func (m *Movement) allowSpecial(command string) bool {
	if m.used[command] {
		return false
	}
	return m.lastSpecial.IsZero() || m.now().Sub(m.lastSpecial) >= m.gap
}

// Keepalive sends the current mode again every couple of seconds until Close.
func (m *Movement) Keepalive() {
	t := time.NewTicker(m.keepEvery)
	defer t.Stop()
	for {
		select {
		case <-m.stop:
			return
		case <-t.C:
			m.mu.Lock()
			if m.mode != "" && !m.dancing {
				_ = m.commander.Send(m.mode)
			}
			m.mu.Unlock()
		}
	}
}

// RestSoon stops the head after a short delay unless another movement comes first.
func (m *Movement) RestSoon() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.dancing {
		return
	}
	m.cancelRest()
	m.rest = time.AfterFunc(m.restAfter, m.Rest)
}

// Wake cancels a pending rest and reports whether the head is resting.
func (m *Movement) Wake() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cancelRest()
	return m.mode == "" && !m.dancing
}

// Rest stops the head now.
func (m *Movement) Rest() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.dancing {
		return
	}
	m.mode = ""
	_ = m.commander.Send("stop")
}

// Dance turns the head left or right for one beat. Other movement waits
// until StopDance or until the beats stop arriving.
func (m *Movement) Dance(move string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cancelRest()
	m.dancing = true
	angle := danceLeft
	if move == commands.MoveRight {
		angle = danceRight
	}
	_ = m.commander.Send(fmt.Sprintf("dance %d", angle))
	if m.danceEnd != nil {
		m.danceEnd.Stop()
	}
	m.danceEnd = time.AfterFunc(m.danceFor, m.StopDance)
}

// StopDance ends a dance and rests the head.
func (m *Movement) StopDance() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.dancing {
		return
	}
	m.dancing = false
	if m.danceEnd != nil {
		m.danceEnd.Stop()
		m.danceEnd = nil
	}
	m.mode = ""
	_ = m.commander.Send("stop")
}

func (m *Movement) cancelRest() {
	if m.rest != nil {
		m.rest.Stop()
		m.rest = nil
	}
}

// Close stops the keepalive and any pending rest.
func (m *Movement) Close() {
	m.stopOnce.Do(func() {
		close(m.stop)
		m.mu.Lock()
		m.cancelRest()
		if m.danceEnd != nil {
			m.danceEnd.Stop()
		}
		m.mu.Unlock()
	})
}
