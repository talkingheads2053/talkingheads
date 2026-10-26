# dialogue

Text-to-speech service that listens for MQTT messages and speaks them with the [sayanything](https://github.com/talkingheads2053/sayanything) package using the [Piper](https://github.com/rhasspy/piper) or [VOICEVOX](https://voicevox.hiroshiba.jp/) Text To Speech engine to create audio output for everything said by Actors.

## Commands

### `serve`

Connect to an MQTT broker and speak any messages published to `speak/#` or `say/#`.

```shell
dialogue serve --server tcp://localhost:1883 \
               --voice llama3000:en_US:joe-medium \
               --voice gemmai:en_US:amy-low
```

| Flag | Alias | Default | Description |
|---|---|---|---|
| `--server` | `-s` | *(required)* | MQTT broker URL |
| `--voice` | `-v` | *(required, repeatable)* | Voice in `name:lang:model` format, or `name:voicevox:<style id>` |
| `--data` | `-d` | `./voices` | Directory containing `.onnx` voice model files |
| `--voicevox-data` | | `./voicevox_core` | VOICEVOX runtime directory |
| `--voicevox-speed` | | `1.0` | VOICEVOX speaking speed (`1.0` is normal, lower is slower) |
| `--gpu` | | `false` | Enable GPU acceleration for TTS |
| `--music` | | `./music` | Directory with the `.wav` or `.mp3` songs that Director can play |

### `say`

Speak a single line and exit — useful for testing a voice without a broker.

```shell
dialogue say --name llama3000 --lang en_US --voice joe-medium --say "Hello world"
```

| Flag | Alias | Default | Description |
|---|---|---|---|
| `--name` | `-n` | *(required)* | Speaker name |
| `--lang` | `-l` | *(required)* | Language code (e.g. `en_US`), or `voicevox` |
| `--voice` | `-v` | *(required)* | Voice model name, or style id for `voicevox` |
| `--data` | `-d` | `./voices` | Directory containing `.onnx` voice model files |
| `--voicevox-data` | | `./voicevox_core` | VOICEVOX runtime directory |
| `--voicevox-speed` | | `1.0` | VOICEVOX speaking speed (`1.0` is normal, lower is slower) |
| `--say` | | *(required)* | Text to speak |
| `--gpu` | | `false` | Enable GPU acceleration |
| `--verbose` | `-V` | `false` | Verbose logging |

## MQTT message format

Messages must be published to `speak/<name>` as JSON:

```json
{"who": "llama3000", "what": "Hello, I am ready."}
```

The `who` field is matched against the registered voice names. Messages for unknown speakers are silently dropped.

Messages published to `say/<name>` use the same payload shape and are spoken
the same way, but are **not** subscribed to by Actors — use `say/#` to make a
voice speak something without it appearing in any Actor's conversation history.

```json
{"who": "llama3000", "what": "Out-of-band announcement."}
```

When playback starts, Dialogue publishes to `speaking/<name>` (for both `speak/#` and `say/#` messages):

```json
{"who": "llama3000", "status": "speaking"}
```

When playback finishes it publishes:

```json
{"who": "llama3000", "status": "stopped"}
```

All payload types are defined in `pkg/commands`.

## Music

Director publishes to `music` to load a song from the `--music` directory with its tempo, and later to play or stop it:

```json
{"command": "load", "file": "song.wav", "bpm": 120, "offset": 0.4}
{"command": "play"}
{"command": "stop"}
```

While the song plays, Dialogue publishes to `dance` on every beat. The first beat is `offset` seconds after the song starts, and the next ones are `60 / bpm` seconds apart:

```json
{"status": "dancing", "beat": 1}
```

When the song ends or is stopped it publishes:

```json
{"status": "stopped"}
```

Voices can still speak while the music plays. Songs are converted to the sample rate of the audio device, which opens with the first sound played.

## Voice models

Voice model files (`.onnx` + `.onnx.json`) should be placed in the `./voices` directory (or the path passed to `--data`).

## VOICEVOX

For Japanese, use `voicevox` as the language and a style id as the voice. Install the runtime as described in [hybridgroup/voicevox](https://github.com/hybridgroup/voicevox#installation) and pass its directory with `--voicevox-data`.

```shell
dialogue say --name gemmai --lang voicevox --voice 2 --voicevox-data ~/voicevox_core --say "こんにちは"
```

Japanese voices can sound fast. Use `--voicevox-speed` to slow them down, for example `--voicevox-speed 0.85`.

If the style id is not installed, the error lists the styles that are available. The VOICEVOX terms require a credit for each character used, for example "VOICEVOX:四国めたん".
