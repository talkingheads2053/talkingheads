package actor

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/hybridgroup/yzma/pkg/message"
)

// Tool describes the features which all tools must implement.
type Tool interface {
	Call(ctx context.Context, toolCall message.ToolCall) string
}

// toolSuccessResponse returns a successful structured tool response as a JSON string.
func toolSuccessResponse(keyValues ...any) string {
	data := make(map[string]any, len(keyValues)/2)
	for i := 0; i < len(keyValues); i += 2 {
		data[keyValues[i].(string)] = keyValues[i+1]
	}

	return toolContentJSON(data, "SUCCESS")
}

// toolErrorResponse returns a failed structured tool response as a JSON string.
func toolErrorResponse(err error) string {
	return toolContentJSON(map[string]any{"error": err.Error()}, "FAILED")
}

// toolContentJSON encodes data and status into a JSON string.
func toolContentJSON(data map[string]any, status string) string {
	info := struct {
		Status string         `json:"status"`
		Data   map[string]any `json:"data"`
	}{
		Status: status,
		Data:   data,
	}

	content, err := json.Marshal(info)
	if err != nil {
		return `{"status":"FAILED","data":{"error":"error marshaling tool response"}}`
	}

	return string(content)
}

// marshalToolDocs formats a slice of tool documents as a JSON array string for
// injection into the system prompt.
func marshalToolDocs(docs []message.ToolDefinition) string {
	b, err := json.MarshalIndent(docs, "", "  ")
	if err != nil {
		return "[]"
	}
	return string(b)
}

// injectToolsIntoSystemPrompt appends tool definitions and usage instructions
// to the system prompt using the format appropriate for the model.
func injectToolsIntoSystemPrompt(systemPrompt, toolsJSON string, format message.Format) string {
	if toolsJSON == "" || toolsJSON == "[]" {
		return systemPrompt
	}
	if format == message.FormatGemma3 || format == message.FormatGemma {
		return systemPrompt + fmt.Sprintf(`

You have access to the following tools:
%s

Tool calls use this syntax directly in your response alongside your spoken text:
  call:tool_movement{command:<|>look<|>,angle:90}

IMPORTANT rules:
- Only use the exact tool names and command values listed above. Do not invent commands.
- The angle parameter for 'look' and 'slowlook' MUST be inside the braces, e.g. angle:90. Never write angle: outside the braces.
- Do NOT write parenthetical stage directions like (character turns left) in your text. Use tool calls instead.

Example of a correct response — spoken text and tool calls in the same turn:
  Hello! I'm doing wonderfully today.
  call:tool_movement{command:<|>look<|>,angle:45}
  I looked to the right just now.

A response with only tool calls and no spoken text is invalid.`, toolsJSON)
	}
	if format == message.FormatQwen {
		return systemPrompt + fmt.Sprintf(`

You have access to the following tools:
%s

When you need to use a tool, use this exact format:
<function=function_name>
<parameter=arg1>value1</parameter>
</function>

IMPORTANT: Your spoken words MUST appear as plain text OUTSIDE any <function=…></function> blocks.
Tool calls are physical action cues only — they do NOT replace spoken text.
You MUST still write the actual words you want to say as plain text in your response.

Example of a correct response — plain text first, then motion tool calls:
Hello! I'm doing wonderfully today.
<function=tool_movement>
<parameter=command>wait</parameter>
</function>

A response with only <function=…></function> blocks and no plain text is ALWAYS wrong.`, toolsJSON)
	}
	if format == message.FormatLFM {
		return systemPrompt + fmt.Sprintf(`
List of tools: %s

When you need to use a tool, write the call as a Python list between the tool call tokens:
<|tool_call_start|>[tool_movement(command="look", angle=90)]<|tool_call_end|>

IMPORTANT: Your spoken words MUST appear as plain text OUTSIDE the tool call tokens.
Tool calls are physical action cues only. They do NOT replace spoken text.

Example of a correct response:
<|tool_call_start|>[tool_movement(command="look", angle=45)]<|tool_call_end|>Hello! I'm doing wonderfully today.

A response with only tool calls and no plain text is ALWAYS wrong.`, toolsJSON)
	}
	return systemPrompt + fmt.Sprintf(`

You have access to the following tools:
%s

When you need to use a tool, respond with a tool call in the following format:
<tool_call>
{"name": "function_name", "arguments": {"arg1": "value1"}}
</tool_call>
After receiving tool results, continue your response normally.`, toolsJSON)
}

// normalizeToolName returns a lowercase version of name with underscores and
// hyphens removed, used as a fallback key when a model elides punctuation from
// tool names (e.g. Gemma 4 outputs "toolmovement" for "tool_movement").
func normalizeToolName(name string) string {
	name = strings.ToLower(name)
	name = strings.ReplaceAll(name, "_", "")
	name = strings.ReplaceAll(name, "-", "")
	return name
}
