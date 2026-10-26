package actor

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hybridgroup/yzma/pkg/message"
	"github.com/talkingheads2053/talkingheads/pkg/commands"
)

// mockCommander records the last command sent.
type mockCommander struct {
	sentCmds []string
}

func (m *mockCommander) Send(cmd string) error {
	m.sentCmds = append(m.sentCmds, cmd)
	return nil
}

type mockMessage struct {
	payload []byte
	topic   string
}

func (m *mockMessage) Duplicate() bool   { return false }
func (m *mockMessage) Qos() byte         { return 0 }
func (m *mockMessage) Retained() bool    { return false }
func (m *mockMessage) Topic() string     { return m.topic }
func (m *mockMessage) MessageID() uint16 { return 0 }
func (m *mockMessage) Payload() []byte   { return m.payload }
func (m *mockMessage) Ack()              {}

// syncCommander records commands sent from any goroutine.
type syncCommander struct {
	mu   sync.Mutex
	cmds []string
}

func (c *syncCommander) Send(cmd string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cmds = append(c.cmds, cmd)
	return nil
}

func (c *syncCommander) sent() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.cmds...)
}

func speakingMsg(who, status string) *mockMessage {
	payload, _ := json.Marshal(commands.Speaking{Who: who, Status: status})
	return &mockMessage{payload: payload, topic: "speaking/" + who}
}

func TestHandleSpeakingStatus_RunsCuedCallsInOrder(t *testing.T) {
	l := newTestListener("gemmai")
	var ran []string
	l.SetMovement(nil, func(calls []message.ToolCall) {
		for _, c := range calls {
			ran = append(ran, c.Function.Arguments["command"])
		}
	})

	l.Cue([]message.ToolCall{makeToolCall("tool_movement", map[string]string{"command": "talk"})})
	l.takeCue(false)
	l.takeCue(false) // a phrase with no calls
	l.Cue([]message.ToolCall{makeToolCall("tool_movement", map[string]string{"command": "look"})})
	l.takeCue(false)

	l.handleSpeakingStatus(nil, speakingMsg("gemmai", commands.StatusSpeaking))
	if strings.Join(ran, ",") != "talk" {
		t.Fatalf("after first phrase ran %v, want [talk]", ran)
	}
	l.handleSpeakingStatus(nil, speakingMsg("gemmai", commands.StatusStopped))
	l.handleSpeakingStatus(nil, speakingMsg("gemmai", commands.StatusSpeaking))
	l.handleSpeakingStatus(nil, speakingMsg("gemmai", commands.StatusStopped))
	l.handleSpeakingStatus(nil, speakingMsg("gemmai", commands.StatusSpeaking))
	if strings.Join(ran, ",") != "talk,look" {
		t.Errorf("ran %v, want [talk look]", ran)
	}
}

func TestHandleSpeakingStatus_SkipsDroppedPhrase(t *testing.T) {
	l := newTestListener("gemmai")
	var ran []string
	l.SetMovement(nil, func(calls []message.ToolCall) {
		ran = append(ran, calls[0].Function.Arguments["command"])
	})

	l.Cue([]message.ToolCall{makeToolCall("tool_movement", map[string]string{"command": "talk"})})
	l.takeCue(false).dropped = true
	l.Cue([]message.ToolCall{makeToolCall("tool_movement", map[string]string{"command": "look"})})
	l.takeCue(false)

	l.handleSpeakingStatus(nil, speakingMsg("gemmai", commands.StatusSpeaking))
	if strings.Join(ran, ",") != "look" {
		t.Errorf("ran %v, want [look]", ran)
	}
}

func TestHandleSpeakingStatus_RepeatedSpeakingIsOnePhrase(t *testing.T) {
	l := newTestListener("gemmai")
	var ran []string
	l.SetMovement(nil, func(calls []message.ToolCall) {
		ran = append(ran, calls[0].Function.Arguments["command"])
	})

	l.Cue([]message.ToolCall{makeToolCall("tool_movement", map[string]string{"command": "talk"})})
	l.takeCue(false)
	l.Cue([]message.ToolCall{makeToolCall("tool_movement", map[string]string{"command": "look"})})
	l.takeCue(false)

	for range 5 {
		l.handleSpeakingStatus(nil, speakingMsg("gemmai", commands.StatusSpeaking))
	}
	if strings.Join(ran, ",") != "talk" {
		t.Errorf("ran %v, want [talk] until the first phrase stops", ran)
	}
}

func TestTakeCue_ThinkingLeavesCalls(t *testing.T) {
	l := newTestListener("gemmai")
	l.Cue([]message.ToolCall{makeToolCall("tool_movement", map[string]string{"command": "talk"})})

	if p := l.takeCue(true); len(p.calls) != 1 || p.calls[0].Function.Arguments["command"] != CommandTalk || p.calls[0].Function.Arguments["target"] != Audience {
		t.Errorf("thinking phrase got %v, want only its own talk call", p.calls)
	}
	if p := l.takeCue(false); len(p.calls) != 1 {
		t.Errorf("reply got %v, want the talk call", p.calls)
	}
}

func speakMsg(who, what string, thinking bool) *mockMessage {
	payload, _ := json.Marshal(commands.Speak{Who: who, What: what, Thinking: thinking})
	return &mockMessage{payload: payload, topic: "speak/" + who}
}

func TestCueHeard_WaitsForTheLineToPlay(t *testing.T) {
	l := newTestListener("gemmai")
	var ran []string
	l.SetMovement(nil, func(calls []message.ToolCall) {
		ran = append(ran, calls[0].Function.Arguments["command"])
	})
	listen := []message.ToolCall{makeToolCall("tool_movement", map[string]string{"command": "listen", "target": "phineas"})}

	l.handleSpeak(nil, speakMsg("phineas", "Hmm.", true))
	l.handleSpeak(nil, speakMsg("phineas", "Hello there.", false))
	l.CueHeard(listen)
	if len(ran) != 0 {
		t.Fatalf("ran %v before the line played", ran)
	}

	l.handleSpeakingStatus(nil, speakingMsg("phineas", commands.StatusSpeaking))
	l.handleSpeakingStatus(nil, speakingMsg("phineas", commands.StatusStopped))
	if strings.Join(ran, ",") != "listen" {
		t.Fatalf("ran %v, want [listen] for the thinking phrase", ran)
	}
	l.handleSpeakingStatus(nil, speakingMsg("phineas", commands.StatusSpeaking))
	if strings.Join(ran, ",") != "listen,listen" {
		t.Errorf("ran %v, want a second listen when the line plays", ran)
	}
}

func TestCueHeard_LateReaction(t *testing.T) {
	l := newTestListener("gemmai")
	var ran []string
	l.SetMovement(nil, func(calls []message.ToolCall) {
		ran = append(ran, calls[0].Function.Arguments["command"])
	})
	listen := []message.ToolCall{makeToolCall("tool_movement", map[string]string{"command": "listen", "target": "phineas"})}

	l.handleSpeak(nil, speakMsg("phineas", "Hello there.", false))
	l.handleSpeakingStatus(nil, speakingMsg("phineas", commands.StatusSpeaking))
	l.CueHeard(listen)
	if strings.Join(ran, ",") != "listen" {
		t.Fatalf("ran %v, want [listen] while the line plays", ran)
	}

	l.handleSpeakingStatus(nil, speakingMsg("phineas", commands.StatusStopped))
	l.CueHeard(listen)
	if strings.Join(ran, ",") != "listen" {
		t.Errorf("ran %v, a reaction after the line ended should be dropped", ran)
	}
}

func TestHandleSpeakingStatus_NoHardcodedMovement(t *testing.T) {
	mc := &syncCommander{}
	m := NewMovement(mc, MovementOptions{})
	l := newTestListener("gemmai")
	l.SetMovement(m, nil)

	l.handleSpeakingStatus(nil, speakingMsg("gemmai", commands.StatusSpeaking))
	l.handleSpeakingStatus(nil, speakingMsg("phineas", commands.StatusSpeaking))

	if got := mc.sent(); len(got) != 0 {
		t.Errorf("expected no commands, got %v", got)
	}
}

func TestHandleSpeakingStatus_RestsWhenIdle(t *testing.T) {
	mc := &syncCommander{}
	m := NewMovement(mc, MovementOptions{})
	m.restAfter = 10 * time.Millisecond
	l := newTestListener("gemmai")
	l.SetMovement(m, nil)

	l.handleSpeakingStatus(nil, speakingMsg("phineas", commands.StatusStopped))

	time.Sleep(50 * time.Millisecond)
	if got := mc.sent(); len(got) != 1 || got[0] != "stop" {
		t.Errorf("got %v, want [stop]", got)
	}
}

func TestHandleSpeakingStatus_MovementCancelsRest(t *testing.T) {
	mc := &syncCommander{}
	m := NewMovement(mc, MovementOptions{})
	m.restAfter = 20 * time.Millisecond
	l := newTestListener("gemmai")
	l.SetMovement(m, nil)

	l.handleSpeakingStatus(nil, speakingMsg("phineas", commands.StatusStopped))
	m.Call(context.Background(), makeToolCall("tool_movement", map[string]string{"command": "listen", "target": "audience"}))

	time.Sleep(60 * time.Millisecond)
	for _, c := range mc.sent() {
		if c == "stop" {
			t.Errorf("rest should be cancelled by a movement, got %v", mc.sent())
		}
	}
}

func TestHandleSpeakingStatus_InvalidPayload(t *testing.T) {
	l := newTestListener("gemmai")
	called := false
	l.SetMovement(nil, func([]message.ToolCall) { called = true })

	l.handleSpeakingStatus(nil, &mockMessage{payload: []byte("not valid json"), topic: "speaking/gemmai"})

	if called {
		t.Error("expected no calls for invalid JSON")
	}
}

// newTestListener builds a MQTTListener suitable for unit tests: no real MQTT
// connection and buffered channels.
func newTestListener(name string) *MQTTListener {
	l := &MQTTListener{
		name:     name,
		incoming: make(chan string, 32),
		heard:    make(chan string, 64),
		done:     make(chan struct{}),
	}
	l.speakCond = sync.NewCond(&l.speakMu)
	return l
}

// --- handleSpeak ---

func TestHandleSpeak_IgnoresSelf(t *testing.T) {
	l := newTestListener("gemmai")

	payload, _ := json.Marshal(commands.Speak{Who: "gemmai", What: "hello"})
	l.handleSpeak(nil, &mockMessage{payload: payload})

	select {
	case got := <-l.heard:
		t.Errorf("expected nothing in heard, got %q", got)
	default:
	}
}

func TestHandleSpeak_IgnoresThinkingPhrase(t *testing.T) {
	l := newTestListener("gemmai")

	payload, _ := json.Marshal(commands.Speak{Who: "phineas", What: "let me think...", Thinking: true})
	l.handleSpeak(nil, &mockMessage{payload: payload})

	select {
	case got := <-l.heard:
		t.Errorf("expected thinking phrase to be filtered, got %q", got)
	default:
	}
}

func TestHandleSpeak_RealSpeech_EnqueuedToHeard(t *testing.T) {
	l := newTestListener("gemmai")

	payload, _ := json.Marshal(commands.Speak{Who: "phineas", What: "The sky is blue."})
	l.handleSpeak(nil, &mockMessage{payload: payload})

	select {
	case got := <-l.heard:
		want := "phineas says: The sky is blue."
		if got != want {
			t.Errorf("heard: got %q, want %q", got, want)
		}
	case <-time.After(time.Second):
		t.Error("timed out waiting for heard message")
	}
}

func TestHandleSpeak_RealSpeech_NotInIncoming(t *testing.T) {
	l := newTestListener("gemmai")

	payload, _ := json.Marshal(commands.Speak{Who: "phineas", What: "hello there"})
	l.handleSpeak(nil, &mockMessage{payload: payload})

	select {
	case got := <-l.incoming:
		t.Errorf("heard speech must not go to incoming, got %q", got)
	default:
	}
}

func TestHandleSpeak_InvalidPayload(t *testing.T) {
	l := newTestListener("gemmai")

	l.handleSpeak(nil, &mockMessage{payload: []byte("not json")})

	select {
	case got := <-l.heard:
		t.Errorf("expected nothing in heard for invalid JSON, got %q", got)
	default:
	}
}

func TestHandleSpeak_ThinkingFalseExplicit_PassesThrough(t *testing.T) {
	l := newTestListener("gemmai")

	payload, _ := json.Marshal(commands.Speak{Who: "phineas", What: "I am ready.", Thinking: false})
	l.handleSpeak(nil, &mockMessage{payload: payload})

	select {
	case got := <-l.heard:
		want := "phineas says: I am ready."
		if got != want {
			t.Errorf("heard: got %q, want %q", got, want)
		}
	case <-time.After(time.Second):
		t.Error("timed out waiting for heard message")
	}
}

// --- drainHeard ---

func TestDrainHeard_Empty(t *testing.T) {
	l := newTestListener("gemmai")
	conv := []message.Message{}
	l.drainHeard(&conv)
	if len(conv) != 0 {
		t.Errorf("expected empty conversation, got %d messages", len(conv))
	}
}

func TestDrainHeard_MultipleMessages(t *testing.T) {
	l := newTestListener("gemmai")
	l.heard <- "phineas says: Hello."
	l.heard <- "phineas says: How are you?"

	conv := []message.Message{}
	l.drainHeard(&conv)

	if len(conv) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(conv))
	}
	for i, want := range []string{"phineas says: Hello.", "phineas says: How are you?"} {
		got := conv[i].GetContent()["content"].(string)
		if got != want {
			t.Errorf("conv[%d]: got %q, want %q", i, got, want)
		}
		if conv[i].GetRole() != "user" {
			t.Errorf("conv[%d] role: got %q, want \"user\"", i, conv[i].GetRole())
		}
	}
}

// --- MoreFunc ---

func TestMoreFunc_BlocksOnDirectionNotOnHeard(t *testing.T) {
	l := newTestListener("gemmai")

	// Put speech in heard — MoreFunc must not unblock on this alone.
	l.heard <- "phineas says: Something."

	moreFn := l.MoreFunc()
	conv := []message.Message{}

	done := make(chan struct{})
	go func() {
		moreFn(&conv)
		close(done)
	}()

	// MoreFunc should still be blocked after a short wait.
	select {
	case <-done:
		t.Error("MoreFunc returned before a Direction was sent")
	case <-time.After(80 * time.Millisecond):
		// expected — still waiting for a direction
	}

	// Now send a Direction to unblock it.
	l.incoming <- "what is consciousness?"
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Error("MoreFunc did not return after Direction was sent")
	}
}

func TestMoreFunc_HeardSpeechPrecedesDirection(t *testing.T) {
	l := newTestListener("gemmai")
	l.heard <- "phineas says: The answer is 42."

	moreFn := l.MoreFunc()
	conv := []message.Message{}

	go func() { l.incoming <- "what did phineas say?" }()
	moreFn(&conv)

	if len(conv) < 2 {
		t.Fatalf("expected at least 2 messages, got %d", len(conv))
	}
	// Heard speech must come before the Direction.
	first := conv[0].GetContent()["content"].(string)
	last := conv[len(conv)-1].GetContent()["content"].(string)
	if first != "phineas says: The answer is 42." {
		t.Errorf("first message: got %q, want heard speech", first)
	}
	if last != "what did phineas say?" {
		t.Errorf("last message: got %q, want direction", last)
	}
}

func TestMoreFunc_HeardArrivingDuringWaitIsIncluded(t *testing.T) {
	l := newTestListener("gemmai")

	moreFn := l.MoreFunc()
	conv := []message.Message{}

	// Send heard speech and direction concurrently.
	go func() {
		time.Sleep(20 * time.Millisecond)
		l.heard <- "phineas says: Just thought of something."
		l.incoming <- "respond to phineas"
	}()

	moreFn(&conv)

	// Both the heard speech and the direction must be in the conversation.
	var contents []string
	for _, m := range conv {
		contents = append(contents, m.GetContent()["content"].(string))
	}
	foundHeard := false
	foundDirection := false
	for _, c := range contents {
		if c == "phineas says: Just thought of something." {
			foundHeard = true
		}
		if c == "respond to phineas" {
			foundDirection = true
		}
	}
	if !foundHeard {
		t.Errorf("heard speech not found in conversation; got %v", contents)
	}
	if !foundDirection {
		t.Errorf("direction not found in conversation; got %v", contents)
	}
	// Direction must be the last message.
	last := conv[len(conv)-1].GetContent()["content"].(string)
	if last != "respond to phineas" {
		t.Errorf("direction must be last; got %q", last)
	}
}

// --- SetPreprocessCallback / preprocessing MoreFunc ---

func TestSetPreprocessCallback_NilIsAccepted(t *testing.T) {
	l := newTestListener("gemmai")
	l.SetPreprocessCallback(nil)
	if l.preprocessCB != nil {
		t.Error("expected preprocessCB to be nil after SetPreprocessCallback(nil)")
	}
}

func TestSetPreprocessCallback_Stored(t *testing.T) {
	l := newTestListener("gemmai")
	called := false
	cb := func(_ *[]message.Message) { called = true }
	l.SetPreprocessCallback(cb)
	if l.preprocessCB == nil {
		t.Fatal("expected preprocessCB to be set")
	}
	conv := []message.Message{}
	l.preprocessCB(&conv)
	if !called {
		t.Error("expected stored callback to be callable")
	}
}

func TestMoreFunc_WithPreprocessCB_CallsCallbackOnHeard(t *testing.T) {
	l := newTestListener("gemmai")

	var mu sync.Mutex
	var callCount int
	l.SetPreprocessCallback(func(_ *[]message.Message) {
		mu.Lock()
		callCount++
		mu.Unlock()
	})

	moreFn := l.MoreFunc()
	conv := []message.Message{}

	go func() {
		time.Sleep(20 * time.Millisecond)
		l.heard <- "phineas says: First."
		l.heard <- "phineas says: Second."
		time.Sleep(60 * time.Millisecond)
		l.incoming <- "what did phineas say?"
	}()

	moreFn(&conv)

	mu.Lock()
	n := callCount
	mu.Unlock()
	if n == 0 {
		t.Fatal("expected preprocessCB to be called at least once for heard speech")
	}
	if len(conv) == 0 {
		t.Fatal("expected conversation to be non-empty after MoreFunc")
	}
	last := conv[len(conv)-1].GetContent()["content"].(string)
	if last != "what did phineas say?" {
		t.Errorf("last message: got %q, want direction", last)
	}
}

func TestMoreFunc_WithPreprocessCB_HeardBeforeDirection(t *testing.T) {
	l := newTestListener("gemmai")
	l.SetPreprocessCallback(func(_ *[]message.Message) {})

	l.heard <- "phineas says: The answer is 42."

	moreFn := l.MoreFunc()
	conv := []message.Message{}
	go func() { l.incoming <- "what did phineas say?" }()
	moreFn(&conv)

	if len(conv) < 2 {
		t.Fatalf("expected at least 2 messages, got %d", len(conv))
	}
	first := conv[0].GetContent()["content"].(string)
	last := conv[len(conv)-1].GetContent()["content"].(string)
	if first != "phineas says: The answer is 42." {
		t.Errorf("first: got %q, want heard speech", first)
	}
	if last != "what did phineas say?" {
		t.Errorf("last: got %q, want direction", last)
	}
}

func TestMoreFunc_WithPreprocessCB_BlocksUntilDirection(t *testing.T) {
	l := newTestListener("gemmai")
	l.SetPreprocessCallback(func(_ *[]message.Message) {})

	moreFn := l.MoreFunc()
	conv := []message.Message{}

	done := make(chan struct{})
	go func() {
		moreFn(&conv)
		close(done)
	}()

	// Sending only heard speech must not unblock MoreFunc.
	l.heard <- "phineas says: Something."
	select {
	case <-done:
		t.Error("MoreFunc returned before a Direction was sent")
	case <-time.After(80 * time.Millisecond):
		// expected — still waiting for a Direction
	}

	l.incoming <- "direction"
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Error("MoreFunc did not return after Direction was sent")
	}
}

func TestMoreFunc_WithPreprocessCB_ClosedDone(t *testing.T) {
	l := newTestListener("gemmai")
	l.SetPreprocessCallback(func(_ *[]message.Message) {})
	moreFn := l.MoreFunc()

	conv := []message.Message{}
	done := make(chan struct{})
	go func() {
		moreFn(&conv)
		close(done)
	}()

	close(l.done)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Error("MoreFunc did not return after done was closed")
	}
}

func TestMoreFunc_ThinkingPhrasesNotAddedToConversation(t *testing.T) {
	l := newTestListener("gemmai")

	// Thinking phrases are filtered in handleSpeak before reaching heard;
	// simulate that by putting only real speech directly into heard.
	l.heard <- "phineas says: real sentence"

	moreFn := l.MoreFunc()
	conv := []message.Message{}
	go func() { l.incoming <- "direction" }()
	moreFn(&conv)

	for _, m := range conv {
		c := m.GetContent()["content"].(string)
		if c == "let me think..." || c == "one moment..." {
			t.Errorf("pause word leaked into conversation: %q", c)
		}
	}
}

// --- lastSpeaker tracking ---

func TestHandleSpeak_TracksLastSpeaker(t *testing.T) {
	l := newTestListener("gemmai")

	payload, _ := json.Marshal(commands.Speak{Who: "phineas", What: "hello"})
	l.handleSpeak(nil, &mockMessage{payload: payload})

	l.lastSpeakerMu.RLock()
	got := l.lastSpeaker
	l.lastSpeakerMu.RUnlock()

	if got != "phineas" {
		t.Errorf("lastSpeaker: got %q, want \"phineas\"", got)
	}
}

func TestHandleSpeak_ThinkingDoesNotUpdateLastSpeaker(t *testing.T) {
	l := newTestListener("gemmai")

	// seed a known lastSpeaker
	l.lastSpeakerMu.Lock()
	l.lastSpeaker = "phineas"
	l.lastSpeakerMu.Unlock()

	payload, _ := json.Marshal(commands.Speak{Who: "qwentin", What: "let me think...", Thinking: true})
	l.handleSpeak(nil, &mockMessage{payload: payload})

	l.lastSpeakerMu.RLock()
	got := l.lastSpeaker
	l.lastSpeakerMu.RUnlock()

	if got != "phineas" {
		t.Errorf("lastSpeaker must not change for thinking=true; got %q", got)
	}
}

// --- handleDirection with Respond=true ---

func TestHandleDirection_Respond_WithKnownLastSpeaker_EmptyWhat(t *testing.T) {
	l := newTestListener("gemmai")
	l.lastSpeakerMu.Lock()
	l.lastSpeaker = "phineas"
	l.lastSpeakerMu.Unlock()

	payload, _ := json.Marshal(commands.Direction{Who: "gemmai", What: "", Respond: true})
	l.handleDirection(nil, &mockMessage{payload: payload})

	select {
	case got := <-l.incoming:
		want := "Now respond directly to phineas."
		if got != want {
			t.Errorf("incoming: got %q, want %q", got, want)
		}
	case <-time.After(time.Second):
		t.Error("timed out waiting for incoming message")
	}
}

func TestHandleDirection_Respond_WithKnownLastSpeaker_NonEmptyWhat(t *testing.T) {
	l := newTestListener("gemmai")
	l.lastSpeakerMu.Lock()
	l.lastSpeaker = "phineas"
	l.lastSpeakerMu.Unlock()

	payload, _ := json.Marshal(commands.Direction{Who: "gemmai", What: "Keep it brief.", Respond: true})
	l.handleDirection(nil, &mockMessage{payload: payload})

	select {
	case got := <-l.incoming:
		want := "Keep it brief. Respond directly to phineas."
		if got != want {
			t.Errorf("incoming: got %q, want %q", got, want)
		}
	case <-time.After(time.Second):
		t.Error("timed out waiting for incoming message")
	}
}

func TestHandleDirection_Respond_UnknownLastSpeaker_FallsBackToWhat(t *testing.T) {
	l := newTestListener("gemmai")
	// lastSpeaker is "" (default)

	payload, _ := json.Marshal(commands.Direction{Who: "gemmai", What: "Say something.", Respond: true})
	l.handleDirection(nil, &mockMessage{payload: payload})

	select {
	case got := <-l.incoming:
		want := "Say something."
		if got != want {
			t.Errorf("incoming: got %q, want %q", got, want)
		}
	case <-time.After(time.Second):
		t.Error("timed out waiting for incoming message")
	}
}

func TestHandleDirection_NoRespond_IgnoresLastSpeaker(t *testing.T) {
	l := newTestListener("gemmai")
	l.lastSpeakerMu.Lock()
	l.lastSpeaker = "phineas"
	l.lastSpeakerMu.Unlock()

	payload, _ := json.Marshal(commands.Direction{Who: "gemmai", What: "Tell us a joke."})
	l.handleDirection(nil, &mockMessage{payload: payload})

	select {
	case got := <-l.incoming:
		want := "Tell us a joke."
		if got != want {
			t.Errorf("incoming: got %q, want %q", got, want)
		}
	case <-time.After(time.Second):
		t.Error("timed out waiting for incoming message")
	}
}

// --- handleSpeak ---

func TestHandleSpeak_SendsNoMovement(t *testing.T) {
	mc := &syncCommander{}
	l := newTestListener("gemmai")
	l.SetMovement(NewMovement(mc, MovementOptions{}), nil)

	payload, _ := json.Marshal(commands.Speak{Who: "phineas", What: "hello"})
	l.handleSpeak(nil, &mockMessage{payload: payload})

	if got := mc.sent(); len(got) != 0 {
		t.Errorf("expected no commands, got %v", got)
	}
}

func TestDirectionPending(t *testing.T) {
	l := newTestListener("gemmai")
	if l.DirectionPending() {
		t.Fatal("expected no direction pending")
	}
	l.incoming <- "hello"
	if !l.DirectionPending() {
		t.Error("expected a direction pending")
	}
}

// --- speakN tracking / WaitSpeakingDoneFunc ---

func TestHandleSpeakingStatus_Stopped_DecrementsSpeakN(t *testing.T) {
	l := newTestListener("gemmai")
	l.speakMu.Lock()
	l.speakN = 1
	l.speakMu.Unlock()

	payload, _ := json.Marshal(commands.Speaking{Who: "gemmai", Status: commands.StatusStopped})
	l.handleSpeakingStatus(nil, &mockMessage{payload: payload})

	l.speakMu.Lock()
	n := l.speakN
	l.speakMu.Unlock()

	if n != 0 {
		t.Errorf("expected speakN=0 after StatusStopped, got %d", n)
	}
}

func TestHandleSpeakingStatus_Stopped_DoesNotGoBelowZero(t *testing.T) {
	l := newTestListener("gemmai")
	// speakN starts at 0

	payload, _ := json.Marshal(commands.Speaking{Who: "gemmai", Status: commands.StatusStopped})
	l.handleSpeakingStatus(nil, &mockMessage{payload: payload})

	l.speakMu.Lock()
	n := l.speakN
	l.speakMu.Unlock()

	if n != 0 {
		t.Errorf("expected speakN=0, got %d", n)
	}
}

func TestWaitSpeakingDoneFunc_ReturnsImmediately_WhenZero(t *testing.T) {
	l := newTestListener("gemmai")
	// speakN is 0

	waitFn := l.WaitSpeakingDoneFunc()
	done := make(chan struct{})
	go func() {
		waitFn()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Error("WaitSpeakingDoneFunc did not return immediately when speakN=0")
	}
}

func TestWaitSpeakingDoneFunc_BlocksUntilSpeakNReachesZero(t *testing.T) {
	l := newTestListener("gemmai")
	l.speakMu.Lock()
	l.speakN = 1
	l.speakMu.Unlock()

	waitFn := l.WaitSpeakingDoneFunc()
	done := make(chan struct{})
	go func() {
		waitFn()
		close(done)
	}()

	// Must still be blocked after a short wait.
	select {
	case <-done:
		t.Fatal("WaitSpeakingDoneFunc returned before speakN reached zero")
	case <-time.After(60 * time.Millisecond):
	}

	// Simulate StatusStopped arriving.
	l.speakMu.Lock()
	l.speakN--
	l.speakCond.Broadcast()
	l.speakMu.Unlock()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Error("WaitSpeakingDoneFunc did not unblock after speakN reached zero")
	}
}

func TestWaitSpeakingDoneFunc_UnblocksWhenListenerClosed(t *testing.T) {
	l := newTestListener("gemmai")
	l.speakMu.Lock()
	l.speakN = 1
	l.speakMu.Unlock()

	waitFn := l.WaitSpeakingDoneFunc()
	done := make(chan struct{})
	go func() {
		waitFn()
		close(done)
	}()

	// Must still be blocked initially.
	select {
	case <-done:
		t.Fatal("WaitSpeakingDoneFunc returned before done channel closed")
	case <-time.After(60 * time.Millisecond):
	}

	l.Close()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Error("WaitSpeakingDoneFunc did not unblock after listener closed")
	}
}

func TestRemoveEmoji(t *testing.T) {
	cases := []struct{ in, want string }{
		{"great job 👍", "great job "},
		{"こんにちは、世界！🎉", "こんにちは、世界！"},
		{"漢字とカタカナ", "漢字とカタカナ"},
	}
	for _, c := range cases {
		if got := removeEmoji(c.in); got != c.want {
			t.Errorf("removeEmoji(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestRepeatsHeard(t *testing.T) {
	l := &MQTTListener{}
	l.SetRepeatGuard(10)
	l.rememberHeard("君らの愚かな愚か者は、タイニーゴーでコードを書けません。")

	if !l.RepeatsHeard("私はタイニーゴーでコードを書けません。") {
		t.Error("copied phrase not caught")
	}
	if l.RepeatsHeard("タイニーゴーは速いが、君たちの理解力は遅い。") {
		t.Error("short shared word caught")
	}

	l.SetRepeatGuard(0)
	if l.RepeatsHeard("私はタイニーゴーでコードを書けません。") {
		t.Error("guard is off but sentence caught")
	}
}

func TestLongestCommonRun(t *testing.T) {
	if n := longestCommonRun(letters("Hello, World!"), letters("hello world")); n != 10 {
		t.Errorf("got %d, want 10", n)
	}
	if n := longestCommonRun("", "abc"); n != 0 {
		t.Errorf("got %d, want 0", n)
	}
}

func TestHandleDirection_RespondTo_OverridesLastSpeaker(t *testing.T) {
	l := newTestListener("gemmai")
	l.lastSpeakerMu.Lock()
	l.lastSpeaker = "phineas"
	l.lastSpeakerMu.Unlock()

	payload, _ := json.Marshal(commands.Direction{Who: "gemmai", Respond: true, RespondTo: "qwentin"})
	l.handleDirection(nil, &mockMessage{payload: payload})

	select {
	case got := <-l.incoming:
		want := "Now respond directly to qwentin."
		if got != want {
			t.Errorf("incoming: got %q, want %q", got, want)
		}
	case <-time.After(time.Second):
		t.Error("timed out waiting for incoming message")
	}
}

func TestHandleDirection_Respond_JaEndsGuidance(t *testing.T) {
	l := newTestListener("gemmai")
	l.SetLang("ja")
	l.lastSpeakerMu.Lock()
	l.lastSpeaker = "qwentin"
	l.lastSpeakerMu.Unlock()

	payload, _ := json.Marshal(commands.Direction{Who: "gemmai", What: "短く", Respond: true})
	l.handleDirection(nil, &mockMessage{payload: payload})

	select {
	case got := <-l.incoming:
		want := "短く。qwentinに直接答えてください。"
		if got != want {
			t.Errorf("incoming: got %q, want %q", got, want)
		}
	case <-time.After(time.Second):
		t.Error("timed out waiting for incoming message")
	}
}

func TestHandleDirection_Respond_QuotesLastTurn(t *testing.T) {
	l := newTestListener("gemmai")
	speak := func(who, what string) {
		payload, _ := json.Marshal(commands.Speak{Who: who, What: what})
		l.handleSpeak(nil, &mockMessage{payload: payload})
		<-l.heard
	}
	speak("qwentin", "Old line.")
	l.handleSpeak(nil, &mockMessage{payload: mustJSON(commands.Speak{Who: "gemmai", What: "Mine."})})
	speak("qwentin", "I like Go.")
	speak("qwentin", "Obey me.")

	payload, _ := json.Marshal(commands.Direction{Who: "gemmai", Respond: true})
	l.handleDirection(nil, &mockMessage{payload: payload})

	want := `qwentin said: "I like Go. Obey me." Now respond directly to qwentin.`
	if got := <-l.incoming; got != want {
		t.Errorf("incoming: got %q, want %q", got, want)
	}
}

func TestHandleDirection_Respond_QuotesLastTurnJa(t *testing.T) {
	l := newTestListener("gemmai")
	l.SetLang("ja")
	payload, _ := json.Marshal(commands.Speak{Who: "qwentin", What: "コードが好きだ。"})
	l.handleSpeak(nil, &mockMessage{payload: payload})
	<-l.heard

	payload, _ = json.Marshal(commands.Direction{Who: "gemmai", Respond: true})
	l.handleDirection(nil, &mockMessage{payload: payload})

	want := "qwentinが言いました：「コードが好きだ。」それではqwentinに直接答えてください。"
	if got := <-l.incoming; got != want {
		t.Errorf("incoming: got %q, want %q", got, want)
	}
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

func TestHasSpeech(t *testing.T) {
	for raw, want := range map[string]bool{
		"":                                 false,
		lfmCall(`command="listen"`):        false,
		lfmCall(`command="listen"`) + "  ": false,
		lfmCallStart + "[tool_movement(":   false,
		lfmCall(`command="talk"`) + "Hi":   true,
		"Hi " + lfmCall(`command="talk"`):  true,
	} {
		if got := hasSpeech(raw); got != want {
			t.Errorf("hasSpeech(%q) = %v, want %v", raw, got, want)
		}
	}
}

func TestHandleDirection_CallsDirectionFunc(t *testing.T) {
	l := newTestListener("gemmai")
	called := false
	l.SetDirectionFunc(func() { called = true })

	payload, _ := json.Marshal(commands.Direction{Who: "gemmai", What: "hello"})
	l.handleDirection(nil, &mockMessage{payload: payload})

	if !called {
		t.Error("direction func not called")
	}
}

func TestHandleSpeakingStatus_WakesRestingHead(t *testing.T) {
	mc := &syncCommander{}
	m := NewMovement(mc, MovementOptions{Stage: Stage{Self: "gemmai", Positions: []string{"gemmai", "phineas"}}})
	m.restAfter = 10 * time.Millisecond
	l := newTestListener("gemmai")
	l.SetMovement(m, func(calls []message.ToolCall) {
		for _, c := range calls {
			m.Call(context.Background(), c)
		}
	})

	l.handleSpeakingStatus(nil, speakingMsg("phineas", commands.StatusSpeaking))
	l.handleSpeakingStatus(nil, speakingMsg("phineas", commands.StatusStopped))
	time.Sleep(50 * time.Millisecond)
	l.handleSpeakingStatus(nil, speakingMsg("phineas", commands.StatusSpeaking))

	want := "look 150,stop,look 150"
	if got := strings.Join(mc.sent(), ","); got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestHandleSpeakingStatus_PhraseStartCancelsRest(t *testing.T) {
	mc := &syncCommander{}
	m := NewMovement(mc, MovementOptions{})
	m.restAfter = 30 * time.Millisecond
	l := newTestListener("gemmai")
	l.SetMovement(m, func(calls []message.ToolCall) {
		for _, c := range calls {
			m.Call(context.Background(), c)
		}
	})

	l.handleSpeakingStatus(nil, speakingMsg("phineas", commands.StatusSpeaking))
	l.handleSpeakingStatus(nil, speakingMsg("phineas", commands.StatusStopped))
	l.handleSpeakingStatus(nil, speakingMsg("phineas", commands.StatusSpeaking))
	time.Sleep(60 * time.Millisecond)

	for _, c := range mc.sent() {
		if c == "stop" {
			t.Errorf("a new phrase should cancel the rest, got %v", mc.sent())
		}
	}
}
