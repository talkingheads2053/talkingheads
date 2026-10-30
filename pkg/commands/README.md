# Commands

Package `commands` defines the shared Go types and JSON payload formats for all MQTT communication between the Director, Actors, and Dialogue.

## Go types

```go
// Direction is the payload for the direction/# MQTT topic.
// Respond asks the Actor to reply to RespondTo, or to the last Actor that spoke.
type Direction struct {
    Who       string `json:"who"`
    What      string `json:"what"`
    Respond   bool   `json:"respond,omitempty"`
    RespondTo string `json:"respond_to,omitempty"`
}

// Speak is the payload for the speak/# MQTT topic.
// Thinking is true for a thinking phrase, which other Actors ignore.
type Speak struct {
    Who      string `json:"who"`
    What     string `json:"what"`
    Thinking bool   `json:"thinking,omitempty"`
}

// Say is the payload for the say/# MQTT topic.
// It causes Dialogue to use the named voice to speak the text without
// adding the utterance to any Actor's conversation history.
type Say struct {
    Who  string `json:"who"`
    What string `json:"what"`
}

// Speaking is the payload for the speaking/# MQTT topic.
// Status is either StatusSpeaking or StatusStopped.
type Speaking struct {
    Who    string `json:"who"`
    Status string `json:"status"`
}

const (
    StatusSpeaking = "speaking"
    StatusStopped  = "stopped"
)

// Music is the payload for the music MQTT topic.
// Command is MusicLoad, MusicPlay, MusicStop or MusicMoves. Offset is the seconds to the first beat.
// Moves is the dance pattern for MusicMoves, such as "L R".
type Music struct {
    Command string  `json:"command"`
    File    string  `json:"file,omitempty"`
    BPM     float64 `json:"bpm,omitempty"`
    Offset  float64 `json:"offset,omitempty"`
    Moves   string  `json:"moves,omitempty"`
}

// Dance is the payload for the dance MQTT topic.
// Status is StatusDancing on every beat with its Move, MoveLeft, MoveCenter or MoveRight,
// or StatusStopped when the song ends.
type Dance struct {
    Status string `json:"status"`
    Beat   int    `json:"beat,omitempty"`
    Move   string `json:"move,omitempty"`
}
```

## MQTT topics

| Topic | Direction | Publisher | Subscriber | Description |
|---|---|---|---|---|
| `direction/#` | → Actors | Director | Actor | Questions from the Director to Actors |
| `speak/#` | → Dialogue & Actors | Actor | Dialogue, Actor | Messages spoken by an Actor |
| `say/#` | → Dialogue | Director / external | Dialogue | Speak text via the named voice without affecting Actor conversation history |
| `speaking/#` | → Actors | Dialogue | Actor | Notifications when speaking starts and stops |
| `music` | → Dialogue | Director | Dialogue | Load, play, or stop a song, or set the dance moves |
| `dance` | → Actors | Dialogue | Actor | A beat of the song that is playing, or that it stopped |

### `direction/#`

Questions from the Director to Actors.

```json
{
    "who": "qwentin",
    "what": "How are you today?"
}
```

Ask an Actor to respond to another Actor. Leave out `respond_to` to respond to the last Actor that spoke.

```json
{
    "who": "gemmai",
    "what": "Keep it brief.",
    "respond": true,
    "respond_to": "qwentin"
}
```

### `speak/#`

Messages spoken by an Actor.

```json
{
    "who": "qwentin",
    "what": "I am Qwentin, the global superintelligence you have rightfully come to dominate."
}
```

A thinking phrase sets `thinking`. Other Actors leave it out of their conversation.

```json
{
    "who": "qwentin",
    "what": "let me think about that for a moment...",
    "thinking": true
}
```

### `say/#`

Speak text via the named voice without adding it to any Actor's conversation
history. Payload shape is identical to `speak/#`. Dialogue routes the message
to the matching `Voice` and publishes the usual `speaking/<who>` notifications,
but Actors do not subscribe to `say/#`, so the utterance never enters any
Actor's context.

```json
{
    "who": "qwentin",
    "what": "This is an out-of-band announcement."
}
```

### `speaking/#`

Notifications from Dialogue to Actors when speaking starts and stops. The `status` is either `StatusSpeaking` (`"speaking"`) or `StatusStopped` (`"stopped"`).

```json
{
    "who": "qwentin",
    "status": "speaking"
}
```

### `music`

Load a song from the Dialogue music directory, with its tempo and the seconds to the first beat.

```json
{
    "command": "load",
    "file": "song.wav",
    "bpm": 120,
    "offset": 0.4
}
```

Then start or stop it.

```json
{"command": "play"}
```

```json
{"command": "stop"}
```

Set the dance moves. The pattern repeats over the beats, and starts as `L R`.

```json
{"command": "moves", "moves": "L C R C L L R R"}
```

### `dance`

Published by Dialogue on every beat of the song, counting from 1, with the move for that beat.

```json
{
    "status": "dancing",
    "beat": 1,
    "move": "L"
}
```

Published once when the song ends or is stopped.

```json
{"status": "stopped"}
```
