package actor

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/hybridgroup/yzma/pkg/message"
)

func TestRegisterMovement_AddsToMap(t *testing.T) {
	tools := make(map[string]Tool)
	RegisterMovement(tools, nil)

	if _, ok := tools["tool_movement"]; !ok {
		t.Fatal("expected tool_movement to be registered in tools map")
	}
}

func TestRegisterMovement_DocumentStructure(t *testing.T) {
	tools := make(map[string]Tool)
	doc := NewMovement(nil, MovementOptions{Stage: Stage{Self: "gemmai", Positions: []string{"gemmai", "phineas"}}}).Register(tools)

	if doc.Function.Name != "tool_movement" {
		t.Errorf("function name: got %v, want %q", doc.Function.Name, "tool_movement")
	}
	props, ok := doc.Function.Parameters["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("properties field is not map[string]interface{}")
	}
	if _, ok := props["command"]; !ok {
		t.Error("expected 'command' property in tool document")
	}
	target, ok := props["target"].(map[string]interface{})
	if !ok {
		t.Fatal("expected 'target' property in tool document")
	}
	if desc := target["description"].(string); !strings.Contains(desc, "audience") || !strings.Contains(desc, "phineas") {
		t.Errorf("target description should list the targets, got %q", desc)
	}
}

func makeToolCall(name string, args map[string]string) message.ToolCall {
	return message.ToolCall{
		Type: "function",
		Function: message.ToolFunction{
			Name:      name,
			Arguments: args,
		},
	}
}

func move(m *Movement, args map[string]string) string {
	return m.Call(context.Background(), makeToolCall("tool_movement", args))
}

func newTestMovement() (*Movement, *syncCommander) {
	mc := &syncCommander{}
	m := NewMovement(mc, MovementOptions{
		Stage:           Stage{Self: "phineas", Positions: []string{"gemmai", "phineas", "qwentin"}},
		InterjectionGap: time.Minute,
	})
	return m, mc
}

func TestMovementCall_Commands(t *testing.T) {
	tests := []struct {
		args map[string]string
		want []string
	}{
		{map[string]string{"command": "talk"}, []string{"speak"}},
		{map[string]string{"command": "talk", "target": "audience"}, []string{"look 90", "speak"}},
		{map[string]string{"command": "listen", "target": "qwentin"}, []string{"look 135"}},
		{map[string]string{"command": "look", "target": "gemmai"}, []string{"look 45"}},
		{map[string]string{"command": "look", "target": "nobody"}, []string{"look 90"}},
		{map[string]string{"command": "slowlook", "target": "gemmai"}, []string{"slowlook 45"}},
		{map[string]string{"command": "doubletake", "target": "qwentin"}, []string{"doubletake 135"}},
		{map[string]string{"command": "facecrowd"}, []string{"slowlook 90"}},
		{map[string]string{"command": "headshake"}, []string{"headshake"}},
	}
	for _, tt := range tests {
		m, mc := newTestMovement()
		if resp := move(m, tt.args); !strings.Contains(resp, "SUCCESS") {
			t.Errorf("%v: got %s", tt.args, resp)
		}
		if got := mc.sent(); strings.Join(got, ",") != strings.Join(tt.want, ",") {
			t.Errorf("%v: sent %v, want %v", tt.args, got, tt.want)
		}
	}
}

func TestMovementCall_MissingCommand(t *testing.T) {
	m, _ := newTestMovement()
	if resp := move(m, map[string]string{}); !strings.Contains(resp, "FAILED") {
		t.Errorf("expected FAILED in response, got: %s", resp)
	}
}

func TestMovementCall_UnknownCommand(t *testing.T) {
	m, _ := newTestMovement()
	if resp := move(m, map[string]string{"command": "dance"}); !strings.Contains(resp, "FAILED") {
		t.Errorf("expected FAILED in response, got: %s", resp)
	}
}

func TestMovementCall_InterjectionOncePerShow(t *testing.T) {
	m, mc := newTestMovement()
	m.gap = 0

	move(m, map[string]string{"command": "doubletake", "target": "gemmai"})
	move(m, map[string]string{"command": "doubletake", "target": "gemmai"})

	want := "doubletake 45,look 45"
	if got := strings.Join(mc.sent(), ","); got != want {
		t.Errorf("sent %s, want %s", got, want)
	}
}

func TestMovementCall_InterjectionGap(t *testing.T) {
	m, mc := newTestMovement()
	now := time.Now()
	m.now = func() time.Time { return now }

	move(m, map[string]string{"command": "headshake"})
	move(m, map[string]string{"command": "facecrowd"})
	now = now.Add(2 * time.Minute)
	move(m, map[string]string{"command": "slowlook", "target": "qwentin"})

	want := "headshake,look 90,slowlook 135"
	if got := strings.Join(mc.sent(), ","); got != want {
		t.Errorf("sent %s, want %s", got, want)
	}
}

func TestMovementCall_RefusedHeadshakeDoesNothing(t *testing.T) {
	m, mc := newTestMovement()
	m.gap = 0

	move(m, map[string]string{"command": "headshake"})
	resp := move(m, map[string]string{"command": "headshake"})

	if !strings.Contains(resp, "SUCCESS") {
		t.Errorf("expected a refused headshake to be skipped quietly, got %s", resp)
	}
	if got := mc.sent(); len(got) != 1 {
		t.Errorf("sent %v, want one headshake", got)
	}
}

func TestMovementKeepalive(t *testing.T) {
	m, mc := newTestMovement()
	m.keepEvery = 10 * time.Millisecond
	go m.Keepalive()
	defer m.Close()

	move(m, map[string]string{"command": "talk"})
	time.Sleep(55 * time.Millisecond)

	speaks := 0
	for _, c := range mc.sent() {
		if c == "speak" {
			speaks++
		}
	}
	if speaks < 3 {
		t.Errorf("expected the talk mode to be sent again, got %v", mc.sent())
	}
}

func TestMovementRest(t *testing.T) {
	m, mc := newTestMovement()
	m.keepEvery = 10 * time.Millisecond
	go m.Keepalive()
	defer m.Close()

	move(m, map[string]string{"command": "talk"})
	m.Rest()
	n := len(mc.sent())
	time.Sleep(40 * time.Millisecond)

	if got := mc.sent(); len(got) != n || got[n-1] != "stop" {
		t.Errorf("expected stop and no keepalive after rest, got %v", got)
	}
}

// --- Stage ---

func TestStageAngleFor_TwoActors(t *testing.T) {
	s := Stage{Self: "gemmai", Positions: []string{"gemmai", "phineas"}}
	if angle := s.AngleFor("phineas"); angle <= 90 {
		t.Errorf("expected angle > 90 for actor to the left, got %d", angle)
	}
	s.Self = "phineas"
	if angle := s.AngleFor("gemmai"); angle >= 90 {
		t.Errorf("expected angle < 90 for actor to the right, got %d", angle)
	}
}

func TestStageAngleFor_ThreeActorsSymmetric(t *testing.T) {
	s := Stage{Self: "phineas", Positions: []string{"gemmai", "phineas", "qwentin"}}
	left, right := s.AngleFor("qwentin"), s.AngleFor("gemmai")
	if left <= 90 || right >= 90 || left+right != 180 {
		t.Errorf("expected symmetric angles, got %d and %d", left, right)
	}
}

func TestStageAngleFor_FallsBackToAudience(t *testing.T) {
	tests := []struct {
		stage  Stage
		target string
	}{
		{Stage{Self: "gemmai"}, "phineas"},
		{Stage{Self: "gemmai", Positions: []string{"gemmai"}}, "phineas"},
		{Stage{Self: "gemmai", Positions: []string{"gemmai", "phineas"}}, "unknown"},
		{Stage{Self: "gemmai", Positions: []string{"gemmai", "phineas"}}, "gemmai"},
		{Stage{Self: "gemmai", Positions: []string{"gemmai", "phineas"}}, Audience},
	}
	for _, tt := range tests {
		if angle := tt.stage.AngleFor(tt.target); angle != 90 {
			t.Errorf("%+v %q: got %d, want 90", tt.stage, tt.target, angle)
		}
	}
}

// --- Dance ---

func TestMovementDance_SwingsOnEachBeat(t *testing.T) {
	m, mc := newTestMovement()
	defer m.Close()

	for beat := 1; beat <= 4; beat++ {
		m.Dance(beat)
	}

	want := []string{"dance 60", "dance 120", "dance 60", "dance 120"}
	if got := mc.sent(); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("sent %v, want %v", got, want)
	}
}

func TestMovementDance_SkipsModelMovement(t *testing.T) {
	m, mc := newTestMovement()
	defer m.Close()

	m.Dance(1)
	resp := move(m, map[string]string{"command": "headshake"})
	m.RestSoon()
	m.Rest()

	if !strings.Contains(resp, "skipped") {
		t.Errorf("expected movement to be skipped while dancing, got %s", resp)
	}
	if got := mc.sent(); len(got) != 1 {
		t.Errorf("sent %v, want only the dance", got)
	}
	m.StopDance()
	if resp := move(m, map[string]string{"command": "headshake"}); strings.Contains(resp, "skipped") {
		t.Errorf("expected the interjection to still be unused after the dance, got %s", resp)
	}
}

func TestMovementDance_KeepaliveSilent(t *testing.T) {
	m, mc := newTestMovement()
	m.keepEvery = 10 * time.Millisecond
	go m.Keepalive()
	defer m.Close()

	move(m, map[string]string{"command": "talk"})
	m.Dance(1)
	n := len(mc.sent())
	time.Sleep(40 * time.Millisecond)

	if got := mc.sent(); len(got) != n {
		t.Errorf("expected no keepalive while dancing, got %v", got)
	}
}

func TestMovementStopDance_Rests(t *testing.T) {
	m, mc := newTestMovement()
	defer m.Close()

	move(m, map[string]string{"command": "talk"})
	m.Dance(1)
	m.StopDance()
	m.StopDance()

	got := mc.sent()
	if got[len(got)-1] != "stop" || got[len(got)-2] != "dance 60" {
		t.Errorf("expected one stop after the dance, got %v", got)
	}
	if !m.Wake() {
		t.Error("expected the head to be resting after the dance")
	}
}

func TestMovementDance_EndsWhenBeatsStop(t *testing.T) {
	m, mc := newTestMovement()
	m.danceFor = 10 * time.Millisecond
	defer m.Close()

	m.Dance(1)
	time.Sleep(40 * time.Millisecond)

	if got := mc.sent(); got[len(got)-1] != "stop" {
		t.Errorf("expected the dance to time out, got %v", got)
	}
}
