# director

Sends questions to actors over MQTT using a full-screen terminal UI built with [bubbletea](https://github.com/charmbracelet/bubbletea).

```
╔══════════════════════════════════════╗
║  DIRECTOR  (ASCII banner, pinned)    ║
╠══════════════════════════════════════╣
║  [scrollable output]                 ║
║  → gemmai: "what is the meaning…"    ║
║  ● recording… press F5 again to stop ║
╠══════════════════════════════════════╣
║  > phineas: tell me a joke_          ║
║  actors: [phineas gemmai]  F5: record║
╚══════════════════════════════════════╝
```

Typed commands and voice (hotmic) input work simultaneously in the same session. The banner is always visible; all output scrolls in the viewport above the input field.

## Flags

| Flag | Required | Default | Description |
|---|---|---|---|
| `--server` | yes | — | MQTT broker address (e.g. `tcp://localhost:1883`) |
| `--actor` | yes | — | Canonical actor name (repeatable; e.g. `--actor gemmai --actor phineas`) |
| `--hotmic-model` | no | — | Path to a whisper.cpp GGML model file. **Enables hotmic when set.** |
| `--hotmic-lang` | no | `auto` | BCP-47 language code for transcription (e.g. `en`), or `auto` |
| `--hotmic-key` | no | `f5` | bubbletea key name that toggles hotmic recording (e.g. `f5`, `f1`, `ctrl+r`) |
| `--hotmic-fuzzy-threshold` | no | `0.6` | Maximum Levenshtein distance ratio (0–1) for fuzzy actor name matching |
| `--hotmic-actor-alias` | no | — | Map alternate spoken names to a canonical actor: `--hotmic-actor-alias "gemmai:jami\|jamai\|jenna"` (repeatable) |

## MQTT message format

Directions are published to `direction/<name>` as JSON. Payload types are defined in `pkg/commands`.

Normal direction:
```json
{"who": "llama3000", "what": "What is the meaning of life?"}
```

Respond direction — instructs the Actor to reply to the last Actor it heard speak:
```json
{"who": "gemmai", "respond": true}
```

Respond direction with optional guidance:
```json
{"who": "gemmai", "what": "Keep it brief.", "respond": true}
```

## Typed input

Type a question into the input field at the bottom of the screen. Each line must be prefixed with an actor name followed by a colon or comma, then press **Enter**:

```
phineas: what is the meaning of life?
llama3000, tell me a joke
```

Prefix with `say` to speak text out-of-band without adding it to the actor's conversation history:

```
gemmai: say "welcome to the show"
```

Prefix with `respond` to instruct the Actor to reply directly to the last Actor it heard speak. Optional guidance text can follow:

```
gemmai respond
gemmai respond: keep it short
phineas respond keep it philosophical
```

## Hotmic input

When `--hotmic-model` is set, press the hotmic key (default **F5**) once to start recording and again to stop. The audio is transcribed locally using whisper.cpp and submitted automatically. The spoken format is the same as typed input: `"<actor>: <question>"`.

Because the hotmic key is a function key rather than a printable character, both typed and voice input work at the same time without conflict.

The spoken actor name is matched using three strategies in order:

1. **Exact match** after normalisation (lowercase, letters and digits only, hiragana turned into katakana).
2. **Substring containment** — handles e.g. "lama" → `llama3000`.
3. **Fuzzy edit-distance** — accepts the closest actor name or alias when the Levenshtein distance is within `--hotmic-fuzzy-threshold` of the longer name. This runs last, so `コヘンティン` still reaches qwentin through the alias `クエンティン`.

**Aliases** defined with `--hotmic-actor-alias` are checked before all three strategies. Use this to handle systematic whisper.cpp mis-transcriptions:

```sh
--hotmic-actor-alias "gemmai:jami|jamai|jenna|jedi"
```

### Japanese

Use `--hotmic-lang ja` with a multilingual whisper model such as `ggml-small.bin`, and add katakana aliases for each actor:

```sh
--hotmic-lang ja \
--hotmic-actor-alias "gemmai:ジェマイ|ジェマ" \
--hotmic-actor-alias "phineas:フィニアス" \
--hotmic-actor-alias "qwentin:クエンティン"
```

The name can be followed by `、` or by nothing at all, as in `ジェマイ自己紹介してください`. A trailing さん, くん, ちゃん, or 様 is dropped, in hiragana or katakana.

Press **Ctrl+C** to exit.

## Building

Hotmic mode requires the whisper.cpp shared library and PortAudio. Set these environment variables before building:

```sh
export WHISPER_DIR=$(git rev-parse --show-toplevel)/lib/whisper.cpp
export BUCKY_LIB=$WHISPER_DIR
```

Then from the `cmd/director` directory:

```sh
go build -o ../../build/director .
```

See [pkg/hotmic/README.md](../../pkg/hotmic/README.md) for full instructions on building whisper.cpp from source and downloading a model.

## Example

Keyboard-only mode:

```sh
./director --server tcp://localhost:1883 \
  --actor llama3000 --actor phineas --actor gemmai --actor qwentin
```

Hotmic mode with the base English model (F5 to record):

```sh
BUCKY_LIB=~/Development/bucky/lib \
./director \
  --server tcp://localhost:1883 \
  --actor phineas --actor gemmai --actor qwentin \
  --hotmic-model ~/models/ggml-base.bin \
  --hotmic-lang en \
  --hotmic-key f5 \
  --hotmic-fuzzy-threshold 0.4 \
  --hotmic-actor-alias "gemmai:jami|jamai|jenna|jedi|jamae|gemma|gem" \
  --hotmic-actor-alias "qwentin:quentin|quintin|quinton" \
  --hotmic-actor-alias "phineas:finneas|finius|finis|frinius"
```
