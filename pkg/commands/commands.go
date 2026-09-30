package commands

// Direction is the payload for the ask/# MQTT topic.
// It represents a question from the Director to an Actor.
// When Respond is true the Actor should respond directly to the last Actor
// that spoke, using What as an optional additional prompt; if What is empty
// the Actor generates a response based on the conversation context alone.
// RespondTo names the Actor to respond to instead of the last one that spoke.
type Direction struct {
	Who       string `json:"who"`
	What      string `json:"what"`
	Respond   bool   `json:"respond,omitempty"`
	RespondTo string `json:"respond_to,omitempty"`
}

// Speak is the payload for the speak/# MQTT topic.
// It represents a message spoken by an Actor.
// Thinking is true when the Actor is using a pause phrase while waiting for
// the model to produce its first token; other Actors should ignore such
// messages and not add them to their conversation context.
type Speak struct {
	Who      string `json:"who"`
	What     string `json:"what"`
	Thinking bool   `json:"thinking,omitempty"`
}

// Say is the payload for the say/# MQTT topic.
// It causes Dialogue to use the named voice to speak the given text without
// adding the utterance to any Actor's conversation history.
type Say struct {
	Who  string `json:"who"`
	What string `json:"what"`
}

const (
	StatusSpeaking = "speaking"
	StatusStopped  = "stopped"
)

// Speaking is the payload for the speaking/# MQTT topic.
// It represents a notification from Dialogue when speaking starts or stops.
// Status is either StatusSpeaking or StatusStopped.
type Speaking struct {
	Who    string `json:"who"`
	Status string `json:"status"`
}
