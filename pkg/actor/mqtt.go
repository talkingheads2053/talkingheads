package actor

import (
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"sync"

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
	name           string
	commander      Commander
	client         mqtt.Client
	incoming       chan string
	heard          chan string
	done           chan struct{}
	closeOnce      sync.Once
	verbose        bool
	preprocessCB   func(*[]message.Message)
	eventsCh       chan<- string
	lastSpeaker    string
	lastSpeakerMu  sync.RWMutex
	actorPositions []string
}

// SetEventsCh registers a channel that receives human-readable event strings
// (e.g. received directions). Must be called before the actor starts running.
func (l *MQTTListener) SetEventsCh(ch chan<- string) {
	l.eventsCh = ch
}

// SetActorPositions records the left-to-right stage order of all actors as
// seen from the audience (e.g. ["gemmai", "phineas", "qwentin"]). When set,
// this Actor will issue a look command towards any other Actor that starts
// speaking. Pass nil or an empty slice to disable the behaviour.
func (l *MQTTListener) SetActorPositions(positions []string) {
	l.actorPositions = positions
}

// lookAngleFor returns the servo angle (40–140) that this Actor should use to
// face the named speaker, and reports whether positioning data is available.
// Actors are assumed to face the audience; 0° is full right, 90° is center,
// 180° is full left from each Actor's own perspective. The angle always falls
// in [40, 55] when the speaker is to the right, or [125, 140] when to the
// left, with the exact value scaling with relative distance between actors.
func (l *MQTTListener) lookAngleFor(speaker string) (int, bool) {
	if len(l.actorPositions) < 2 {
		return 0, false
	}
	myIdx, speakerIdx := -1, -1
	for i, name := range l.actorPositions {
		if name == l.name {
			myIdx = i
		}
		if name == speaker {
			speakerIdx = i
		}
	}
	if myIdx < 0 || speakerIdx < 0 || myIdx == speakerIdx {
		return 0, false
	}
	n := len(l.actorPositions)
	diff := speakerIdx - myIdx
	absDiff := diff
	if absDiff < 0 {
		absDiff = -absDiff
	}
	// Scale a 15° spread across the band based on relative distance (closer = less extreme).
	spread := absDiff * 15 / (n - 1)
	var angle int
	if diff > 0 {
		// Higher index = to this Actor's left → angle in [125, 140].
		angle = 125 + spread
	} else {
		// Lower index = to this Actor's right → angle in [40, 55].
		angle = 55 - spread
	}
	if angle < 40 {
		angle = 40
	}
	if angle > 140 {
		angle = 140
	}
	return angle, true
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
// ready-to-use MQTTListener. If commander is nil, a LogCommander is used.
func NewMQTTListener(name, server string, commander Commander, verbose bool) (*MQTTListener, error) {
	if commander == nil {
		commander = &LogCommander{}
	}
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
		name:      name,
		commander: commander,
		client:    client,
		incoming:  make(chan string, 32),
		heard:     make(chan string, 64),
		done:      make(chan struct{}),
		verbose:   verbose,
	}

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
		l.lastSpeakerMu.RLock()
		lastSpeaker := l.lastSpeaker
		l.lastSpeakerMu.RUnlock()
		if lastSpeaker != "" {
			if text == "" {
				text = "Now respond directly to " + lastSpeaker + "."
			} else {
				text = text + " Respond directly to " + lastSpeaker + "."
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
		// Own speaking status.
		switch s.Status {
		case commands.StatusSpeaking:
			if err := l.commander.Send("speak"); err != nil {
				if l.verbose {
					log.Printf("failed to send speak command: %v\n", err)
				}
			}
		case commands.StatusStopped:
			if err := l.commander.Send("stop"); err != nil {
				if l.verbose {
					log.Printf("failed to send stop command: %v\n", err)
				}
			}
		}
		return
	}
	// Another actor's speaking status — look towards them and enter waiting while they speak.
	switch s.Status {
	case commands.StatusSpeaking:
		if angle, ok := l.lookAngleFor(s.Who); ok {
			if err := l.commander.Send(fmt.Sprintf("look %d", angle)); err != nil {
				if l.verbose {
					log.Printf("failed to send look command: %v\n", err)
				}
			}
		} else {
			if err := l.commander.Send("wait"); err != nil {
				if l.verbose {
					log.Printf("failed to send wait command: %v\n", err)
				}
			}
		}
	case commands.StatusStopped:
		if err := l.commander.Send("stop"); err != nil {
			if l.verbose {
				log.Printf("failed to send stop command: %v\n", err)
			}
		}
	}
}

func (l *MQTTListener) handleSpeak(_ mqtt.Client, msg mqtt.Message) {
	var s commands.Speak
	if err := json.Unmarshal(msg.Payload(), &s); err != nil {
		log.Printf("Failed to unmarshal speak message: %v\n", err)
		return
	}
	// Ignore own messages to avoid self-referential loops.
	if s.Who == l.name {
		return
	}
	// Ignore thinking phrases — they are filler content spoken while the model
	// is generating and should not enter other actors' conversation context.
	if s.Thinking {
		return
	}
	l.lastSpeakerMu.Lock()
	l.lastSpeaker = s.Who
	l.lastSpeakerMu.Unlock()
	if angle, ok := l.lookAngleFor(s.Who); ok {
		if err := l.commander.Send(fmt.Sprintf("look %d", angle)); err != nil {
			if l.verbose {
				log.Printf("failed to send look command: %v\n", err)
			}
		}
	}
	if l.verbose {
		log.Printf("Heard %s say: %s\n", s.Who, s.What)
	}
	l.enqueueHeard(s.Who + " says: " + s.What)
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

// PauseOutputFunc returns an outputFunc that publishes pause phrases to
// "speak/<name>" with Thinking set to true, signalling to other Actors that
// the message is filler and should not be added to their conversation context.
func (l *MQTTListener) PauseOutputFunc() func(string) {
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
		payload, err := json.Marshal(commands.Speak{Who: l.name, What: content, Thinking: thinking})
		if err != nil {
			log.Printf("failed to marshal response: %v\n", err)
			return
		}

		topic := "speak/" + l.name
		go func() {
			token := l.client.Publish(topic, 0, false, payload)
			token.Wait()
			if token.Error() != nil {
				log.Printf("failed to publish response to %s: %v\n", topic, token.Error())
			}
		}()
	}
}

// Close unblocks any pending MoreFunc call and disconnects from the broker.
// Safe to call multiple times.
func (l *MQTTListener) Close() {
	l.closeOnce.Do(func() {
		close(l.done)
		l.client.Disconnect(250)
	})
}

func removeEmoji(str string) string {
	// Regex pattern to match most emoji characters
	emojiPattern := "[\U0001F600-\U0001F64F\U0001F300-\U0001F5FF\U0001F680-\U0001F6FF\U0001F700-\U0001F77F\U0001F780-\U0001F7FF\U0001F800-\U0001F8FF\U0001F900-\U0001F9FF\U00002702-\U000027B0\U000024C2-\U0001F251]+"
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
