package actor

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/hybridgroup/yzma/pkg/message"
)

// mockTool is a simple Tool implementation for testing.
type mockTool struct {
	called bool
}

func (m *mockTool) Call(_ context.Context, toolCall message.ToolCall) string {
	m.called = true
	return toolSuccessResponse("result", "ok")
}

// TestPreprocessFunc_ReturnsNonNilCallback verifies that PreprocessFunc always
// returns a non-nil callable regardless of model state.
func TestPreprocessFunc_ReturnsNonNilCallback(t *testing.T) {
	a := &Actor{}
	fn := a.PreprocessFunc(context.Background())
	if fn == nil {
		t.Fatal("PreprocessFunc returned nil")
	}
}

// TestPreprocessFunc_CancelledContextIsSilent verifies that when the context
// is already cancelled the callback does not panic — the error from a nil
// llama context is swallowed because ctx.Err() != nil.
func TestPreprocessFunc_CancelledContextIsSilent(t *testing.T) {
	a := &Actor{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	fn := a.PreprocessFunc(ctx)
	conv := []message.Message{
		message.Chat{Role: "user", Content: "hello"},
	}
	fn(&conv) // must not panic
}

func TestGetMore_NilFunc_ReturnsFalse(t *testing.T) {
	a := &Actor{moreConversationFunc: nil}

	conv := []message.Message{}
	got := a.GetMore(&conv)
	if got {
		t.Error("expected GetMore to return false when moreConversationFunc is nil")
	}
}

func TestGetMore_WithFunc_ReturnsTrue(t *testing.T) {
	called := false
	a := &Actor{
		moreConversationFunc: func(conversation *[]message.Message) {
			called = true
		},
	}

	conv := []message.Message{}
	got := a.GetMore(&conv)
	if !got {
		t.Error("expected GetMore to return true when moreConversationFunc is set")
	}
	if !called {
		t.Error("expected moreConversationFunc to be called")
	}
}

func TestCallTools_UnknownTool_Skipped(t *testing.T) {
	a := &Actor{tools: make(map[string]Tool)}

	toolCalls := []message.ToolCall{
		{
			Type: "function",
			Function: message.ToolFunction{
				Name:      "nonexistent_tool",
				Arguments: map[string]string{},
			},
		},
	}

	resps := a.callTools(context.Background(), toolCalls)
	if len(resps) != 0 {
		t.Errorf("expected 0 responses for unknown tool, got %d", len(resps))
	}
}

func TestCallTools_KnownTool_Called(t *testing.T) {
	mock := &mockTool{}
	a := &Actor{
		tools: map[string]Tool{
			"mock_tool": mock,
		},
	}

	toolCalls := []message.ToolCall{
		{
			Type: "function",
			Function: message.ToolFunction{
				Name:      "mock_tool",
				Arguments: map[string]string{},
			},
		},
	}

	resps := a.callTools(context.Background(), toolCalls)
	if len(resps) != 1 {
		t.Fatalf("expected 1 response, got %d", len(resps))
	}
	if !mock.called {
		t.Error("expected mock tool to be called")
	}
	tr, ok := resps[0].(message.ToolResponse)
	if !ok {
		t.Fatalf("expected ToolResponse, got %T", resps[0])
	}
	if tr.Role != "tool" {
		t.Errorf("role: got %q, want %q", tr.Role, "tool")
	}
}

func TestCallTools_MixedTools(t *testing.T) {
	mock := &mockTool{}
	a := &Actor{
		tools: map[string]Tool{
			"known_tool": mock,
		},
	}

	toolCalls := []message.ToolCall{
		{Type: "function", Function: message.ToolFunction{Name: "known_tool", Arguments: map[string]string{}}},
		{Type: "function", Function: message.ToolFunction{Name: "unknown_tool", Arguments: map[string]string{}}},
	}

	resps := a.callTools(context.Background(), toolCalls)
	if len(resps) != 1 {
		t.Errorf("expected 1 response (unknown skipped), got %d", len(resps))
	}
}

// TestCallTools_NormalizedName verifies that a tool registered as "tool_movement"
// is found when the model outputs the name without underscores ("toolmovement"),
// which is the observed behaviour of Gemma 4 models.
func TestCallTools_NormalizedName_Found(t *testing.T) {
	mock := &mockTool{}
	a := &Actor{
		tools: map[string]Tool{
			"tool_movement": mock,
		},
	}

	toolCalls := []message.ToolCall{
		{
			Type: "function",
			Function: message.ToolFunction{
				Name:      "toolmovement",
				Arguments: map[string]string{"command": "speak"},
			},
		},
	}

	resps := a.callTools(context.Background(), toolCalls)
	if len(resps) != 1 {
		t.Fatalf("expected 1 response via normalized lookup, got %d", len(resps))
	}
	if !mock.called {
		t.Error("expected mock tool to be called")
	}
}

// TestNormalizeToolName covers the normalizeToolName helper.
func TestNormalizeToolName(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"tool_movement", "toolmovement"},
		{"toolmovement", "toolmovement"},
		{"Tool_Movement", "toolmovement"},
		{"tool-movement", "toolmovement"},
		{"noop", "noop"},
	}
	for _, c := range cases {
		got := normalizeToolName(c.input)
		if got != c.want {
			t.Errorf("normalizeToolName(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}

// TestStripActorMarkup_MissingSpaceAfterPeriod verifies that a single period
// glued to a following word is split with a space, while decimals, ellipses
// and single-letter abbreviations are left alone. Both "lowercase.UPPERCASE"
// and "lowercase.lowercase" boundaries are fixed.
func TestStripActorMarkup_MissingSpaceAfterPeriod(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"I like things.I really do", "I like things. I really do"},
		{"Hello world.Then goodbye", "Hello world. Then goodbye"},
		{"one.Two.Three", "one. Two. Three"},
		// lowercase.lowercase boundaries are also fixed.
		{"done.then we go", "done. then we go"},
		{"hello.world", "hello. world"},
		{"first.second.third", "first. second. third"},
		// Untouched: decimals, ellipses, single-letter abbreviations.
		{"pi is 3.14", "pi is 3.14"},
		{"wait...okay", "wait...okay"},
		{"e.g. this", "e.g. this"},
		{"i.e. that", "i.e. that"},
		{"U.S.A. today", "U.S.A. today"},
		{"already. spaced", "already. spaced"},
		{"end of sentence.", "end of sentence."},
		// JSON-wrapped responses are unwrapped and still get the fix.
		{`{"response": "hello.World"}`, "hello. World"},
		{`{"response": "hello.world"}`, "hello. world"},
	}
	for _, c := range cases {
		got := stripActorMarkup(c.input)
		if got != c.want {
			t.Errorf("stripActorMarkup(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}

// TestStripActorMarkup_HTMLTags verifies that HTML/XML tags are removed,
// including orphaned opening and closing tags with no matching counterpart.
func TestStripActorMarkup_HTMLTags(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		// Matched pairs.
		{"<strong>hello</strong>", "hello"},
		{"<em>world</em>", "world"},
		// Orphaned opening tag.
		{"<strong>hello", "hello"},
		// Orphaned closing tag.
		{"hello</bold>", "hello"},
		// Tags with attributes.
		{`<span class="foo">text</span>`, "text"},
		// Multiple tags.
		{"<b>one</b> and <i>two</i>", "one and two"},
		// Nested orphans.
		{"</strong>some text<br>here", "some texthere"},
		// No tags — unchanged.
		{"plain text", "plain text"},
	}
	for _, c := range cases {
		got := stripActorMarkup(c.input)
		if got != c.want {
			t.Errorf("stripActorMarkup(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}

// TestStripActorMarkup_NonEnglish verifies that characters outside the
// printable ASCII range are removed.
func TestStripActorMarkup_NonEnglish(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		// Accented / Latin-extended characters.
		{"caf\u00e9", "caf"},
		{"na\u00efve", "nave"},
		// Non-Latin scripts.
		{"hello \u4e16\u754c", "hello"},
		{"\u0645\u0631\u062d\u0628\u0627 world", "world"},
		// Emoji.
		{"great job \U0001f44d", "great job"},
		// Mixed: only ASCII kept.
		{"It\u2019s fine", "Its fine"},
		// Pure ASCII passes through unchanged.
		{"Hello, world!", "Hello, world!"},
	}
	for _, c := range cases {
		got := stripActorMarkup(c.input)
		if got != c.want {
			t.Errorf("stripActorMarkup(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}

// TestTruncateToSentences verifies that the helper caps the number of
// sentences in its input and drops any partial trailing sentence.
func TestTruncateToSentences(t *testing.T) {
	cases := []struct {
		name  string
		input string
		max   int
		want  string
	}{
		{"unlimited zero", "One. Two. Three.", 0, "One. Two. Three."},
		{"unlimited negative", "One. Two. Three.", -1, "One. Two. Three."},
		{"cap to one", "One. Two. Three.", 1, "One."},
		{"cap to two", "One. Two. Three.", 2, "One. Two."},
		{"cap above count", "One. Two.", 5, "One. Two."},
		{"mixed terminators", "Hi! How are you? I am fine.", 2, "Hi! How are you?"},
		{"drop partial trailing", "Done. And then", 1, "Done."},
		{"no terminator", "incomplete sentence", 3, "incomplete sentence"},
	}
	for _, c := range cases {
		got := truncateToSentences(c.input, c.max)
		if got != c.want {
			t.Errorf("%s: truncateToSentences(%q, %d) = %q, want %q", c.name, c.input, c.max, got, c.want)
		}
	}
}

func TestCoalesceSameRole_MergesConsecutiveUsers(t *testing.T) {
	in := []message.Message{
		message.Chat{Role: "system", Content: "sys"},
		message.Chat{Role: "user", Content: "qwentin says: hi"},
		message.Chat{Role: "user", Content: "phineas, please respond"},
		message.Chat{Role: "assistant", Content: "ok"},
		message.Chat{Role: "user", Content: "another heard"},
		message.Chat{Role: "user", Content: "next direction"},
	}
	out := coalesceSameRole(in)
	if len(out) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(out))
	}
	if out[1].GetRole() != "user" ||
		out[1].GetContent()["content"].(string) != "qwentin says: hi\nphineas, please respond" {
		t.Errorf("first user pair not merged: %+v", out[1])
	}
	if out[3].GetRole() != "user" ||
		out[3].GetContent()["content"].(string) != "another heard\nnext direction" {
		t.Errorf("trailing user pair not merged: %+v", out[3])
	}
}

func TestCoalesceSameRole_PreservesToolMessages(t *testing.T) {
	tc := []message.ToolCall{{Type: "function", Function: message.ToolFunction{Name: "tool_movement"}}}
	in := []message.Message{
		message.Chat{Role: "user", Content: "u1"},
		message.Tool{Role: "assistant", Content: "", ToolCalls: tc},
		message.Chat{Role: "assistant", Content: "spoken"},
	}
	out := coalesceSameRole(in)
	// The assistant Tool message and the assistant Chat message must NOT be
	// merged — the Tool message carries structured ToolCalls.
	if len(out) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(out))
	}
	if _, ok := out[1].(message.Tool); !ok {
		t.Errorf("expected message.Tool at index 1, got %T", out[1])
	}
}

func TestCoalesceSameRole_NoOpWhenAlternating(t *testing.T) {
	in := []message.Message{
		message.Chat{Role: "system", Content: "sys"},
		message.Chat{Role: "user", Content: "u"},
		message.Chat{Role: "assistant", Content: "a"},
	}
	out := coalesceSameRole(in)
	if len(out) != 3 {
		t.Fatalf("expected unchanged length 3, got %d", len(out))
	}
}

func TestCoalesceSameRole_HandlesEmptyContent(t *testing.T) {
	in := []message.Message{
		message.Chat{Role: "user", Content: ""},
		message.Chat{Role: "user", Content: "hello"},
	}
	out := coalesceSameRole(in)
	if len(out) != 1 {
		t.Fatalf("expected 1 message, got %d", len(out))
	}
	if got := out[0].GetContent()["content"].(string); got != "hello" {
		t.Errorf("empty + non-empty merge: got %q, want %q", got, "hello")
	}
}

// --- SetThinkingOutputFunc / runThinkingPhrases ---

func TestSetThinkingOutputFunc_IsStored(t *testing.T) {
	a := &Actor{}
	fn := func(s string) {}
	a.SetThinkingOutputFunc(fn)
	if a.thinkingOutputFunc == nil {
		t.Error("expected thinkingOutputFunc to be set after SetThinkingOutputFunc")
	}
}

func TestSetThinkingOutputFunc_NilClearsField(t *testing.T) {
	a := &Actor{thinkingOutputFunc: func(s string) {}}
	a.SetThinkingOutputFunc(nil)
	if a.thinkingOutputFunc != nil {
		t.Error("expected thinkingOutputFunc to be nil after SetThinkingOutputFunc(nil)")
	}
}

func TestRunThinkingPhrases_UsesThinkingOutputFunc_WhenSet(t *testing.T) {
	var mu sync.Mutex
	var thinkingCalls, outputCalls int

	a := &Actor{
		cfg: Config{
			ThinkingPhrases:  []string{"hmm..."},
			ThinkingInterval: 60, // long interval so only the immediate first phrase fires
		},
		outputFunc:         func(s string) { mu.Lock(); outputCalls++; mu.Unlock() },
		thinkingOutputFunc: func(s string) { mu.Lock(); thinkingCalls++; mu.Unlock() },
	}

	done := make(chan struct{})
	next := a.setupThinkingPhrases()
	go a.runThinkingPhrases(done, next)

	// Give the goroutine time to emit the first (immediate) thinking phrase.
	time.Sleep(50 * time.Millisecond)
	close(done)

	mu.Lock()
	p, o := thinkingCalls, outputCalls
	mu.Unlock()

	if p == 0 {
		t.Error("expected thinkingOutputFunc to be called at least once")
	}
	if o != 0 {
		t.Errorf("expected outputFunc to not be called, got %d calls", o)
	}
}

func TestRunThinkingPhrases_FallsBackToOutputFunc_WhenThinkingOutputFuncNil(t *testing.T) {
	var mu sync.Mutex
	var outputCalls int

	a := &Actor{
		cfg: Config{
			ThinkingPhrases:  []string{"hmm..."},
			ThinkingInterval: 60,
		},
		outputFunc:         func(s string) { mu.Lock(); outputCalls++; mu.Unlock() },
		thinkingOutputFunc: nil,
	}

	done := make(chan struct{})
	next := a.setupThinkingPhrases()
	go a.runThinkingPhrases(done, next)

	time.Sleep(50 * time.Millisecond)
	close(done)

	mu.Lock()
	o := outputCalls
	mu.Unlock()

	if o == 0 {
		t.Error("expected outputFunc to be called when thinkingOutputFunc is nil")
	}
}

func TestSetSpeakingDoneFunc_IsStored(t *testing.T) {
	a := &Actor{}
	fn := func() {}
	a.SetSpeakingDoneFunc(fn)
	if a.speakingDoneFunc == nil {
		t.Error("expected speakingDoneFunc to be set after SetSpeakingDoneFunc")
	}
}

func TestSetSpeakingDoneFunc_NilClearsField(t *testing.T) {
	a := &Actor{speakingDoneFunc: func() {}}
	a.SetSpeakingDoneFunc(nil)
	if a.speakingDoneFunc != nil {
		t.Error("expected speakingDoneFunc to be nil after SetSpeakingDoneFunc(nil)")
	}
}

func TestRunThinkingPhrases_CallsSpeakingDoneFunc_AfterEachPhrase(t *testing.T) {
	var mu sync.Mutex
	var emitCalls, doneCalls int

	a := &Actor{
		cfg: Config{
			ThinkingPhrases:  []string{"hmm..."},
			ThinkingInterval: 60, // long interval — only the immediate first phrase fires
		},
		thinkingOutputFunc: func(s string) { mu.Lock(); emitCalls++; mu.Unlock() },
		speakingDoneFunc:   func() { mu.Lock(); doneCalls++; mu.Unlock() },
	}

	done := make(chan struct{})
	next := a.setupThinkingPhrases()
	go a.runThinkingPhrases(done, next)

	time.Sleep(50 * time.Millisecond)
	close(done)

	mu.Lock()
	e, d := emitCalls, doneCalls
	mu.Unlock()

	if e == 0 {
		t.Error("expected thinkingOutputFunc to be called at least once")
	}
	if d != e {
		t.Errorf("expected speakingDoneFunc to be called once per emit; emits=%d, doneFunc calls=%d", e, d)
	}
}
