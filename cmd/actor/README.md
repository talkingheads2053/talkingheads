# actor

Command-line program that runs Actor in a conversation loop.

## Usage

```
actor [options]
```

`--model-url` is required. The model is downloaded once to the default models directory.

## Flags

| Flag | Alias | Default | Description |
|---|---|---|---|
| `--model-url` | `-u` | | HuggingFace or other URL to download the model from |
| `--script` | `-s` | | Path to a system prompt file (repeatable; files are concatenated in order) |
| `--server` | `-b` | | MQTT broker URL (e.g. `tcp://localhost:1883`); enables MQTT mode |
| `--name` | `-n` | `actor` | Actor name used for MQTT topics `direction/<name>` and `speak/<name>` |
| `--serial` | | | Serial port for sending action commands to the microcontroller (e.g. `/dev/ttyACM0`) |
| `--baud` | | `9600` | Baud rate for the serial port |
| `--theme` | | | Personality color sent to the action firmware on startup (`red`, `green`, `blue`, `purple`, `orange`, `yellow`) |
| `--lang` | | `en` | Language for the prompts the actor sends to the model and its default thinking phrases (`en`, `ja`) |
| `--banner-name` | | | Katakana name for the startup banner when `--lang` is `ja`. Defaults to ジェマイ, フィニアス, or クエンティン for the cast, otherwise the English name is used |
| `--thinking-phrases-file` | `-tp` | | Path to a file of thinking phrases, one per line; overrides the built-in defaults |
| `--thinking-interval` | | `5` | Seconds between repeated thinking phrases while waiting for the model's first token |
| `--actor-positions` | `-ap` | | Comma-separated left-to-right stage order of all actors as seen from the audience (e.g. `gemmai,phineas,qwentin`); pass the same value to every actor |
| `--interjection-gap` | | `3m` | Shortest time between two special movements such as `doubletake` or `headshake` |
| `--react` | | `true` | Move the head in reaction to lines heard from other actors (LFM models only) |
| `--verbose` | | `false` | Verbose logging for debugging |

### Installation flags

| Flag | Alias | Default | Description |
|---|---|---|---|
| `--libpath` | `-l` | | Path to the llama.cpp library directory (or set `YZMA_LIB`) |
| `--processor` | `-p` | `cpu` | Processor to use (`cpu` or `cuda`); auto-detected when omitted |
| `--update-install` | | `false` | Re-download yzma even when it is already installed (picks up the latest version) |
| `--install-version` | | | Pin a specific llama.cpp build to install (e.g. `b5000`); overrides the latest-version lookup; implies `--update-install` behaviour |

### Model flags

| Flag | Default | Description |
|---|---|---|
| `--model-format` | `auto` | Tool call format (`auto`, `standard`, `qwen`, `glm`, `mistral`, `gemma3`, `gemma`, `gpt`, `phi`, `lfm`). `auto` detects LFM from the model architecture and the others from the model file name |
| `--inject-tools` | `true` | Add the tool definitions to the system prompt |
| `--enable-thinking` | `false` | Turn on reasoning mode for models that support it, such as Qwen3 |
| `--max-tokens` | `2048` | Most tokens generated per turn |
| `--context-size` | `4096` | Context window size in tokens |
| `--batch-size` | `0` | Logical batch size `n_batch` (`0` = llama.cpp default) |
| `--ubatch-size` | `0` | Physical micro batch size `n_ubatch` (`0` = llama.cpp default) |

### Sampling flags

| Flag | Default | Description |
|---|---|---|
| `--temperature` | `0.6` | Sampling temperature |
| `--top-p` | `0.95` | Top-P (nucleus) sampling threshold |
| `--min-p` | `0.05` | Min-P sampling threshold (minimum probability relative to the most likely token; `0.0` disables) |
| `--top-k` | `20` | Top-K sampling limit |
| `--repeat-penalty` | `1.0` | Penalise recently-seen tokens (`1.0` disables) |
| `--freq-penalty` | `0.0` | Penalise tokens by frequency (`0.0` disables) |
| `--presence-penalty` | `0.0` | Penalise tokens by presence (`0.0` disables) |
| `--dry-multiplier` | `0.0` | DRY repetition penalty multiplier (`0.0` disables) |
| `--max-sentences` | `0` | Cap the number of sentences spoken per turn (`0` = unlimited) |
| `--repeat-guard` | `0` | Write a sentence again when it shares this many letters in a row with a recent line from any actor, including its own earlier lines (`0` = off). The copy is removed from the model context first, and after 3 tries the reply ends there |

## Head movement

The model makes every head movement with a `tool_movement` call. It picks a command such as `talk`, `listen`, or `look`, and a target, which is `audience` or an actor name from `--actor-positions`. See [pkg/actor](../../pkg/actor/README.md#movement-tool) for the full list of commands.

Pass the **same comma-separated list to every actor**, ordered left-to-right as the audience sees the stage:

```sh
--actor-positions gemmai,phineas,qwentin
```

Facing another actor always lands in the **[30°, 60°]** band when they are to the right, or **[120°, 150°]** when to the left. Within each band the exact value scales with the distance between the two actors on stage. The audience is straight ahead at 90°.

Special moves (`slowlook`, `doubletake`, `facecrowd`, `headshake`) run at most once per show, with at least `--interjection-gap` between any two.

With an LFM model (`--model-format lfm`, or detected automatically) each movement runs when the sentence after it starts playing, so it matches the voice. When another actor speaks, this actor runs a short reaction that turns to listen to them. `--react=false` turns the reaction off.

When another actor starts a thinking phrase, this actor turns to listen to them. When nothing is spoken for a second the head rests.

## Thinking phrases

As soon as a direction arrives, the actor speaks a randomly chosen *thinking phrase* (e.g. `"let me think about that for a moment..."`) to mask the inference startup latency. Additional phrases are emitted at roughly `--thinking-interval` second intervals until the first sentence of the reply is ready. Each phrase waits for the one before it to finish playing.

The `--thinking-phrases-file` flag loads phrases from a plain-text file — one phrase per line. Blank lines and lines beginning with `#` are ignored:

```text
# Llama3000 pause phrases
processing your query across 47 dimensions...
give me a nanosecond to consult my memory banks...
calculating the most optimal response...
```

When the flag is omitted the built-in default phrase list for `--lang` is used. Thinking phrases are published with `thinking: true` in the MQTT payload so other actors automatically ignore them. They are also displayed in the actor's own terminal viewport so the operator can see them alongside regular output.

## System prompts

The `--script` flag accepts a path to a Markdown file and can be repeated to
compose a prompt from multiple files. Files are concatenated in order, separated
by a blank line. If no `--script` flag is given, the actor defaults to
`"You are a helpful assistant."`.

Pre-built scripts live in the `scripts/` directory at the repo root:

| Script | Description |
|---|---|
| `scripts/llama3000.md` | Llama3000 persona for panel discussion roleplay |
| `scripts/gemmai.md` | Gemmai persona |
| `scripts/phineas.md` | Phineas persona |
| `scripts/qwentin.md` | Qwentin persona |
| `scripts/movement.md` | Head movement tool instructions (append to any persona) |

## Modes

**Interactive (default)** — reads input from stdin, prints responses to stdout.

**MQTT** — set `--server` to connect to an MQTT broker. The actor subscribes to
`direction/<name>` for direct prompts, `speak/#` to hear other actors, and
`speaking/<name>` to receive playback notifications from Dialogue. It publishes
its responses to `speak/<name>`. Payloads use the JSON formats defined in
`pkg/commands`.

**Serial** — set `--serial` to send head movement commands to a microcontroller
running the action firmware. If omitted, commands are logged to the console instead.

## Japanese

Set `--lang ja` to send Japanese prompts to the model and use Japanese thinking phrases. The startup banner shows the katakana name from `--banner-name`. Use `--repeat-guard` to rewrite sentences that repeat earlier lines. In this mode a sentence with an English word outside a short allow list is also written again.

## Examples

```shell
# Interactive, local model
actor --model-url https://huggingface.co/<user>/<repo>/resolve/main/llama.gguf

# Single persona script
actor --model-url https://huggingface.co/<user>/<repo>/resolve/main/llama.gguf \
      --script scripts/llama3000.md

# Persona + movement tool instructions
actor --model-url https://huggingface.co/<user>/<repo>/resolve/main/llama.gguf \
      --script scripts/llama3000.md \
      --script scripts/movement.md

# MQTT mode with stage positions
actor --model-url https://huggingface.co/<user>/<repo>/resolve/main/llama.gguf \
      --server tcp://localhost:1883 \
      --name gemmai \
      --script scripts/gemmai.md \
      --script scripts/movement.md \
      --actor-positions gemmai,phineas,qwentin

# With serial head control
actor --model-url https://huggingface.co/<user>/<repo>/resolve/main/llama.gguf \
      --script scripts/llama3000.md \
      --script scripts/movement.md \
      --serial /dev/ttyACM0 --baud 9600
```
