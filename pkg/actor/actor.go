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
	"unicode"
	"unicode/utf8"

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

	// cachedTokens are the prompt tokens held in the KV cache from the last decode.
	// Each turn only decodes what comes after the prefix shared with them.
	cachedTokens []llama.Token

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

	log.Printf("llama.cpp %s", llama.Version())
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

// warmUpSystemPrompt decodes the system prompt into the KV cache so later turns
// reuse it. sysContent must be the final system message content.
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

	a.cachedTokens = nil
	if a.cfg.Verbose {
		log.Printf("[verbose] warm-up: decoding %d system prompt tokens", len(tokens))
	}
	return a.syncCache(ctx, tokens)
}

// preDecodeConversation decodes the conversation into the KV cache without
// sampling, so generateTurn only has to decode the Direction on top.
func (a *Actor) preDecodeConversation(ctx context.Context, conversation *[]message.Message) error {
	tokens, err := a.renderTokens(conversation, false)
	if err != nil {
		return err
	}
	return a.syncCache(ctx, tokens)
}

// renderTokens applies the chat template and tokenizes the result. It drops the
// oldest non-system messages until the prompt leaves room for MaxTokens.
func (a *Actor) renderTokens(conversation *[]message.Message, addAssistant bool) ([]llama.Token, error) {
	tmplOpts := template.Options{EnableThinking: a.cfg.EnableThinking}
	maxPromptTokens := int(llama.NCtx(a.llamaCtx)) - a.cfg.MaxTokens
	for {
		renderConv := prepareConversationForTemplate(*conversation, a.cfg.ModelFormat)
		prompt, err := template.ApplyWithOptions(a.chatTemplate, renderConv, addAssistant, tmplOpts)
		if err != nil {
			return nil, fmt.Errorf("error applying chat template: %w", err)
		}
		tokens := llama.Tokenize(a.vocab, prompt, true, true)
		if maxPromptTokens <= 0 || len(tokens) <= maxPromptTokens || len(*conversation) <= 2 {
			return tokens, nil
		}
		if a.cfg.Verbose {
			log.Println("trimming oldest message from conversation to fit context window")
		}
		*conversation = append((*conversation)[:1], (*conversation)[2:]...)
	}
}

// syncCache keeps the part of the KV cache shared with tokens and decodes the rest.
// The last token is always decoded so its logits are ready for sampling.
func (a *Actor) syncCache(ctx context.Context, tokens []llama.Token) error {
	mem, err := llama.GetMemory(a.llamaCtx)
	if err != nil {
		return fmt.Errorf("error getting memory: %w", err)
	}

	keep := commonPrefix(a.cachedTokens, tokens)
	if keep >= len(tokens) {
		keep = len(tokens) - 1
	}
	t0 := time.Now()
	if keep > 0 {
		if ok, rmErr := llama.MemorySeqRm(mem, 0, llama.Pos(keep), -1); !ok || rmErr != nil {
			keep = 0
		}
	}
	if keep <= 0 {
		keep = 0
		if err := llama.MemoryClear(mem, true); err != nil {
			return fmt.Errorf("error clearing memory: %w", err)
		}
	}
	a.cachedTokens = nil

	nBatch := int(llama.NBatch(a.llamaCtx))
	if nBatch <= 0 {
		nBatch = 512
	}
	if a.cfg.Verbose {
		log.Printf("[verbose] cache: kept %d tokens, decoding %d, batch size %d", keep, len(tokens)-keep, nBatch)
	}
	for i := keep; i < len(tokens); i += nBatch {
		if err := ctx.Err(); err != nil {
			return err
		}
		end := min(i+nBatch, len(tokens))
		if _, err := llama.Decode(a.llamaCtx, llama.BatchGetOne(tokens[i:end])); err != nil {
			return fmt.Errorf("error decoding prompt: %w", err)
		}
	}
	if a.cfg.Verbose {
		log.Printf("[verbose] prompt decode done: %v", time.Since(t0))
	}

	a.cachedTokens = tokens
	return nil
}

func commonPrefix(a, b []llama.Token) int {
	n := 0
	for n < len(a) && n < len(b) && a[n] == b[n] {
		n++
	}
	return n
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
			Content: phrasesFor(a.cfg.Lang).noWords,
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
		Content: phrasesFor(a.cfg.Lang).noWordsSpeak,
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
	llama.SamplerReset(a.sampler)

	tokens, err := a.renderTokens(conversation, true)
	if err != nil {
		return "", false, nil, err
	}
	if err := a.syncCache(ctx, tokens); err != nil {
		return "", false, nil, err
	}

	var buf strings.Builder
	pieceBuf := make([]byte, 128)
	stream := &sentenceStream{emit: a.outputFunc, max: a.cfg.MaxSentences}

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
			buf.Write(pieceBuf[:n])

			full := buf.String()
			for _, marker := range generationStopMarkers {
				if idx := strings.Index(full, marker); idx >= 0 {
					buf.Reset()
					buf.WriteString(full[:idx])
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

			stream.feed(full)
			if stream.full() {
				break generateLoop
			}
		}

		if _, err := llama.Decode(a.llamaCtx, llama.BatchGetOne([]llama.Token{token})); err != nil {
			a.cachedTokens = nil
			break
		}
		// Keep generated tokens cached so the next turn reuses this reply.
		a.cachedTokens = append(a.cachedTokens, token)
	}

	if a.cfg.Verbose {
		elapsed := time.Since(t2)
		tps := float64(tokenCount) / elapsed.Seconds()
		log.Printf("[verbose] generation: %d tokens in %v (%.2f t/s)", tokenCount, elapsed, tps)
	}

	text := stripThinkTags(buf.String())
	if a.cfg.Verbose {
		log.Printf("raw generation: %q", text)
	}

	spoken := stream.finish(buf.String())
	hadText := spoken != "" && a.outputFunc != nil
	if toolCalls := message.ParseToolCalls(text); len(toolCalls) > 0 {
		return spoken, hadText, toolCalls, nil
	}
	return spoken, false, nil, nil
}

// stripThinkTags removes <think> and </think>. Qwen3 with no_think still emits
// </think> after function blocks, which makes StripMarkup drop the text before it.
func stripThinkTags(s string) string {
	s = strings.ReplaceAll(s, "<think>", "")
	s = strings.ReplaceAll(s, "</think>", "")
	return strings.TrimSpace(s)
}

// sentenceStream speaks sentences while the model is still generating. It stops
// streaming once markup shows up and leaves the rest to finish.
type sentenceStream struct {
	emit    func(string)
	max     int
	count   int
	pos     int
	stopped bool
	spoken  []string
}

// feed speaks the complete sentences in raw that have not been spoken yet.
func (s *sentenceStream) feed(raw string) {
	if s.stopped || s.full() {
		return
	}
	pending := raw[s.pos:]
	if strings.ContainsAny(pending, "<{[(*`（") || strings.Contains(pending, "call:") {
		s.stopped = true
		return
	}
	if end := lastSentenceEnd(pending); end >= 0 {
		s.say(pending[:end])
		s.pos += end
	}
}

// finish speaks what is left in raw and returns everything spoken this turn.
func (s *sentenceStream) finish(raw string) string {
	if s.pos < len(raw) {
		s.say(stripThinkTags(raw[s.pos:]))
		s.pos = len(raw)
	}
	return strings.Join(s.spoken, " ")
}

func (s *sentenceStream) say(segment string) {
	if rest := flushSentences(stripActorMarkup(segment), s.add); rest != "" {
		s.add(rest)
	}
}

func (s *sentenceStream) add(sentence string) {
	if s.full() {
		return
	}
	s.count++
	s.spoken = append(s.spoken, sentence)
	if s.emit != nil {
		s.emit(sentence)
	}
}

func (s *sentenceStream) full() bool {
	return s.max > 0 && s.count >= s.max
}

// lastSentenceEnd returns the index just past the last sentence terminator in
// s, or -1. An ASCII terminator at the very end does not count yet.
func lastSentenceEnd(s string) int {
	last := -1
	for i := 0; i < len(s); i++ {
		if end, ok := sentenceEnd(s, i, false); ok {
			last = end
		}
	}
	return last
}

// sentenceEnd reports whether a sentence terminator starts at byte i of s and
// returns the index just past it. Japanese terminators need no trailing space.
func sentenceEnd(s string, i int, final bool) (int, bool) {
	switch s[i] {
	case '.', '!', '?':
		if i+1 >= len(s) {
			return i + 1, final
		}
		switch s[i+1] {
		case ' ', '\n', '\t':
			return i + 1, true
		}
		return 0, false
	}
	r, n := utf8.DecodeRuneInString(s[i:])
	switch r {
	case '。', '！', '？':
		return i + n, true
	}
	return 0, false
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

// jaStageDirectionRE matches Japanese parentheticals such as "（うなずく）",
// which have no spaces to tell them apart from single words.
var jaStageDirectionRE = regexp.MustCompile(`（[^（）]*）|\([^()]*[\p{Hiragana}\p{Katakana}\p{Han}][^()]*\)`)

// htmlTagRE matches any HTML/XML-style tag including orphaned (unmatched)
// opening or closing tags such as <strong>, </bold>, <em class="foo">, etc.
var htmlTagRE = regexp.MustCompile(`</?[a-zA-Z][a-zA-Z0-9]*[^>]*>`)

// nonASCIIRE matches runs of characters outside printable ASCII and the
// Japanese kana, kanji, CJK punctuation and full-width forms.
var nonASCIIRE = regexp.MustCompile("[^\t\n\r -~\u3000-\u30FF\u3400-\u4DBF\u4E00-\u9FFF\uFF01-\uFF5E\uFF61-\uFF9F]+")

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
	s = jaStageDirectionRE.ReplaceAllString(s, "")
	s = dropUnmatchedParens(s)
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

// dropUnmatchedParens removes ASCII or full-width parentheses that have no
// partner, such as a stray ')' the model writes at the end of a reply.
func dropUnmatchedParens(s string) string {
	if !strings.ContainsAny(s, "()（）") {
		return s
	}
	drop := make(map[int]bool)
	var open []int
	for i, r := range s {
		switch r {
		case '(', '（':
			open = append(open, i)
		case ')', '）':
			if len(open) > 0 {
				open = open[:len(open)-1]
			} else {
				drop[i] = true
			}
		}
	}
	for _, i := range open {
		drop[i] = true
	}
	if len(drop) == 0 {
		return s
	}
	var b strings.Builder
	for i, r := range s {
		if !drop[i] {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// flushSentences calls fn for each complete sentence found in buf (see
// sentenceEnd) and returns any remaining partial sentence.
func flushSentences(buf string, fn func(string)) string {
	for {
		end := -1
		for i := 0; i < len(buf); i++ {
			if e, ok := sentenceEnd(buf, i, true); ok {
				end = e
				break
			}
		}
		if end < 0 {
			break
		}
		if sentence := strings.TrimSpace(buf[:end]); sentence != "" {
			fn(sentence)
		}
		buf = strings.TrimLeftFunc(buf[end:], unicode.IsSpace)
	}
	return buf
}

// truncateToSentences keeps at most max complete sentences of s and drops any
// partial trailing sentence. When max <= 0, s is returned unchanged.
func truncateToSentences(s string, max int) string {
	if max <= 0 {
		return s
	}
	count := 0
	for i := 0; i < len(s); i++ {
		if end, ok := sentenceEnd(s, i, true); ok {
			count++
			if count >= max {
				return strings.TrimSpace(s[:end])
			}
		}
	}
	return s
}
