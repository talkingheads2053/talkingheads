# pkg/actor

Package `actor` manages the conversation loop with the language model and exposes tool calls that control the actor's physical head movement.

## Key types

### `Actor`

Created with `NewActor`. Runs a conversation loop against the configured model, dispatching any tool calls the model emits to the registered tools.

```go
a, err := actor.NewActor(mp, cfg, commander, moreFunc, outputFunc)
```

- `mp` — path to the model file(s)
- `cfg` — `Config` with sampling, model format, stage, and language settings (start from `DefaultConfig()`)
- `commander` — where action commands are sent (`nil` logs to console)
- `moreFunc` — called each turn to append new user input to the conversation
- `outputFunc` — called with each text response from the model

### `Commander`

Interface used to send action commands to the microcontroller.

| Implementation | Description |
|---|---|
| `LogCommander` (default) | Logs commands via `log.Printf` |
| `SerialCommander` | Sends commands over a serial port |

```go
// Serial
sc, err := actor.NewSerialCommander("/dev/ttyACM0", 9600)

// Console (nil also works)
var commander actor.Commander // nil → LogCommander
```

### `Movement` tool

Registered automatically by `NewActor`. The model makes every head movement with a `tool_movement` call. It picks a command and a target, which is an actor name from `--actor-positions` or `audience`. The firmware makes the small motions within a mode.

| Command | Kind | Serial |
|---|---|---|
| `talk` | mode | `look <angle>` when a target is given, then `speak` |
| `listen` | mode | `look <angle>`, then the firmware waits facing the speaker |
| `look` | gaze | `look <angle>` |
| `slowlook` | special | `slowlook <angle>` |
| `doubletake` | special | `doubletake <angle>` |
| `facecrowd` | special | `slowlook 90` |
| `headshake` | special | `headshake` |

Special moves run at most once per show, with at least `--interjection-gap` between any two. A refused one turns into a plain `look`, or nothing for `headshake`.

The current mode is sent again every 2 seconds, because the firmware stops after 5 seconds without a command. When nothing is spoken for a second the head gets `stop` and rests. If a phrase then starts, the head picks up `talk` or `listen` again.

Angle convention: `0` = full right, `90` = center, `180` = full left.

### Tool calls with LFM models

With `--model-format lfm` the reply streams with its tool calls cut out. Each call runs when the sentence after it starts playing, so the movement matches the voice. Calls with no sentence after them run at once.

Each time the actor hears another actor, it runs a short reaction that may only write tool calls, such as `listen` facing the speaker. The calls run when the heard line starts playing, and are dropped if the speaker already stopped. A direction arriving stops the reaction. `--react=false` turns this off.

Thinking phrases keep going until the first sentence of the reply is ready, since the reply starts with tool calls. When another actor starts a thinking phrase, this actor turns to `listen` to them.

LFM is detected from the model architecture when `--model-format` is `auto`.

```mermaid
sequenceDiagram
participant model as Model
participant actor as Actor
participant mqtt as MQTT broker
participant dialogue as Dialogue
participant head as The Head
model->>actor: tool call, then sentence
actor->>actor: cue the tool call for the sentence
actor->>mqtt: speak/gemmai
mqtt->>dialogue: speak/gemmai
dialogue->>mqtt: speaking/gemmai speaking
mqtt->>actor: speaking/gemmai speaking
actor->>head: look and speak over serial
dialogue->>mqtt: speaking/gemmai stopped
mqtt->>actor: speaking/gemmai stopped
actor->>head: stop after one second of quiet
```

## Serial protocol

Commands are sent as plain ASCII terminated with `\r`:

```
look 135\r
headshake\r
stop\r
```

This matches the serial API implemented by the `action` firmware in `action/`.

## MQTT topics

All payloads use the JSON types from `pkg/commands`.

| Topic | Direction | Description |
|---|---|---|
| `direction/<name>` | subscribe | Direct question from the Director — triggers a response |
| `speak/#` | subscribe | Hear what other Actors say — added as context only, never triggers a response |
| `speaking/<name>` | subscribe | Notifications from Dialogue when this Actor's voice starts or stops playing |
| `speak/<name>` | publish | Publish the Actor's response |

Actors do **not** subscribe to `say/#`. Messages published there are spoken by
Dialogue but are deliberately invisible to every Actor's conversation history.

### Directing an Actor to speak

The Director publishes a `Direction` payload to `direction/<name>` to prompt the Actor for a response.

| Field | Type | Description |
|---|---|---|
| `who` | string | Name of the Actor being directed |
| `what` | string | The prompt or guidance text (may be empty) |
| `respond` | bool | `true` to instruct the Actor to respond to the last Actor it heard speak; omitted (false) for a normal direction |
| `respond_to` | string | Actor to respond to instead of the last speaker; only used with `respond` |

When `respond` is `true` the Actor builds the direction text automatically. The target is `respond_to`, or the last speaker when it is empty:

- If a target is known and `what` is empty → `"Now respond directly to <target>."`
- If a target is known and `what` is non-empty → `"<what> Respond directly to <target>."`
- If the target has spoken, their last line is quoted first → `<target> said: "<line>" `
- If no target is known → `what` is used as-is.

With `--lang ja` the same text is sent in Japanese.

The Actor tracks the last non-thinking speaker internally, updated each time a `speak/#` message with `thinking: false` arrives.

### Heard speech from other Actors

When another Actor publishes a sentence to `speak/<other>`, this Actor receives it via the `speak/#` subscription and adds it to the conversation as context. The heard speech is buffered and injected into the conversation immediately before the next Direction is processed — the Actor never responds to heard speech on its own.

Thinking phrases (filler sentences spoken while the model is generating, such as `"let me think..."`) are published with `"thinking": true` in the JSON payload. Receiving Actors check this field and discard those messages — they are never added to the conversation context. Thinking messages also do **not** update the last-speaker record.

### `speak` payload fields

| Field | Type | Description |
|---|---|---|
| `who` | string | Name of the Actor that spoke |
| `what` | string | The spoken text |
| `thinking` | bool | `true` when the message is a thinking phrase; omitted (false) for real speech |

### Speaking status

When Dialogue begins playing audio for this Actor it publishes a `speaking/<name>` message with `status: "speaking"`. When playback finishes it publishes `status: "stopped"`. The Actor uses these notifications to:

- Run the movement tool calls cued for each phrase when it starts playing, and rest the head when nothing is playing.
- **Gate thinking-phrase pacing** — the next thinking phrase is not emitted until the current one has finished playing. `MQTTListener` tracks in-flight phrase count via `speakN`; `WaitSpeakingDoneFunc()` returns a function that blocks until `speakN` reaches zero. When `SetSpeakingDoneFunc` is called with that function, `runThinkingPhrases` waits for each phrase to complete before emitting the next.

## Thinking phrase pacing

By default thinking phrases are emitted on a random timer (`ThinkingInterval` ± jitter). When connected over MQTT the actor also waits for the current phrase to finish being spoken before scheduling the next one:

```go
a.SetSpeakingDoneFunc(ml.WaitSpeakingDoneFunc())
```

This prevents the audio queue from filling with multiple overlapping thinking phrases when the model generates slowly.
