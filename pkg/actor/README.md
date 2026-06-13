# pkg/actor

Package `actor` manages the conversation loop with the language model and exposes tool calls that control the actor's physical head 
movement.

## Key types

### `Actor`

Created with `NewActor`. Runs a conversation loop against the configured model, dispatching any tool calls the model emits to the registered tools.

```go
a, err := actor.NewActor(mp, commander, moreFunc, outputFunc)
```

- `mp` — path to the model file(s)
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

Registered automatically by `NewActor`. Exposes the following commands to the model:

| Command | Angle required | Description |
|---|---|---|
| `look` | yes (0–180) | Turn head to angle immediately |
| `slowlook` | yes (0–180) | Turn head to angle slowly |
| `headshake` | no | Shake head (indicates "no") |
| `wait` | no | Idle movement while waiting to speak |
| `speak` | no | Small movement while speaking |
| `stop` | no | Stop and center (90°) |

Angle convention: `0` = full right, `90` = center, `180` = full left.

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

When `respond` is `true` the Actor builds the direction text automatically:

- If a previous speaker is known and `what` is empty → `"Now respond directly to <lastSpeaker>."`
- If a previous speaker is known and `what` is non-empty → `"<what> Respond directly to <lastSpeaker>."`
- If no speaker has been heard yet → `what` is used as-is.

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

- Drive the head-movement servo (`speak` / `stop` commands).
- **Gate thinking-phrase pacing** — the next thinking phrase is not emitted until the current one has finished playing. `MQTTListener` tracks in-flight phrase count via `speakN`; `WaitSpeakingDoneFunc()` returns a function that blocks until `speakN` reaches zero. When `SetSpeakingDoneFunc` is called with that function, `runThinkingPhrases` waits for each phrase to complete before emitting the next.

## Thinking phrase pacing

By default thinking phrases are emitted on a random timer (`ThinkingInterval` ± jitter). When connected over MQTT the actor also waits for the current phrase to finish being spoken before scheduling the next one:

```go
a.SetSpeakingDoneFunc(ml.WaitSpeakingDoneFunc())
```

This prevents the audio queue from filling with multiple overlapping thinking phrases when the model generates slowly.
