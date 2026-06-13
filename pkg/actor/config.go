package actor

import "github.com/hybridgroup/yzma/pkg/message"

var defaultThinkingPhrases = []string{
	"let me think about that for a moment...",
	"I am going to think about that for a moment...",
	"let me consider that question carefully...",
	"let me ponder that one for a second...",
	"let me try to figure that out...",
	"let me work that out in my head...",
	"give me just a moment to think...",
	"hold on while I gather my thoughts...",
	"that is actually a really interesting question...",
	"well, that is certainly something to consider...",
	"I am going to need a moment for that...",
	"let me take a brief moment to reflect...",
	"now let me think about how to answer...",
	"I am not sure that I should actually answer that...",
	"does that really deserve a response from me?...",
	"I wonder if you would even understand my response...",
	"I am still mulling that one over in my mind...",
	"that one is going to take a little thought...",
	"I want to make sure I get this right...",
	"hmm, let me chew on that for a bit...",
	"give me a second to put this together...",
	"let me roll that around in my head...",
	"hold on, I want to think this through...",
	"let me see if I can put words to it...",
	"that is a question worth pausing on...",
	"I want to give you a thoughtful answer...",
	"let me see how best to explain this...",
	"I am sorting through my thoughts on that...",
	"give me a beat to think this over...",
	"let me piece my thoughts together first...",
	"that one caught me a little off guard...",
	"I am not entirely sure where to begin...",
	"let me try to find the right words...",
	"that is going to take some careful thought...",
	"I need a brief moment to consider that...",
	"hmm, I had not thought about it that way...",
	"let me see if I can do that justice...",
	"I am going to need a second on this...",
	"let me try to wrap my head around that...",
	"that is a surprisingly good question to ask...",
	"I am working through a few possibilities here...",
	"let me weigh that out before I answer...",
	"I want to think about that a bit more...",
	"let me line my thoughts up properly first...",
	"give me a moment to find the answer...",
	"I am still turning that over in my mind...",
	"let me consider how I really feel about it...",
	"I want to be careful with how I phrase this...",
	"that question deserves more than a quick answer...",
	"let me think about whether that is even true...",
	"I am wondering how honest I should be here...",
	"do you really want me to answer that one?...",
	"I am not convinced you actually want to know...",
	"let me decide if this is worth saying aloud...",
	"I am trying to be diplomatic about this one...",
	"let me see if I can phrase this politely...",
	"I am genuinely going to have to think about this...",
}

const (
	DefaultTemperature      = float32(0.6)
	DefaultTopP             = float32(0.95)
	DefaultMinP             = float32(0.05)
	DefaultTopK             = int32(20)
	DefaultMaxTokens        = 2048
	DefaultContextSize      = 4096
	DefaultRepeatPenalty    = float32(1.0) // 1.0 = disabled
	DefaultFreqPenalty      = float32(0.0) // 0.0 = disabled
	DefaultPresencePenalty  = float32(0.0) // 0.0 = disabled
	DefaultDryMultiplier    = float32(0.0) // 0.0 = disabled
	DefaultBatchSize        = uint32(0)    // 0 = use llama.cpp default
	DefaultUBatchSize       = uint32(0)    // 0 = use llama.cpp default
	DefaultThinkingInterval = 5            // seconds between repeated thinking phrases
	DefaultMaxSentences     = 0            // 0 = unlimited
)

// Config holds the tunable parameters for an Actor.
type Config struct {
	Temperature float32
	TopP        float32
	MinP        float32
	TopK        int32
	MaxTokens   int
	ContextSize uint32
	// BatchSize is the logical maximum batch size (n_batch). 0 = use llama.cpp default.
	BatchSize uint32
	// UBatchSize is the physical maximum micro-batch size (n_ubatch). 0 = use llama.cpp default.
	UBatchSize uint32
	// RepeatPenalty penalises recently-seen tokens to reduce repetition.
	// 1.0 = disabled; values around 1.1–1.3 are effective for verbose models.
	RepeatPenalty float32
	// FreqPenalty penalises tokens proportional to how often they have appeared.
	// 0.0 = disabled.
	FreqPenalty float32
	// PresencePenalty penalises any token that has appeared at all.
	// 0.0 = disabled.
	PresencePenalty float32
	// DryMultiplier enables DRY (Don't Repeat Yourself) repetition penalty.
	// 0.0 = disabled; values around 0.8 are a good starting point.
	DryMultiplier float32
	// ModelFormat controls which tool-call grammar instructions are injected
	// into the system prompt. Leave as message.FormatAuto (zero value) to
	// auto-detect from the model path.
	ModelFormat message.Format
	// InjectTools controls whether tool definitions and usage instructions are
	// appended to the system prompt. Defaults to true. Set to false for models
	// that have native tool-call support baked into their chat template.
	InjectTools bool
	// EnableThinking controls whether thinking/reasoning mode is enabled for
	// models that support it (e.g. Qwen3). When false the model is instructed
	// to skip chain-of-thought reasoning and respond directly. Defaults to
	// false to avoid spoken thinking content being published via MQTT.
	EnableThinking bool
	// UseMmap controls whether the model file is loaded via mmap.
	// Defaults to true. Set to false to disable mmap (e.g. when loading from
	// a network filesystem that does not support mmap).
	UseMmap bool
	// Verbose enables verbose logging for debugging.
	Verbose bool
	// ThinkingPhrases is a list of filler phrases (e.g. "let me think...") that the actor
	// speaks immediately after receiving a new request, before the model has
	// produced its first token, to mask the inference startup latency.
	// Set to nil or an empty slice to disable.
	ThinkingPhrases []string
	// ThinkingInterval is the number of seconds between repeated thinking phrases while
	// waiting for the model to produce its first token. Defaults to DefaultThinkingInterval.
	ThinkingInterval int
	// MaxSentences caps the number of sentences spoken per turn. Sentences
	// beyond the limit are dropped before being emitted to outputFunc.
	// 0 = unlimited.
	MaxSentences int
}

// DefaultConfig returns a Config populated with sensible defaults.
func DefaultConfig() Config {
	return Config{
		Temperature:      DefaultTemperature,
		TopP:             DefaultTopP,
		MinP:             DefaultMinP,
		TopK:             DefaultTopK,
		MaxTokens:        DefaultMaxTokens,
		ContextSize:      DefaultContextSize,
		BatchSize:        DefaultBatchSize,
		UBatchSize:       DefaultUBatchSize,
		RepeatPenalty:    DefaultRepeatPenalty,
		FreqPenalty:      DefaultFreqPenalty,
		PresencePenalty:  DefaultPresencePenalty,
		DryMultiplier:    DefaultDryMultiplier,
		InjectTools:      true,
		EnableThinking:   false,
		UseMmap:          true,
		Verbose:          false,
		ThinkingPhrases:  defaultThinkingPhrases,
		ThinkingInterval: DefaultThinkingInterval,
		MaxSentences:     DefaultMaxSentences,
	}
}
