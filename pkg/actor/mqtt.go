package actor

import (
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"strings"
	"sync"
	"unicode"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/hybridgroup/yzma/pkg/message"
	"github.com/talkingheads2053/talkingheads/pkg/commands"
)

// MQTTListener connects to an MQTT broker and wires up an Actor to receive
// user input from "direction/<name>" and publish responses to "speak/<name>".
//
// The published response payload is {"who":"<name>","what":"<content>"},
// which is compatible with the speak/# subscription in pkg/dialogue.
type MQTTListener struct {
	name          string
	client        mqtt.Client
	incoming      chan string
	heard         chan string
	done          chan struct{}
	closeOnce     sync.Once
	verbose       bool
	preprocessCB  func(*[]message.Message)
	eventsCh      chan<- string
	lastSpeaker   string
	lastSpeakerMu sync.RWMutex
	turnSpeaker   string
	lastLines     map[string][]string
	lang          string

	repeatLen int
	heardMu   sync.Mutex
	recent    []string

	speakMu   sync.Mutex
	speakCond *sync.Cond
	speakN    int // number of phrases issued but not yet StatusStopped

	movement *Movement
	runCalls func([]message.ToolCall)
	cueMu    sync.Mutex
	cue      []message.ToolCall // calls for the next phrase published
	cued     []*cuedPhrase      // published phrases in order
}

// cuedPhrase holds the calls for one published phrase.
type cuedPhrase struct {
	calls   []message.ToolCall
	dropped bool
}

// SetEventsCh registers a channel that receives human-readable event strings
// (e.g. received directions). Must be called before the actor starts running.
func (l *MQTTListener) SetEventsCh(ch chan<- string) {
	l.eventsCh = ch
}

// SetMovement sets the head movement and how to run movement tool calls.
// Calls cued for a phrase run when the dialogue starts speaking it.
func (l *MQTTListener) SetMovement(m *Movement, run func([]message.ToolCall)) {
	l.movement = m
	l.runCalls = run
}

// Cue holds calls until the next phrase is published.
func (l *MQTTListener) Cue(calls []message.ToolCall) {
	l.cueMu.Lock()
	defer l.cueMu.Unlock()
	l.cue = append(l.cue, calls...)
}

// DirectionPending reports whether a direction is waiting to be answered.
func (l *MQTTListener) DirectionPending() bool {
	return len(l.incoming) > 0
}

// SetLang sets the language code used for the text sent to the model.
func (l *MQTTListener) SetLang(lang string) {
	l.lang = lang
}

// SetRepeatGuard makes RepeatsHeard report a sentence that shares n letters
// in a row with a recent line from another actor or from this one. 0 turns it off.
func (l *MQTTListener) SetRepeatGuard(n int) {
	l.repeatLen = n
}

// recentHeardLines is how many recent lines RepeatsHeard checks.
const recentHeardLines = 12

func (l *MQTTListener) rememberHeard(text string) {
	if l.repeatLen <= 0 {
		return
	}
	l.heardMu.Lock()
	defer l.heardMu.Unlock()
	l.recent = append(l.recent, letters(text))
	if len(l.recent) > recentHeardLines {
		l.recent = l.recent[len(l.recent)-recentHeardLines:]
	}
}

// RepeatsHeard reports whether sentence copies a run of letters from a line
// this actor or another actor said recently.
func (l *MQTTListener) RepeatsHeard(sentence string) bool {
	if l.repeatLen <= 0 {
		return false
	}
	s := letters(sentence)
	l.heardMu.Lock()
	defer l.heardMu.Unlock()
	for _, h := range l.recent {
		if longestCommonRun(s, h) >= l.repeatLen {
			if l.verbose {
				log.Printf("Sentence repeats a recent line: %s\n", sentence)
			}
			return true
		}
	}
	return false
}

// letters lowercases s and keeps only its letters and digits.
func letters(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		return -1
	}, s)
}

// longestCommonRun returns the length in runes of the longest substring of a
// that is also in b.
func longestCommonRun(as, bs string) int {
	a, b := []rune(as), []rune(bs)
	prev := make([]int, len(b)+1)
	curr := make([]int, len(b)+1)
	best := 0
	for i := range a {
		for j := range b {
			if a[i] == b[j] {
				curr[j+1] = prev[j] + 1
				best = max(best, curr[j+1])
			} else {
				curr[j+1] = 0
			}
		}
		prev, curr = curr, prev
	}
	return best
}

// emit sends a message to eventsCh when set, without blocking.
func (l *MQTTListener) emit(msg string) {
	if l.eventsCh == nil {
		return
	}
	select {
	case l.eventsCh <- msg:
	default:
	}
}

// NewMQTTListener connects to the broker, subscribes to "direction/<name>" for
// direct prompts and "speak/#" to hear other actors, and returns a
// ready-to-use MQTTListener.
func NewMQTTListener(name, server string, verbose bool) (*MQTTListener, error) {
	options := mqtt.NewClientOptions()
	options.AddBroker(server)
	options.SetClientID("actor-" + name)
	options.KeepAlive = 300

	if verbose {
		log.Printf("Connecting to MQTT broker at %s\n", server)
	}
	client := mqtt.NewClient(options)
	token := client.Connect()
	if token.Wait() && token.Error() != nil {
		return nil, token.Error()
	}

	l := &MQTTListener{
		name:     name,
		client:   client,
		incoming: make(chan string, 32),
		heard:    make(chan string, 64),
		done:     make(chan struct{}),
		verbose:  verbose,
	}
	l.speakCond = sync.NewCond(&l.speakMu)

	if err := subscribe(client, "direction/"+name, l.handleDirection); err != nil {
		client.Disconnect(250)
		return nil, err
	}
	if verbose {
		log.Printf("Subscribed to direction/%s\n", name)
	}
	if err := subscribe(client, "speak/#", l.handleSpeak); err != nil {
		client.Disconnect(250)
		return nil, err
	}
	if verbose {
		log.Printf("Subscribed to speak/#\n")
	}
	if err := subscribe(client, "speaking/#", l.handleSpeakingStatus); err != nil {
		client.Disconnect(250)
		return nil, err
	}
	if verbose {
		log.Printf("Subscribed to speaking/#\n")
	}

	return l, nil
}

// subscribe subscribes to a single MQTT topic and logs the result.
func subscribe(client mqtt.Client, topic string, handler mqtt.MessageHandler) error {
	token := client.Subscribe(topic, 0, handler)
	if token.Wait() && token.Error() != nil {
		return token.Error()
	}
	return nil
}

func (l *MQTTListener) handleDirection(_ mqtt.Client, msg mqtt.Message) {
	var a commands.Direction
	if err := json.Unmarshal(msg.Payload(), &a); err != nil {
		log.Printf("Failed to unmarshal direction message: %v\n", err)
		return
	}
	if l.verbose {
		log.Printf("Received direction message from Director: %s\n", a.What)
	}

	text := a.What
	if a.Respond {
		target := a.RespondTo
		if target == "" {
			l.lastSpeakerMu.RLock()
			target = l.lastSpeaker
			l.lastSpeakerMu.RUnlock()
		}
		if target != "" {
			p := phrasesFor(l.lang)
			if text == "" {
				text = fmt.Sprintf(p.respondNow, target)
			} else {
				text = endSentence(text, p.sentenceEnd) + fmt.Sprintf(p.respondAlso, target)
			}
			if line := l.lastLine(target); line != "" {
				text = fmt.Sprintf(p.respondQuote, target, line) + text
			}
		}
	}

	l.emit(fmt.Sprintf("Director: %q", text))
	l.enqueue(text)
}

func (l *MQTTListener) handleSpeakingStatus(_ mqtt.Client, msg mqtt.Message) {
	var s commands.Speaking
	if err := json.Unmarshal(msg.Payload(), &s); err != nil {
		log.Printf("Failed to unmarshal speaking message: %v\n", err)
		return
	}
	if s.Who == l.name {
		switch s.Status {
		case commands.StatusSpeaking:
			if calls := l.nextCued(); len(calls) > 0 && l.runCalls != nil {
				l.runCalls(calls)
			}
		case commands.StatusStopped:
			l.speakMu.Lock()
			if l.speakN > 0 {
				l.speakN--
			}
			idle := l.speakN == 0
			l.speakCond.Broadcast()
			l.speakMu.Unlock()
			if idle && l.movement != nil {
				l.movement.RestSoon()
			}
		}
		return
	}
	if s.Status == commands.StatusStopped && l.movement != nil {
		l.movement.RestSoon()
	}
}

// takeCue gives the cued calls to a phrase being published.
func (l *MQTTListener) takeCue() *cuedPhrase {
	l.cueMu.Lock()
	defer l.cueMu.Unlock()
	phrase := &cuedPhrase{calls: l.cue}
	l.cue = nil
	l.cued = append(l.cued, phrase)
	return phrase
}

// nextCued returns the calls cued for the phrase the dialogue started speaking.
func (l *MQTTListener) nextCued() []message.ToolCall {
	l.cueMu.Lock()
	defer l.cueMu.Unlock()
	for len(l.cued) > 0 {
		p := l.cued[0]
		l.cued = l.cued[1:]
		if !p.dropped {
			return p.calls
		}
	}
	return nil
}

func (l *MQTTListener) handleSpeak(_ mqtt.Client, msg mqtt.Message) {
	var s commands.Speak
	if err := json.Unmarshal(msg.Payload(), &s); err != nil {
		log.Printf("Failed to unmarshal speak message: %v\n", err)
		return
	}
	// Ignore thinking phrases — they are filler content spoken while the model
	// is generating and should not enter other actors' conversation context.
	if s.Thinking {
		return
	}
	l.lastSpeakerMu.Lock()
	if s.Who != l.turnSpeaker {
		if l.lastLines == nil {
			l.lastLines = make(map[string][]string)
		}
		l.lastLines[s.Who] = nil
		l.turnSpeaker = s.Who
	}
	l.lastLines[s.Who] = append(l.lastLines[s.Who], s.What)
	// Ignore own messages to avoid self-referential loops.
	if s.Who == l.name {
		l.lastSpeakerMu.Unlock()
		return
	}
	l.lastSpeaker = s.Who
	l.lastSpeakerMu.Unlock()
	if l.verbose {
		log.Printf("Heard %s say: %s\n", s.Who, s.What)
	}
	l.rememberHeard(s.What)
	l.enqueueHeard(fmt.Sprintf(phrasesFor(l.lang).says, s.Who, s.What))
}

// lastLine returns what who said in their most recent turn.
func (l *MQTTListener) lastLine(who string) string {
	l.lastSpeakerMu.RLock()
	defer l.lastSpeakerMu.RUnlock()
	return strings.Join(l.lastLines[who], phrasesFor(l.lang).lineJoin)
}

func (l *MQTTListener) enqueueHeard(text string) {
	select {
	case l.heard <- text:
	case <-l.done:
	}
}

// drainHeard appends all buffered heard-speech messages to the conversation
// without blocking. These messages provide context from other actors but do
// not trigger a response by themselves.
func (l *MQTTListener) drainHeard(conversation *[]message.Message) {
	for {
		select {
		case text, ok := <-l.heard:
			if !ok || text == "" {
				return
			}
			*conversation = append(*conversation, message.Chat{Role: "user", Content: text})
		default:
			return
		}
	}
}

// drainIncoming appends all buffered Direction messages to the conversation
// without blocking.
func (l *MQTTListener) drainIncoming(conversation *[]message.Message) {
	for {
		select {
		case text, ok := <-l.incoming:
			if !ok || text == "" {
				return
			}
			*conversation = append(*conversation, message.Chat{Role: "user", Content: text})
		default:
			return
		}
	}
}

func (l *MQTTListener) enqueue(text string) {
	select {
	case l.incoming <- text:
	case <-l.done:
	}
}

// SetPreprocessCallback registers a callback that is invoked each time a
// heard Speak message is appended to the conversation while waiting for a
// Direction. The callback is called with the updated conversation so it can
// pre-decode the new context into the model's KV cache. Pass nil to disable.
func (l *MQTTListener) SetPreprocessCallback(cb func(*[]message.Message)) {
	l.preprocessCB = cb
}

// MoreFunc returns a moreConversationFunc that blocks until a Direction
// arrives, then appends any buffered heard-speech context followed by the
// Direction. Heard speech from other actors is accumulated in the conversation
// but never triggers a response on its own.
//
// When a preprocessing callback has been registered via SetPreprocessCallback,
// each incoming heard Speak message triggers a pre-decode into the KV cache so
// that the Actor is ready to respond with minimal latency when a Direction
// arrives.
func (l *MQTTListener) MoreFunc() func(*[]message.Message) {
	return func(conversation *[]message.Message) {
		// Drain any heard speech that accumulated during the previous
		// generation cycle, adding it as context.
		l.drainHeard(conversation)

		if l.preprocessCB == nil {
			// No preprocessing: block until a Direction arrives or the listener
			// is closed, then drain any remaining heard speech and return.
			var directionText string
			select {
			case text, ok := <-l.incoming:
				if !ok || text == "" {
					return
				}
				directionText = text
			case <-l.done:
				return
			}
			l.drainHeard(conversation)
			*conversation = append(*conversation, message.Chat{Role: "user", Content: directionText})
			l.drainIncoming(conversation)
			return
		}

		// Preprocessing mode: wait for heard speech or a Direction. Each time
		// a heard message arrives, add it to the conversation and invoke the
		// preprocessing callback so the model's KV cache stays current.
		for {
			select {
			case text, ok := <-l.heard:
				if !ok || text == "" {
					continue
				}
				*conversation = append(*conversation, message.Chat{Role: "user", Content: text})
				l.drainHeard(conversation)
				l.preprocessCB(conversation)
			case text, ok := <-l.incoming:
				if !ok || text == "" {
					return
				}
				// A Direction arrived — drain any final heard speech so the
				// Actor has the most up-to-date context, then append the Direction.
				l.drainHeard(conversation)
				*conversation = append(*conversation, message.Chat{Role: "user", Content: text})
				l.drainIncoming(conversation)
				return
			case <-l.done:
				return
			}
		}
	}
}

// OutputFunc returns an outputFunc that publishes the actor's response to
// "speak/<name>" as {"who":"<name>","what":"<content>"}.
func (l *MQTTListener) OutputFunc() func(string) {
	return l.makeOutputFunc(false)
}

// ThinkingOutputFunc returns an outputFunc that publishes thinking phrases to
// "speak/<name>" with Thinking set to true, signalling to other Actors that
// the message is filler and should not be added to their conversation context.
func (l *MQTTListener) ThinkingOutputFunc() func(string) {
	return l.makeOutputFunc(true)
}

func (l *MQTTListener) makeOutputFunc(thinking bool) func(string) {
	return func(content string) {
		if len(content) == 0 {
			return
		}

		content = removeEmoji(content)
		content = removeOtherUnwantedChars(content)
		if len(content) == 0 {
			return
		}
		if !thinking {
			l.rememberHeard(content)
		}
		payload, err := json.Marshal(commands.Speak{Who: l.name, What: content, Thinking: thinking})
		if err != nil {
			log.Printf("failed to marshal response: %v\n", err)
			return
		}

		// Count this phrase as in-flight before launching the goroutine so that
		// WaitSpeakingDoneFunc callers see speakN > 0 before StatusStopped arrives.
		l.speakMu.Lock()
		l.speakN++
		l.speakMu.Unlock()

		phrase := l.takeCue()

		topic := "speak/" + l.name
		go func() {
			token := l.client.Publish(topic, 0, false, payload)
			token.Wait()
			if token.Error() != nil {
				log.Printf("failed to publish response to %s: %v\n", topic, token.Error())
				l.cueMu.Lock()
				phrase.dropped = true
				l.cueMu.Unlock()
				if len(phrase.calls) > 0 && l.runCalls != nil {
					l.runCalls(phrase.calls)
				}
				// Publish failed — the dialogue system will never send StatusStopped,
				// so decrement here to avoid blocking waiters indefinitely.
				l.speakMu.Lock()
				if l.speakN > 0 {
					l.speakN--
				}
				l.speakCond.Broadcast()
				l.speakMu.Unlock()
			}
		}()
	}
}

// WaitSpeakingDoneFunc returns a function that blocks until all in-flight
// phrases (issued via OutputFunc or ThinkingOutputFunc) have received a
// StatusStopped acknowledgement from the dialogue system. It returns
// immediately if the listener is closed.
func (l *MQTTListener) WaitSpeakingDoneFunc() func() {
	return func() {
		l.speakMu.Lock()
		defer l.speakMu.Unlock()
		for l.speakN > 0 {
			select {
			case <-l.done:
				return
			default:
			}
			l.speakCond.Wait()
		}
	}
}

// Close unblocks any pending MoreFunc call and disconnects from the broker.
// Safe to call multiple times.
func (l *MQTTListener) Close() {
	l.closeOnce.Do(func() {
		close(l.done)
		// Wake any goroutines blocked in WaitSpeakingDoneFunc.
		l.speakCond.Broadcast()
		if l.client != nil {
			l.client.Disconnect(250)
		}
	})
}

func removeEmoji(str string) string {
	// Emoji blocks only, so CJK text is kept. See https://www.unicode.org/charts/ for the block ranges.
	emojiPattern := "[\U0001F100-\U0001F2FF\U0001F300-\U0001F5FF\U0001F600-\U0001F64F\U0001F680-\U0001F6FF\U0001F700-\U0001FAFF\U00002600-\U000026FF\U00002702-\U000027B0\U000024C2\U0000FE0F\U0000200D]+"
	re := regexp.MustCompile(emojiPattern)
	// Replace matched emoji with an empty string to remove it
	return re.ReplaceAllString(str, "")
}

func removeOtherUnwantedChars(str string) string {
	// Regex pattern to match control characters and symbols that cannot be read aloud
	// (e.g., markdown formatting: *, _, #, `, ~, |, \).
	unwantedPattern := `[\x00-\x1F\x7F*_#` + "`~|\\\\]+"
	re := regexp.MustCompile(unwantedPattern)
	return re.ReplaceAllString(str, "")
}
