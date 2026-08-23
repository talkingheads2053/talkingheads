package actor

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/hybridgroup/yzma/pkg/llama"
	"github.com/hybridgroup/yzma/pkg/message"
	"github.com/hybridgroup/yzma/pkg/template"
)

// Actor drives a conversation using a local llama.cpp model loaded via yzma.
type Actor struct {
	cfg          Config
	llamaModel   llama.Model
	llamaCtx     llama.Context
	vocab        llama.Vocab
	sampler      llama.Sampler
	chatTemplate string

	// nCachedPrompt is the number of prompt tokens currently held in the KV /
	// recurrent cache from the previous turn. It is used to implement
	// incremental decoding: only tokens beyond this offset need to be decoded
	// each turn, saving the cost of re-decoding the entire conversation history.
	nCachedPrompt int

	// nSystemPromptTokens is the number of tokens occupied by the system prompt
	// alone (rendered via the chat template with no user turns). These tokens are
	// decoded once during warm-up and never need to be re-decoded as long as the
	// system prompt doesn't change. When context trimming forces a full KV cache
	// clear, only the tokens *beyond* this prefix need decoding on the next turn.
	nSystemPromptTokens int

	moreConversationFunc func(conversation *[]message.Message)
	outputFunc           func(content string)
	thinkingOutputFunc   func(content string)
	speakingDoneFunc     func()
	tools                map[string]Tool
	toolsJSON            string
}

// NewActor creates a new instance of Actor.
// modelPath is the local path to a GGUF model file.
// The llama.cpp shared libraries are loaded from the YZMA_LIB environment variable.
func NewActor(modelPath string, cfg Config, commander Commander, moreFunc func(conversation *[]message.Message), outputFunc func(content string)) (*Actor, error) {
	libPath := os.Getenv("YZMA_LIB")
	if libPath == "" {
		return nil, fmt.Errorf("YZMA_LIB environment variable not set")
	}

	if err := llama.Load(libPath); err != nil {
		return nil, fmt.Errorf("unable to load llama.cpp: %w", err)
	}

	if !cfg.Verbose {
		llama.LogSet(llama.LogSilent())
	}
	llama.Init()

	log.Println("loading model...")

	modelParams := llama.ModelDefaultParams()
	if !cfg.UseMmap {
		// llama.cpp replaced the separate use_mmap/use_mlock booleans with a
		// single load-mode enum, so clearing mmap means dropping to the
		// equivalent mode that keeps any mlock setting intact.
		switch modelParams.LoadMode {
		case llama.LoadModeMmap:
			modelParams.LoadMode = llama.LoadModeNone
		case llama.LoadModeMmapMlock:
			modelParams.LoadMode = llama.LoadModeMlock
		}
	}
	mdl, err := llama.ModelLoadFromFile(modelPath, modelParams)
	if err != nil {
		return nil, fmt.Errorf("unable to load model: %w", err)
	}

	ctxParams := llama.ContextDefaultParams()
	ctxParams.NCtx = cfg.ContextSize
	if cfg.BatchSize > 0 {
		ctxParams.NBatch = cfg.BatchSize
	}
	if cfg.UBatchSize > 0 {
		ctxParams.NUbatch = cfg.UBatchSize
	}
	lctx, err := llama.InitFromModel(mdl, ctxParams)
	if err != nil {
		llama.ModelFree(mdl)
		return nil, fmt.Errorf("unable to create context: %w", err)
	}

	vocab := llama.ModelGetVocab(mdl)

	// Resolve the model format first — we need it to pick the right fallback
	// chat template when the model file carries no tokenizer.chat_template.
	if cfg.ModelFormat == message.FormatAuto {
		cfg.ModelFormat = message.DetectFormatFromPath(modelPath)
	}

	chatTmpl := llama.ModelChatTemplate(mdl, "")
	if chatTmpl == "" {
		// No template baked into the model metadata. Fall back to a built-in
		// template that matches the model family when one is available.
		var builtinName string
		switch cfg.ModelFormat {
		case message.FormatGemma3, message.FormatGemma:
			builtinName = "gemma3"
		default:
			builtinName = "chatml"
		}
		if tmplContent, ok := template.BuiltinTemplate(builtinName); ok {
			chatTmpl = tmplContent
		} else {
			chatTmpl = builtinName
		}
	}

	sp := llama.DefaultSamplerParams()
	sp.Temp = cfg.Temperature
	sp.TopP = cfg.TopP
	sp.MinP = cfg.MinP
	sp.TopK = cfg.TopK
	sp.PenaltyRepeat = cfg.RepeatPenalty
	sp.PenaltyFreq = cfg.FreqPenalty
	sp.PenaltyPresent = cfg.PresencePenalty
	sp.DryMultiplier = cfg.DryMultiplier
	smpl := llama.NewSampler(mdl, llama.DefaultSamplers, sp)

	toolsMap := make(map[string]Tool)
	toolDocs := []message.ToolDefinition{
		RegisterMovement(toolsMap, commander),
	}

	return &Actor{
		cfg:                  cfg,
		llamaModel:           mdl,
		llamaCtx:             lctx,
		vocab:                vocab,
		sampler:              smpl,
		chatTemplate:         chatTmpl,
		moreConversationFunc: moreFunc,
		outputFunc:           outputFunc,
		tools:                toolsMap,
		toolsJSON:            marshalToolDocs(toolDocs),
	}, nil
}

// prepareConversationForTemplate preprocesses a conversation before rendering
// when the model's chat template does not support a dedicated system role
// (e.g. Gemma3 templates only support "user" and "model" roles). In that case
// the leading system message content is prepended to the first user message so
// the model still receives the system instructions. All other formats are
// returned unchanged.
func prepareConversationForTemplate(conv []message.Message, format message.Format) []message.Message {
	if format != message.FormatGemma3 {
		return coalesceSameRole(conv)
	}
	if len(conv) == 0 || conv[0].GetRole() != "system" {
		return coalesceSameRole(conv)
	}
	sysContent := conv[0].GetContent()["content"].(string)
	out := make([]message.Message, 0, len(conv))
	merged := false
	for _, msg := range conv[1:] {
		if !merged && msg.GetRole() == "user" {
			userContent := msg.GetContent()["content"].(string)
			out = append(out, message.Chat{Role: "user", Content: sysContent + "\n\n" + userContent})
			merged = true
		} else {
			out = append(out, msg)
		}
	}
	if !merged {
		// No user message to merge into; strip the system message.
		return coalesceSameRole(conv[1:])
	}
	return coalesceSameRole(out)
}

// coalesceSameRole merges runs of consecutive plain chat messages that share
// the same role into a single message, joining their content with a newline.
// This is required because some chat templates (Mistral and certain Qwen
// instruct variants) refuse to render conversations whose user/assistant turns
// don't strictly alternate, raising errors like:
//
//	Conversation roles must alternate user/assistant/user/assistant/...
//
// Only message.Chat entries are merged; message.Tool (assistant turns with
// tool calls) and tool-result messages are preserved verbatim because they
// carry structured fields that cannot be concatenated as strings.
func coalesceSameRole(conv []message.Message) []message.Message {
	if len(conv) < 2 {
		return conv
	}
	out := make([]message.Message, 0, len(conv))
	for _, msg := range conv {
		cur, curIsChat := msg.(message.Chat)
		if curIsChat && len(out) > 0 {
			if prev, prevIsChat := out[len(out)-1].(message.Chat); prevIsChat && prev.Role == cur.Role {
				switch {
				case prev.Content == "":
					prev.Content = cur.Content
				case cur.Content != "":
					prev.Content = prev.Content + "\n" + cur.Content
				}
				out[len(out)-1] = prev
				continue
			}
		}
		out = append(out, msg)
	}
	return out
}

// warmUpSystemPrompt tokenizes the system prompt by itself, decodes it into
// the KV cache, and records the token count in nSystemPromptTokens /
// nCachedPrompt. Subsequent turns then start decoding from that offset,
// skipping the most expensive part of the prompt on every turn.
// sysContent must be the final system message content (tools already injected).
func (a *Actor) warmUpSystemPrompt(ctx context.Context, sysContent string) error {
	// Many chat templates (e.g. Qwen, ChatML) require at least one user message
	// to render successfully. Use an empty-content user placeholder so the
	// template is satisfied, then measure how many tokens the system portion
	// occupies by comparing the full render against a render with a non-empty
	// user message. The system prefix length is the number of leading tokens
	// shared by both renders.
	tmplOpts := template.Options{EnableThinking: a.cfg.EnableThinking}

	renderWith := func(userContent string) (string, error) {
		conv := []message.Message{
			message.Chat{Role: "system", Content: sysContent},
			message.Chat{Role: "user", Content: userContent},
		}
		renderConv := prepareConversationForTemplate(conv, a.cfg.ModelFormat)
		return template.ApplyWithOptions(a.chatTemplate, renderConv, false, tmplOpts)
	}

	promptEmpty, err := renderWith("")
	if err != nil {
		return fmt.Errorf("error applying chat template for warm-up: %w", err)
	}
	promptSentinel, err := renderWith("WARMUP_SENTINEL_TOKEN")
	if err != nil {
		return fmt.Errorf("error applying chat template for warm-up (sentinel): %w", err)
	}

	tokensEmpty := llama.Tokenize(a.vocab, promptEmpty, true, true)
	tokensSentinel := llama.Tokenize(a.vocab, promptSentinel, true, true)

	// Find the longest common prefix between the two tokenisations. Everything
	// up to that boundary is purely system-prompt tokens.
	prefixLen := 0
	for prefixLen < len(tokensEmpty) && prefixLen < len(tokensSentinel) &&
		tokensEmpty[prefixLen] == tokensSentinel[prefixLen] {
		prefixLen++
	}
	if prefixLen == 0 {
		// Shouldn't happen, but if the two renders share no prefix something is
		// wrong with the template. Decode the full empty-user render instead.
		prefixLen = len(tokensEmpty)
	}

	tokens := tokensEmpty[:prefixLen]

	if len(tokens) == 0 {
		return nil
	}

	mem, err := llama.GetMemory(a.llamaCtx)
	if err != nil {
		return fmt.Errorf("error getting memory for warm-up: %w", err)
	}
	if clearErr := llama.MemoryClear(mem, true); clearErr != nil {
		return fmt.Errorf("error clearing memory for warm-up: %w", clearErr)
	}

	nBatch := int(llama.NBatch(a.llamaCtx))
	if nBatch <= 0 {
		nBatch = 512
	}

	t0 := time.Now()
	if a.cfg.Verbose {
		log.Printf("[verbose] warm-up: decoding %d system prompt tokens, batch size %d", len(tokens), nBatch)
	}
	for i := 0; i < len(tokens); i += nBatch {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		end := i + nBatch
		if end > len(tokens) {
			end = len(tokens)
		}
		if _, err := llama.Decode(a.llamaCtx, llama.BatchGetOne(tokens[i:end])); err != nil {
			return fmt.Errorf("error decoding system prompt during warm-up: %w", err)
		}
	}
	if a.cfg.Verbose {
		log.Printf("[verbose] warm-up done: %v", time.Since(t0))
	}

	a.nSystemPromptTokens = len(tokens)
	a.nCachedPrompt = len(tokens)
	return nil
}

// preDecodeConversation renders the conversation, tokenizes it, and decodes
// the new tokens into the KV cache without sampling any output. It uses the
// same three-case incremental-decode logic as generateTurn so that subsequent
// calls to generateTurn only need to decode the delta (typically a single
// Direction message) rather than the full accumulated context.
//
// The prompt is rendered with addAssistant=false because no generation is
// requested; generateTurn will append the assistant-turn opener when it runs.
// Since both renders share the same token prefix, Case 1 of the cache logic
// applies and generateTurn only decodes the Direction tokens on top.
func (a *Actor) preDecodeConversation(ctx context.Context, conversation *[]message.Message) error {
	tmplOpts := template.Options{EnableThinking: a.cfg.EnableThinking}
	renderConv := prepareConversationForTemplate(*conversation, a.cfg.ModelFormat)
	prompt, err := template.ApplyWithOptions(a.chatTemplate, renderConv, false, tmplOpts)
	if err != nil {
		return fmt.Errorf("error applying chat template: %w", err)
	}

	tokens := llama.Tokenize(a.vocab, prompt, true, true)

	// Trim oldest non-system messages if the prompt exceeds the context window.
	if nCtx := int(llama.NCtx(a.llamaCtx)); nCtx > 0 {
		maxPromptTokens := nCtx - a.cfg.MaxTokens
		for len(tokens) > maxPromptTokens && len(*conversation) > 2 {
			if a.cfg.Verbose {
				log.Println("trimming oldest message from conversation to fit context window")
			}
			*conversation = append((*conversation)[:1], (*conversation)[2:]...)
			renderConv = prepareConversationForTemplate(*conversation, a.cfg.ModelFormat)
			prompt, err = template.ApplyWithOptions(a.chatTemplate, renderConv, false, tmplOpts)
			if err != nil {
				return fmt.Errorf("error applying chat template after trim: %w", err)
			}
			tokens = llama.Tokenize(a.vocab, prompt, true, false)
		}
	}

	mem, err := llama.GetMemory(a.llamaCtx)
	if err != nil {
		return fmt.Errorf("error getting memory: %w", err)
	}

	var decodeFrom int
	t0 := time.Now()
	if a.nCachedPrompt > 0 && len(tokens) >= a.nCachedPrompt {
		if ok, rmErr := llama.MemorySeqRm(mem, 0, llama.Pos(a.nCachedPrompt), -1); ok && rmErr == nil {
			decodeFrom = a.nCachedPrompt
			if a.cfg.Verbose {
				log.Printf("[verbose] preprocess cache trim: kept %d cached tokens, removed tail (%v)", a.nCachedPrompt, time.Since(t0))
			}
		} else {
			if clearErr := llama.MemoryClear(mem, true); clearErr != nil {
				return fmt.Errorf("error clearing memory: %w", clearErr)
			}
			a.nCachedPrompt = 0
			if a.cfg.Verbose {
				log.Printf("[verbose] preprocess cache trim failed, full clear (%v)", time.Since(t0))
			}
		}
	} else if a.nCachedPrompt > 0 && a.nSystemPromptTokens > 0 && len(tokens) >= a.nSystemPromptTokens {
		if ok, rmErr := llama.MemorySeqRm(mem, 0, llama.Pos(a.nSystemPromptTokens), -1); ok && rmErr == nil {
			decodeFrom = a.nSystemPromptTokens
			a.nCachedPrompt = a.nSystemPromptTokens
			if a.cfg.Verbose {
				log.Printf("[verbose] preprocess cache trim (shrink): restored to system prefix (%d tokens) (%v)", a.nSystemPromptTokens, time.Since(t0))
			}
		} else {
			if clearErr := llama.MemoryClear(mem, true); clearErr != nil {
				return fmt.Errorf("error clearing memory: %w", clearErr)
			}
			a.nCachedPrompt = 0
			if a.cfg.Verbose {
				log.Printf("[verbose] preprocess cache trim failed (shrink), full clear (%v)", time.Since(t0))
			}
		}
	} else {
		if clearErr := llama.MemoryClear(mem, true); clearErr != nil {
			return fmt.Errorf("error clearing memory: %w", clearErr)
		}
		a.nCachedPrompt = 0
		if a.cfg.Verbose {
			log.Printf("[verbose] preprocess memory clear: %v", time.Since(t0))
		}
	}

	nBatch := int(llama.NBatch(a.llamaCtx))
	if nBatch <= 0 {
		nBatch = 512
	}
	if a.cfg.Verbose {
		log.Printf("[verbose] preprocess: decoding %d new tokens (of %d total)", len(tokens)-decodeFrom, len(tokens))
	}
	for i := decodeFrom; i < len(tokens); i += nBatch {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		end := i + nBatch
		if end > len(tokens) {
			end = len(tokens)
		}
		if _, err := llama.Decode(a.llamaCtx, llama.BatchGetOne(tokens[i:end])); err != nil {
			return fmt.Errorf("error pre-decoding conversation: %w", err)
		}
	}

	a.nCachedPrompt = len(tokens)
	return nil
}

// PreprocessFunc returns a callback suitable for use as a preprocessing hook
// in MQTTListener. Each time another actor's Speak message is added to the
// conversation while waiting for a Direction, the callback pre-decodes the
// updated conversation into the KV cache. When the Direction finally arrives,
// generateTurn only needs to decode the Direction tokens — not the entire
// accumulated heard context — reducing response latency.
func (a *Actor) PreprocessFunc(ctx context.Context) func(*[]message.Message) {
	return func(conversation *[]message.Message) {
		if err := a.preDecodeConversation(ctx, conversation); err != nil {
			if ctx.Err() == nil {
				log.Printf("preprocess: %v", err)
			}
		}
	}
}

// Close releases model and context resources.
func (a *Actor) Close() {
	llama.SamplerFree(a.sampler)
	llama.Free(a.llamaCtx)
	llama.ModelFree(a.llamaModel)
	llama.Close()
}

// Run starts the actor and runs the chat loop.
func (a *Actor) Run(ctx context.Context, systemPrompt string) error {
	// Set the abort callback once for the duration of the run so that
	// llama.Decode is interrupted immediately when the context is cancelled.
	// Done here rather than per-turn to avoid a log line on every turn.
	llama.SetAbortCallback(a.llamaCtx, func() bool { return ctx.Err() != nil })
	defer llama.SetAbortCallback(a.llamaCtx, nil)

	sysContent := systemPrompt
	if a.cfg.InjectTools {
		sysContent = injectToolsIntoSystemPrompt(systemPrompt, a.toolsJSON, a.cfg.ModelFormat)
	}
	conversation := []message.Message{
		message.Chat{Role: "system", Content: sysContent},
	}

	// Pre-decode the system prompt into the KV cache so the first (and every
	// subsequent) turn only needs to decode the incremental user/assistant turns.
	if err := a.warmUpSystemPrompt(ctx, sysContent); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		log.Printf("warm-up failed (continuing without pre-cached prompt): %v", err)
	}

	needMoreInput := true
	consecutiveToolOnlyTurns := 0

	nextThinkingPhrase := a.setupThinkingPhrases()

	for {
		var thinkingDone chan struct{}
		if needMoreInput {
			consecutiveToolOnlyTurns = 0
			before := len(conversation)
			if ok := a.GetMore(&conversation); !ok {
				return nil
			}
			if len(conversation) == before {
				// moreFunc returned without adding anything — check if context
				// was cancelled, otherwise loop back and wait for the next message.
				select {
				case <-ctx.Done():
					return ctx.Err()
				default:
					continue
				}
			}

			if len(a.cfg.ThinkingPhrases) > 0 && a.outputFunc != nil {
				thinkingDone = make(chan struct{})
				go a.runThinkingPhrases(thinkingDone, nextThinkingPhrase)
			}
		}

		// Build a one-shot stop function that closes thinkingDone the first time
		// it is called. It is passed into generateTurn so that thinking phrases
		// are cut off as soon as the model produces its first token rather
		// than after the full generation completes.
		thinkingStopped := false
		stopPausing := func() {
			if !thinkingStopped && thinkingDone != nil {
				thinkingStopped = true
				close(thinkingDone)
			}
		}

		content, hadText, toolCalls, err := a.generateTurn(ctx, &conversation, stopPausing)
		stopPausing() // no-op if already triggered on the first token
		if err != nil {
			return err
		}

		if len(toolCalls) > 0 {
			consecutiveToolOnlyTurns, needMoreInput = a.handleToolCalls(ctx, &conversation, toolCalls, content, hadText, consecutiveToolOnlyTurns)
			continue
		}

		a.appendAssistant(&conversation, content)
		needMoreInput = true
	}
}

// handleToolCalls processes a turn that produced tool calls, updating the
// conversation and returning the new consecutiveToolOnlyTurns and needMoreInput values.
func (a *Actor) handleToolCalls(ctx context.Context, conversation *[]message.Message, toolCalls []message.ToolCall, content string, hadText bool, consecutiveToolOnlyTurns int) (int, bool) {
	const maxConsecutiveToolOnlyTurns = 2

	toolResults := a.callTools(ctx, toolCalls)

	if !a.templateSupportsToolMessages() {
		// Format has no tool-role support (e.g. Gemma 3). Tool calls are
		// fire-and-forget physical action cues; never append tool call or
		// tool result messages to the conversation history.
		if hadText {
			a.appendAssistant(conversation, content)
			return 0, true
		}
		consecutiveToolOnlyTurns++
		if consecutiveToolOnlyTurns >= maxConsecutiveToolOnlyTurns {
			if a.cfg.Verbose {
				log.Printf("breaking tool-call loop after %d consecutive tool-only turns", consecutiveToolOnlyTurns)
			}
			return 0, true
		}
		*conversation = append(*conversation, message.Chat{
			Role:    "user",
			Content: "You called motion tools but included no spoken words. You MUST write your actual answer as plain text. Reply now with spoken sentences.",
		})
		if a.cfg.Verbose {
			log.Printf("tool-only turn %d/%d, nudging for verbal response", consecutiveToolOnlyTurns, maxConsecutiveToolOnlyTurns)
		}
		return consecutiveToolOnlyTurns, false
	}

	if hadText {
		// Text was spoken alongside the tool calls — record the full
		// exchange (including spoken text) so subsequent turns have correct context.
		a.appendToolCalls(conversation, toolCalls, content)
		*conversation = append(*conversation, toolResults...)
		return 0, true
	}
	consecutiveToolOnlyTurns++
	a.appendToolCalls(conversation, toolCalls, "")
	*conversation = append(*conversation, toolResults...)
	if consecutiveToolOnlyTurns >= maxConsecutiveToolOnlyTurns {
		// Still no text after nudge — give up and wait for new input.
		if a.cfg.Verbose {
			log.Printf("breaking tool-call loop after %d consecutive tool-only turns", consecutiveToolOnlyTurns)
		}
		return 0, true
	}
	// Inject a user nudge so the model understands it must also
	// give a verbal response — tool calls alone are not enough.
	*conversation = append(*conversation, message.Chat{
		Role:    "user",
		Content: "You called motion tools but included no spoken words. Note: calling tool_movement with command 'speak' is a head-motion cue — it is NOT a verbal response. You MUST write your actual answer as plain text outside any function blocks. Reply now with spoken sentences.",
	})
	if a.cfg.Verbose {
		log.Printf("tool-only turn %d/%d, nudging for verbal response", consecutiveToolOnlyTurns, maxConsecutiveToolOnlyTurns)
	}
	return consecutiveToolOnlyTurns, false
}

// SetThinkingOutputFunc sets an alternate output function used when emitting
// thinking phrases. When set, thinking phrases are published via this function
// instead of the regular outputFunc, allowing them to be flagged with
// Thinking=true so other Actors can ignore them.
func (a *Actor) SetThinkingOutputFunc(f func(content string)) {
	a.thinkingOutputFunc = f
}

// SetSpeakingDoneFunc registers a callback that blocks until the most recently
// emitted phrase has finished being spoken. When set, runThinkingPhrases calls it
// after each thinking phrase so the next phrase is not emitted until the current
// one has been fully played back.
func (a *Actor) SetSpeakingDoneFunc(fn func()) {
	a.speakingDoneFunc = fn
}

// setupThinkingPhrases builds a shuffled deck of thinking phrases and returns a
// function that yields the next phrase, reshuffling when the deck is exhausted.
// A mutex guards the deck so the goroutine from the previous turn may still be
// exiting when the next one starts.
func (a *Actor) setupThinkingPhrases() func() string {
	var mu sync.Mutex
	deck := make([]int, len(a.cfg.ThinkingPhrases))
	for i := range deck {
		deck[i] = i
	}
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	r.Shuffle(len(deck), func(i, j int) { deck[i], deck[j] = deck[j], deck[i] })
	pos := 0
	return func() string {
		mu.Lock()
		defer mu.Unlock()
		if pos >= len(deck) {
			r.Shuffle(len(deck), func(i, j int) { deck[i], deck[j] = deck[j], deck[i] })
			pos = 0
		}
		w := a.cfg.ThinkingPhrases[deck[pos]]
		pos++
		return w
	}
}

// runThinkingPhrases emits thinking phrases via outputFunc (or thinkingOutputFunc
// when set) until done is closed. It is run in a goroutine and should be
// stopped by closing done.
func (a *Actor) runThinkingPhrases(done <-chan struct{}, next func() string) {
	emit := a.outputFunc
	if a.thinkingOutputFunc != nil {
		emit = a.thinkingOutputFunc
	}
	maxInterval := a.cfg.ThinkingInterval
	if maxInterval <= 0 {
		maxInterval = DefaultThinkingInterval
	}
	// half of maxInterval in milliseconds, used as the random base
	halfMs := int64(maxInterval) * 250

	// Say the first pause word immediately.
	select {
	case <-done:
		return
	default:
		emit(next())
		if a.speakingDoneFunc != nil {
			a.speakingDoneFunc()
		}
	}

	for {
		jitter := time.Duration(halfMs+rand.Int63n(halfMs+1)) * time.Millisecond
		select {
		case <-time.After(jitter):
			select {
			case <-done:
				return
			default:
			}
			emit(next())
			if a.speakingDoneFunc != nil {
				a.speakingDoneFunc()
			}
		case <-done:
			return
		}
	}
}

// GetMore gets more input and appends it to the conversation.
func (a *Actor) GetMore(conversation *[]message.Message) bool {
	if a.moreConversationFunc == nil {
		return false
	}

	a.moreConversationFunc(conversation)
	return true
}

// generateTurn applies the chat template, runs inference, and returns the text
// content and any tool calls parsed from the response. hadText is true when
// spoken text was flushed to outputFunc alongside tool calls in the same turn.
// The conversation pointer may be updated to trim old messages if the prompt
// exceeds the model's context window.
func (a *Actor) generateTurn(ctx context.Context, conversation *[]message.Message, stopPausing func()) (string, bool, []message.ToolCall, error) {
	tmplOpts := template.Options{EnableThinking: a.cfg.EnableThinking}
	renderConv := prepareConversationForTemplate(*conversation, a.cfg.ModelFormat)
	prompt, err := template.ApplyWithOptions(a.chatTemplate, renderConv, true, tmplOpts)
	if err != nil {
		return "", false, nil, fmt.Errorf("error applying chat template: %w", err)
	}

	llama.SamplerReset(a.sampler)

	tokens := llama.Tokenize(a.vocab, prompt, true, true)

	// Trim oldest non-system messages if the prompt exceeds the context window.
	if nCtx := int(llama.NCtx(a.llamaCtx)); nCtx > 0 {
		maxPromptTokens := nCtx - a.cfg.MaxTokens
		for len(tokens) > maxPromptTokens && len(*conversation) > 2 {
			// Drop the second message (oldest non-system entry) and re-tokenize.
			if a.cfg.Verbose {
				log.Println("trimming oldest message from conversation to fit context window")
			}
			*conversation = append((*conversation)[:1], (*conversation)[2:]...)
			renderConv = prepareConversationForTemplate(*conversation, a.cfg.ModelFormat)
			prompt, err = template.ApplyWithOptions(a.chatTemplate, renderConv, true, tmplOpts)
			if err != nil {
				return "", false, nil, fmt.Errorf("error applying chat template after trim: %w", err)
			}
			tokens = llama.Tokenize(a.vocab, prompt, true, false)
		}
	}

	mem, err := llama.GetMemory(a.llamaCtx)
	if err != nil {
		return "", false, nil, fmt.Errorf("error getting memory: %w", err)
	}

	// Incremental decode: reuse the cached prompt prefix from the previous turn
	// and only decode the tokens added since then. This avoids re-decoding the
	// entire conversation history on every turn (the main source of latency on
	// slow hardware).
	//
	// Three cases:
	//   1. New prompt extends the cached prefix — trim only the generated tail
	//      (nCachedPrompt..∞) and decode the new tokens from nCachedPrompt.
	//   2. New prompt is shorter (context trimming dropped old messages) but
	//      still covers the system-prompt prefix — restore from nSystemPromptTokens.
	//   3. Neither of the above — full KV clear; re-decode from nSystemPromptTokens
	//      if the system prompt was pre-cached, otherwise from 0.
	var decodeFrom int
	t0 := time.Now()
	if a.nCachedPrompt > 0 && len(tokens) >= a.nCachedPrompt {
		// Case 1: prompt grew — trim the generated tail and decode the delta.
		if ok, rmErr := llama.MemorySeqRm(mem, 0, llama.Pos(a.nCachedPrompt), -1); ok && rmErr == nil {
			decodeFrom = a.nCachedPrompt
			if a.cfg.Verbose {
				log.Printf("[verbose] cache trim: kept %d cached tokens, removed tail (%v)", a.nCachedPrompt, time.Since(t0))
			}
		} else {
			// MemorySeqRm failed (recurrent/hybrid model): full clear, but
			// re-use the system-prompt prefix if it was pre-decoded.
			if clearErr := llama.MemoryClear(mem, true); clearErr != nil {
				return "", false, nil, fmt.Errorf("error clearing memory: %w", clearErr)
			}
			a.nCachedPrompt = 0
			if a.cfg.Verbose {
				log.Printf("[verbose] cache trim failed, full clear (%v)", time.Since(t0))
			}
		}
	} else if a.nCachedPrompt > 0 && a.nSystemPromptTokens > 0 && len(tokens) >= a.nSystemPromptTokens {
		// Case 2: prompt shrank (context trim dropped messages) but the
		// system-prompt prefix is still valid. Discard everything after the
		// system prompt and re-decode from there.
		if ok, rmErr := llama.MemorySeqRm(mem, 0, llama.Pos(a.nSystemPromptTokens), -1); ok && rmErr == nil {
			decodeFrom = a.nSystemPromptTokens
			a.nCachedPrompt = a.nSystemPromptTokens
			if a.cfg.Verbose {
				log.Printf("[verbose] cache trim (shrink): restored to system prefix (%d tokens) (%v)", a.nSystemPromptTokens, time.Since(t0))
			}
		} else {
			// MemorySeqRm failed: full clear.
			if clearErr := llama.MemoryClear(mem, true); clearErr != nil {
				return "", false, nil, fmt.Errorf("error clearing memory: %w", clearErr)
			}
			a.nCachedPrompt = 0
			if a.cfg.Verbose {
				log.Printf("[verbose] cache trim failed (shrink), full clear (%v)", time.Since(t0))
			}
		}
	} else {
		// Case 3: full clear. If the system prompt was pre-decoded (warm-up ran
		// successfully) and the new prompt is long enough, restore it into the
		// cache by decoding only the system-prompt tokens before continuing.
		if clearErr := llama.MemoryClear(mem, true); clearErr != nil {
			return "", false, nil, fmt.Errorf("error clearing memory: %w", clearErr)
		}
		a.nCachedPrompt = 0
		if a.cfg.Verbose {
			log.Printf("[verbose] memory clear: %v", time.Since(t0))
		}
	}

	nBatch := int(llama.NBatch(a.llamaCtx))
	if nBatch <= 0 {
		nBatch = 512
	}
	if a.cfg.Verbose {
		log.Printf("[verbose] prompt decode: %d new tokens (of %d total), batch size %d", len(tokens)-decodeFrom, len(tokens), nBatch)
	}
	t1 := time.Now()
	for i := decodeFrom; i < len(tokens); i += nBatch {
		end := i + nBatch
		if end > len(tokens) {
			end = len(tokens)
		}
		if _, err := llama.Decode(a.llamaCtx, llama.BatchGetOne(tokens[i:end])); err != nil {
			return "", false, nil, fmt.Errorf("error decoding prompt: %w", err)
		}
	}
	if a.cfg.Verbose {
		log.Printf("[verbose] prompt decode done: %v", time.Since(t1))
	}

	a.nCachedPrompt = len(tokens)

	var chunks []string
	pieceBuf := make([]byte, 128)

	generationStopMarkers := message.StopMarkers(a.vocab, a.cfg.ModelFormat)

	t2 := time.Now()
	tokenCount := 0

generateLoop:
	for i := 0; i < a.cfg.MaxTokens; i++ {
		select {
		case <-ctx.Done():
			return "", false, nil, ctx.Err()
		default:
		}

		token := llama.SamplerSample(a.sampler, a.llamaCtx, -1)
		if llama.VocabIsEOG(a.vocab, token) {
			break
		}
		tokenCount++
		if tokenCount == 1 {
			stopPausing()
		}

		n := llama.TokenToPiece(a.vocab, token, pieceBuf, 0, true)
		if n > 0 {
			piece := string(pieceBuf[:n])
			chunks = append(chunks, piece)

			full := strings.Join(chunks, "")
			for _, marker := range generationStopMarkers {
				if idx := strings.Index(full, marker); idx >= 0 {
					chunks = []string{full[:idx]}
					break generateLoop
				}
			}
			// Stop generation if too many tool call blocks have accumulated.
			// Models that flood the output with identical tool calls (e.g.,
			// dozens of consecutive "wait" commands) would otherwise consume
			// the entire MaxTokens budget without producing any spoken text.
			const maxToolCallBlocksPerGeneration = 8
			toolCallCount := strings.Count(full, "</tool_call>") + strings.Count(full, "</function>")
			// Also count Gemma/Gemma3 call: blocks, which use a closing brace
			// instead of an XML tag.
			if a.cfg.ModelFormat == message.FormatGemma || a.cfg.ModelFormat == message.FormatGemma3 {
				toolCallCount += strings.Count(full, "call:tool_")
			}
			if toolCallCount >= maxToolCallBlocksPerGeneration {
				log.Printf("stopping generation: accumulated %d tool call blocks", toolCallCount)
				break generateLoop
			}
		}

		if _, err := llama.Decode(a.llamaCtx, llama.BatchGetOne([]llama.Token{token})); err != nil {
			break
		}
	}

	if a.cfg.Verbose {
		elapsed := time.Since(t2)
		tps := float64(tokenCount) / elapsed.Seconds()
		log.Printf("[verbose] generation: %d tokens in %v (%.2f t/s)", tokenCount, elapsed, tps)
	}

	text := strings.TrimSpace(strings.TrimLeft(strings.Join(chunks, ""), "\n"))

	// Strip <think> / </think> tags before any further processing. Qwen3 with
	// no_think still emits </think> after function blocks, which confuses
	// StripMarkup: its orphaned-</think> handler strips everything before the
	// first </think> (including the text preceding the first function call).
	text = strings.ReplaceAll(text, "<think>", "")
	text = strings.ReplaceAll(text, "</think>", "")
	text = strings.TrimSpace(text)

	if a.cfg.Verbose {
		log.Printf("raw generation: %q", text)
	}

	toolCalls := message.ParseToolCalls(text)
	if len(toolCalls) > 0 {
		var hadText bool
		var spokenText string
		if spokenText = stripActorMarkup(text); spokenText != "" && a.outputFunc != nil {
			spokenText = truncateToSentences(spokenText, a.cfg.MaxSentences)
			remaining := flushSentences(spokenText, a.outputFunc)
			if remaining != "" {
				a.outputFunc(remaining)
			}
			hadText = true
		}
		return spokenText, hadText, toolCalls, nil
	}

	content := stripActorMarkup(text)
	if content != "" && a.outputFunc != nil {
		content = truncateToSentences(content, a.cfg.MaxSentences)
		remaining := flushSentences(content, a.outputFunc)
		if remaining != "" {
			a.outputFunc(remaining)
		}
	}

	return content, false, nil, nil
}

// appendToolCalls adds the assistant's tool call request to the conversation.
// spokenText may be non-empty when the model included spoken words in the same
// turn; it is preserved in the message so subsequent turns see the full context.
func (a *Actor) appendToolCalls(conversation *[]message.Message, toolCalls []message.ToolCall, spokenText string) {
	*conversation = append(*conversation, message.Tool{
		Role:      "assistant",
		Content:   spokenText,
		ToolCalls: toolCalls,
	})
}

// appendAssistant adds the actor's text response to the conversation.
func (a *Actor) appendAssistant(conversation *[]message.Message, content string) {
	if content == "" {
		return
	}

	*conversation = append(*conversation, message.Chat{Role: "assistant", Content: content})
}

// callTools looks up requested tools by name and executes them.
func (a *Actor) callTools(ctx context.Context, toolCalls []message.ToolCall) []message.Message {
	resps := make([]message.Message, 0, len(toolCalls))

	for _, toolCall := range toolCalls {
		tool, exists := a.tools[toolCall.Function.Name]
		if !exists {
			// Fallback: try a normalized name match (strip underscores/hyphens,
			// lowercase) to handle models that elide punctuation in tool names
			// (e.g. Gemma 4 outputs "toolmovement" for "tool_movement").
			norm := normalizeToolName(toolCall.Function.Name)
			for registeredName, t := range a.tools {
				if normalizeToolName(registeredName) == norm {
					tool = t
					exists = true
					break
				}
			}
		}
		if !exists {
			// Fallback: some models (e.g. Phi-4) call movement commands directly
			// by name (e.g. name="slowlook") instead of using tool_movement with
			// command="slowlook". Reroute those to tool_movement.
			if movementTool, ok := a.tools["tool_movement"]; ok {
				switch toolCall.Function.Name {
				case "look", "slowlook", "headshake":
					if toolCall.Function.Arguments == nil {
						toolCall.Function.Arguments = map[string]string{}
					}
					toolCall.Function.Arguments["command"] = toolCall.Function.Name
					toolCall.Function.Name = "tool_movement"
					tool = movementTool
					exists = true
				}
			}
		}
		if !exists {
			log.Printf("\u001b[91mUnknown tool: %s\u001b[0m\n", toolCall.Function.Name)
			continue
		}

		if a.cfg.Verbose {
			log.Printf("\u001b[92m%s(%v)\u001b[0m: ", toolCall.Function.Name, toolCall.Function.Arguments)
		}

		content := tool.Call(ctx, toolCall)
		if strings.Contains(content, `"FAILED"`) {
			log.Printf("\u001b[91m%s\u001b[0m\n", content)
		}

		resps = append(resps, message.ToolResponse{
			Role:    "tool",
			Name:    toolCall.Function.Name,
			Content: content,
		})
	}

	return resps
}

// templateSupportsToolMessages reports whether the model's chat template
// supports dedicated tool and tool-result conversation roles. When false
// (e.g. Gemma 3), tool calls are treated as fire-and-forget physical action
// cues and their results are never appended to the conversation history.
func (a *Actor) templateSupportsToolMessages() bool {
	return a.cfg.ModelFormat != message.FormatGemma3
}

// orphanAngleRE matches bare "angle:N" tokens that models write outside the
// tool-call braces instead of inside them — never spoken text.
var orphanAngleRE = regexp.MustCompile(`\bangle:\d+\b`)

// stageDirectionRE matches multi-word parenthetical stage directions such as
// "(character tilts head to the left)" that models write instead of tool calls.
// Single-word parentheticals like "(five)" are preserved.
var stageDirectionRE = regexp.MustCompile(`\([^)]*\s[^)]*\)`)

// htmlTagRE matches any HTML/XML-style tag including orphaned (unmatched)
// opening or closing tags such as <strong>, </bold>, <em class="foo">, etc.
var htmlTagRE = regexp.MustCompile(`</?[a-zA-Z][a-zA-Z0-9]*[^>]*>`)

// nonASCIIRE matches runs of characters outside the printable ASCII range.
// Tab, newline, carriage return, and the space–tilde range are preserved.
var nonASCIIRE = regexp.MustCompile("[^\t\n\r -~]+")

// jsonResponseRE extracts the string value of a "response" key from a JSON
// object that may be incomplete (missing closing brace). It matches:
//
//	{"response": "value"}
//	{"response":"value"}   (no space)
//	{"response": "value"  (no closing brace — truncated generation)
var jsonResponseRE = regexp.MustCompile(`\{\s*"response"\s*:\s*"((?:[^"]|\\")*)"`)

// missingSpaceAfterPeriodRE matches a lowercase word (two or more letters),
// a single period, then a following letter — uppercase or lowercase
// (e.g. "things.I", "world.Then", "done.then"). Used to insert the missing
// inter-sentence space without disturbing decimals ("3.14"), ellipses
// ("...") or single-letter abbreviations ("e.g.", "i.e.", "U.S.A."). The
// two-letter minimum before the period is what protects the abbreviations
// and the trailing dot of an ellipsis (which is preceded by another dot,
// not a letter).
var missingSpaceAfterPeriodRE = regexp.MustCompile(`([a-z]{2,})\.([A-Za-z])`)

// stripActorMarkup calls message.StripMarkup and then removes artefacts that
// are specific to the talkingheads actor (orphaned angle parameters, stage
// directions). This keeps the yzma library general-purpose.
func stripActorMarkup(s string) string {
	s = message.StripMarkup(s)
	s = htmlTagRE.ReplaceAllString(s, "")
	s = nonASCIIRE.ReplaceAllString(s, "")
	s = orphanAngleRE.ReplaceAllString(s, "")
	s = stageDirectionRE.ReplaceAllString(s, "")
	// Replace newlines with spaces so that adjacent words separated only by a
	// line break (e.g. after a stripped markdown bullet) don't get glued
	// together when downstream code collapses or removes other whitespace.
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "|", " ")
	s = missingSpaceAfterPeriodRE.ReplaceAllString(s, "$1. $2")
	s = strings.TrimSpace(s)
	// Some fine-tuned models (e.g. gemma-3-270M-finetune) wrap every reply in
	// a JSON envelope: {"response": "..."} — possibly without a closing brace
	// when the model truncates its output before finishing the JSON object.
	if strings.HasPrefix(s, "{") {
		// Fast path: complete JSON object with a closing brace.
		if end := strings.LastIndex(s, "}"); end >= 0 {
			var env map[string]any
			if err := json.Unmarshal([]byte(s[:end+1]), &env); err == nil {
				if resp, ok := env["response"].(string); ok && resp != "" {
					return missingSpaceAfterPeriodRE.ReplaceAllString(strings.TrimSpace(resp), "$1. $2")
				}
			}
		}
		// Fallback: incomplete JSON — extract with a regex so truncated output
		// like {"response": "text without closing brace is still unwrapped.
		if m := jsonResponseRE.FindStringSubmatch(s); len(m) == 2 && m[1] != "" {
			return missingSpaceAfterPeriodRE.ReplaceAllString(strings.TrimSpace(m[1]), "$1. $2")
		}
	}
	return s
}

// flushSentences calls fn for each complete sentence found in buf (delimited
// by '.', '!' or '?' followed by whitespace or end-of-string) and returns any
// remaining partial sentence.
func flushSentences(buf string, fn func(string)) string {
	for {
		idx := -1
		for i := 0; i < len(buf); i++ {
			c := buf[i]
			if c == '.' || c == '!' || c == '?' {
				if i+1 >= len(buf) || buf[i+1] == ' ' || buf[i+1] == '\n' || buf[i+1] == '\t' {
					idx = i
					break
				}
			}
		}
		if idx < 0 {
			break
		}
		if sentence := strings.TrimSpace(buf[:idx+1]); sentence != "" {
			fn(sentence)
		}
		buf = strings.TrimLeft(buf[idx+1:], " \n\t")
	}
	return buf
}

// truncateToSentences returns s truncated to at most max complete sentences
// (delimited by '.', '!' or '?' followed by whitespace or end-of-string). When
// max <= 0, s is returned unchanged. Any partial trailing sentence beyond the
// limit is dropped.
func truncateToSentences(s string, max int) string {
	if max <= 0 {
		return s
	}
	count := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '.' || c == '!' || c == '?' {
			if i+1 >= len(s) || s[i+1] == ' ' || s[i+1] == '\n' || s[i+1] == '\t' {
				count++
				if count >= max {
					return strings.TrimSpace(s[:i+1])
				}
			}
		}
	}
	return s
}
