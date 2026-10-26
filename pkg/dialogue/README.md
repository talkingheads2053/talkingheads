# pkg/dialogue

Package `dialogue` uses the [sayanything](https://github.com/talkingheads2053/sayanything) package with the [Piper](https://github.com/rhasspy/piper) or [VOICEVOX](https://voicevox.hiroshiba.jp/) Text To Speech engine to create audio output for everything said by Actors.

## Key types

### `Voice`

Wraps a Piper or VOICEVOX TTS engine and audio player for a named speaker.

```go
v, err := dialogue.NewVoice(name, lang, voiceModel, dataDir, gpu, speed)
```

- `name` — speaker identifier matched against the `who` field in MQTT messages
- `lang` — language code (e.g. `en_US`), or `voicevox`
- `voiceModel` — voice model name (e.g. `en_US-joe-medium`), or a VOICEVOX style id
- `dataDir` — directory with the `.onnx` model files, or the VOICEVOX runtime directory
- `gpu` — enable GPU acceleration
- `speed` — VOICEVOX speaking speed (`1.0` is normal)

| Method | Description |
|---|---|
| `SayOnce(text)` | Speak synchronously, blocking until playback finishes |
| `SayAnything(text)` | Speak asynchronously in a goroutine |

### `Listener`

Subscribes to `speak/#` on an MQTT broker and routes each message to the matching `Voice`.

```go
listener, err := dialogue.NewListener(clientID, brokerURL, voices, verbose)
listener.Listen() // blocks
```

`Listen` blocks, consuming messages from the internal channel. For each incoming message it:

1. Publishes `speaking/<who>` with `status: "speaking"` to notify Actors that playback is starting.
2. Calls `SayOnce` (blocking) to synthesise and play the audio.
3. Publishes `speaking/<who>` with `status: "stopped"` when playback completes.

Messages use the `commands.Speak` type from `pkg/commands` (`{"who": "…", "what": "…"}`). Messages for unknown speakers are logged and dropped.

It also subscribes to `say/#` and treats incoming `commands.Say` messages identically to `Speak` for the purpose of voice playback and `speaking/<who>` notifications. The difference is that Actors do **not** subscribe to `say/#`, so utterances delivered via `say` are spoken aloud without being added to any Actor's conversation history.

## MQTT topics

| Topic | Direction | Description |
|---|---|---|
| `speak/#` | subscribe | Messages to be spoken; routed by `who` field |
| `say/#` | subscribe | Speak text via the named voice without affecting Actor conversation history |
| `speaking/<who>` | publish | Notifies Actors when speaking starts (`"speaking"`) and stops (`"stopped"`) |
