package actor

import (
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

func TestHandleSpeakingStatus_Speaking(t *testing.T) {
	mc := &mockCommander{}
	l := &MQTTListener{name: "testactor", commander: mc}

	payload, _ := json.Marshal(commands.Speaking{Who: "testactor", Status: commands.StatusSpeaking})
	msg := &mockMessage{payload: payload, topic: "speaking/testactor"}

	l.handleSpeakingStatus(nil, msg)

	if len(mc.sentCmds) != 1 || mc.sentCmds[0] != "speak" {
		t.Errorf("commander.Send: got %v, want [\"speak\"]", mc.sentCmds)
	}
}

func TestHandleSpeakingStatus_Stopped(t *testing.T) {
	mc := &mockCommander{}
	l := &MQTTListener{name: "testactor", commander: mc}

	payload, _ := json.Marshal(commands.Speaking{Who: "testactor", Status: commands.StatusStopped})
	msg := &mockMessage{payload: payload, topic: "speaking/testactor"}

	l.handleSpeakingStatus(nil, msg)

	if len(mc.sentCmds) != 1 || mc.sentCmds[0] != "stop" {
		t.Errorf("commander.Send: got %v, want [\"stop\"]", mc.sentCmds)
	}
}

func TestHandleSpeakingStatus_InvalidPayload(t *testing.T) {
	mc := &mockCommander{}
	l := &MQTTListener{name: "testactor", commander: mc}

	msg := &mockMessage{payload: []byte("not valid json"), topic: "speaking/testactor"}

	l.handleSpeakingStatus(nil, msg)

	// No command expected for invalid JSON.
	if len(mc.sentCmds) != 0 {
		t.Errorf("expected no commands for invalid JSON, got %v", mc.sentCmds)
	}
}

func TestHandleSpeakingStatus_UnknownStatus(t *testing.T) {
	mc := &mockCommander{}
	l := &MQTTListener{name: "testactor", commander: mc}

	payload, _ := json.Marshal(commands.Speaking{Who: "testactor", Status: "unknown"})
	msg := &mockMessage{payload: payload, topic: "speaking/testactor"}

	l.handleSpeakingStatus(nil, msg)

	// No command expected for an unrecognised status.
	if len(mc.sentCmds) != 0 {
		t.Errorf("expected no commands for unknown status, got %v", mc.sentCmds)
	}
}

func TestHandleSpeakingStatus_OtherActorSpeaking(t *testing.T) {
	mc := &mockCommander{}
	l := &MQTTListener{name: "testactor", commander: mc}

	payload, _ := json.Marshal(commands.Speaking{Who: "otheractor", Status: commands.StatusSpeaking})
	msg := &mockMessage{payload: payload, topic: "speaking/otheractor"}

	l.handleSpeakingStatus(nil, msg)

	// No positions set — falls back to wait.
	if len(mc.sentCmds) != 1 || mc.sentCmds[0] != "wait" {
		t.Errorf("commander.Send: got %v, want [\"wait\"]", mc.sentCmds)
	}
}

func TestHandleSpeakingStatus_OtherActorSpeaking_WithPositions(t *testing.T) {
	mc := &mockCommander{}
	l := newTestListener("gemmai")
	l.commander = mc
	l.SetActorPositions([]string{"gemmai", "phineas"})

	payload, _ := json.Marshal(commands.Speaking{Who: "phineas", Status: commands.StatusSpeaking})
	msg := &mockMessage{payload: payload, topic: "speaking/phineas"}

	l.handleSpeakingStatus(nil, msg)

	// Positions set — must send a look command immediately.
	if len(mc.sentCmds) == 0 {
		t.Fatal("expected a look command, got none")
	}
	if !strings.HasPrefix(mc.sentCmds[0], "look ") {
		t.Errorf("expected command starting with 'look ', got %q", mc.sentCmds[0])
	}
}

func TestHandleSpeakingStatus_OtherActorStopped(t *testing.T) {
	mc := &mockCommander{}
	l := &MQTTListener{name: "testactor", commander: mc}

	payload, _ := json.Marshal(commands.Speaking{Who: "otheractor", Status: commands.StatusStopped})
	msg := &mockMessage{payload: payload, topic: "speaking/otheractor"}

	l.handleSpeakingStatus(nil, msg)

	if len(mc.sentCmds) != 1 || mc.sentCmds[0] != "stop" {
		t.Errorf("commander.Send: got %v, want [\"stop\"]", mc.sentCmds)
	}
}

// newTestListener builds a MQTTListener suitable for unit tests: no real MQTT
// connection and buffered channels.
func newTestListener(name string) *MQTTListener {
	return &MQTTListener{
		name:      name,
		commander: &mockCommander{},
		incoming:  make(chan string, 32),
		heard:     make(chan string, 64),
		done:      make(chan struct{}),
	}
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

func TestHandleSpeak_IgnoresPauseWord(t *testing.T) {
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

func TestMoreFunc_PauseWordsNotAddedToConversation(t *testing.T) {
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

// --- lookAngleFor ---

func TestLookAngleFor_TwoActors(t *testing.T) {
	// gemmai is left (index 0), phineas is right (index 1).
	l := newTestListener("gemmai")
	l.SetActorPositions([]string{"gemmai", "phineas"})

	angle, ok := l.lookAngleFor("phineas")
	if !ok {
		t.Fatal("expected ok=true")
	}
	// phineas has higher index → to gemmai's left → angle > 90
	if angle <= 90 {
		t.Errorf("expected angle > 90 for actor to the left, got %d", angle)
	}
}

func TestLookAngleFor_TwoActors_OtherDirection(t *testing.T) {
	l := newTestListener("phineas")
	l.SetActorPositions([]string{"gemmai", "phineas"})

	angle, ok := l.lookAngleFor("gemmai")
	if !ok {
		t.Fatal("expected ok=true")
	}
	// gemmai has lower index → to phineas's right → angle < 90
	if angle >= 90 {
		t.Errorf("expected angle < 90 for actor to the right, got %d", angle)
	}
}

func TestLookAngleFor_ThreeActors_Center(t *testing.T) {
	l := newTestListener("phineas")
	l.SetActorPositions([]string{"gemmai", "phineas", "qwentin"})

	leftAngle, ok := l.lookAngleFor("qwentin")
	if !ok {
		t.Fatal("expected ok=true for qwentin")
	}
	rightAngle, ok := l.lookAngleFor("gemmai")
	if !ok {
		t.Fatal("expected ok=true for gemmai")
	}
	// qwentin is to the left → angle > 90; gemmai is to the right → angle < 90
	if leftAngle <= 90 {
		t.Errorf("expected leftAngle > 90, got %d", leftAngle)
	}
	if rightAngle >= 90 {
		t.Errorf("expected rightAngle < 90, got %d", rightAngle)
	}
	// Symmetric around 90
	if leftAngle+rightAngle != 180 {
		t.Errorf("expected symmetric angles summing to 180, got %d+%d=%d", leftAngle, rightAngle, leftAngle+rightAngle)
	}
}

func TestLookAngleFor_EmptyPositions_ReturnsFalse(t *testing.T) {
	l := newTestListener("gemmai")
	_, ok := l.lookAngleFor("phineas")
	if ok {
		t.Error("expected ok=false with no positions set")
	}
}

func TestLookAngleFor_SinglePosition_ReturnsFalse(t *testing.T) {
	l := newTestListener("gemmai")
	l.SetActorPositions([]string{"gemmai"})
	_, ok := l.lookAngleFor("phineas")
	if ok {
		t.Error("expected ok=false with only one actor in positions")
	}
}

func TestLookAngleFor_UnknownSpeaker_ReturnsFalse(t *testing.T) {
	l := newTestListener("gemmai")
	l.SetActorPositions([]string{"gemmai", "phineas"})
	_, ok := l.lookAngleFor("unknown")
	if ok {
		t.Error("expected ok=false for speaker not in positions")
	}
}

// --- handleSpeak with actor positions ---

func TestHandleSpeak_SendsLook_WhenPositionsSet(t *testing.T) {
	mc := &mockCommander{}
	l := newTestListener("gemmai")
	l.commander = mc
	l.SetActorPositions([]string{"gemmai", "phineas"})

	payload, _ := json.Marshal(commands.Speak{Who: "phineas", What: "hello"})
	l.handleSpeak(nil, &mockMessage{payload: payload})

	// Expect a look command to have been sent.
	if len(mc.sentCmds) == 0 {
		t.Fatal("expected a look command, got none")
	}
	cmd := mc.sentCmds[0]
	if !strings.HasPrefix(cmd, "look ") {
		t.Errorf("expected command starting with 'look ', got %q", cmd)
	}
}

func TestHandleSpeak_NoLook_WhenPositionsNotSet(t *testing.T) {
	mc := &mockCommander{}
	l := newTestListener("gemmai")
	l.commander = mc
	// actorPositions not set

	payload, _ := json.Marshal(commands.Speak{Who: "phineas", What: "hello"})
	l.handleSpeak(nil, &mockMessage{payload: payload})

	for _, cmd := range mc.sentCmds {
		if strings.HasPrefix(cmd, "look") {
			t.Errorf("unexpected look command when positions not set: %q", cmd)
		}
	}
}
