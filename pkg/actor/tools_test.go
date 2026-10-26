package actor

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/hybridgroup/yzma/pkg/message"
)

// ---- toolSuccessResponse ----

func TestToolSuccessResponse_Structure(t *testing.T) {
	resp := toolSuccessResponse("key", "value")

	var payload struct {
		Status string         `json:"status"`
		Data   map[string]any `json:"data"`
	}
	if err := json.Unmarshal([]byte(resp), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if payload.Status != "SUCCESS" {
		t.Errorf("status: got %q, want %q", payload.Status, "SUCCESS")
	}
	if payload.Data["key"] != "value" {
		t.Errorf("data[key]: got %v, want %q", payload.Data["key"], "value")
	}
}

func TestToolSuccessResponse_MultipleKeyValues(t *testing.T) {
	resp := toolSuccessResponse("a", "1", "b", "2")

	var payload struct {
		Status string         `json:"status"`
		Data   map[string]any `json:"data"`
	}
	if err := json.Unmarshal([]byte(resp), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if payload.Data["a"] != "1" || payload.Data["b"] != "2" {
		t.Errorf("unexpected data: %v", payload.Data)
	}
}

// ---- toolErrorResponse ----

func TestToolErrorResponse_Structure(t *testing.T) {
	resp := toolErrorResponse(context.DeadlineExceeded)

	var payload struct {
		Status string         `json:"status"`
		Data   map[string]any `json:"data"`
	}
	if err := json.Unmarshal([]byte(resp), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if payload.Status != "FAILED" {
		t.Errorf("status: got %q, want %q", payload.Status, "FAILED")
	}
	if !strings.Contains(payload.Data["error"].(string), "deadline exceeded") {
		t.Errorf("error message not found in data: %v", payload.Data)
	}
}

// ---- injectToolsIntoSystemPrompt ----

func TestInjectToolsIntoSystemPrompt_LFM(t *testing.T) {
	got := injectToolsIntoSystemPrompt("You are Gemmai.", `[{"name":"tool_movement"}]`, message.FormatLFM)

	for _, want := range []string{
		"You are Gemmai.\nList of tools: [{\"name\":\"tool_movement\"}]",
		"[tool_movement(command='talk', target='audience')]",
		"[tool_movement(command='listen', target='phineas')]",
		"at most once in a show",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}
