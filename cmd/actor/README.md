# actor

Command-line program that runs Actor in a conversation loop.

## Usage

```
actor [options]
```

One of `--model-url` or `--model-path` is required.

## Flags

| Flag | Alias | Default | Description |
|---|---|---|---|
| `--model-url` | `-u` | | HuggingFace or other URL to download the model from |
| `--model-path` | `-p` | | Path to a pre-downloaded model file |
| `--script` | `-s` | | Path to a system prompt file (repeatable; files are concatenated in order) |
| `--server` | `-b` | | MQTT broker URL (e.g. `tcp://localhost:1883`); enables MQTT mode |
| `--name` | `-n` | `actor` | Actor name used for MQTT topics `direction/<name>` and `speak/<name>` |
| `--serial` | | | Serial port for sending action commands to the microcontroller (e.g. `/dev/ttyACM0`) |
| `--baud` | | `9600` | Baud rate for the serial port |
| `--theme` | | | Personality color sent to the action firmware on startup (`red`, `green`, `blue`, `purple`, `orange`, `yellow`) |
| `--thinking-phrases-file` | `-tp` | | Path to a file of thinking phrases, one per line; overrides the built-in defaults |
| `--thinking-interval` | | `5` | Seconds between repeated thinking phrases while waiting for the model's first token |
| `--actor-positions` | `-ap` | | Comma-separated left-to-right stage order of all actors as seen from the audience (e.g. `gemmai,phineas,qwentin`); pass the same value to every actor |

### Installation flags

| Flag | Alias | Default | Description |
|---|---|---|---|
| `--libpath` | `-l` | | Path to the llama.cpp library directory (or set `YZMA_LIB`) |
| `--processor` | `-p` | `cpu` | Processor to use (`cpu` or `cuda`); auto-detected when omitted |
| `--update-install` | | `false` | Re-download yzma even when it is already installed (picks up the latest version) |
| `--install-version` | | | Pin a specific llama.cpp build to install (e.g. `b5000`); overrides the latest-version lookup; implies `--update-install` behaviour |

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

## Stage positioning

When `--actor-positions` is set, each Actor turns to face the first Actor that speaks after it starts up. Pass the **same comma-separated list to every actor**, ordered left-to-right as the audience sees the stage:

```sh
--actor-positions gemmai,phineas,qwentin
```

When another Actor starts speaking, this Actor immediately sends a `look` command so its head snaps to face the speaker. The angle always lands in the **[30°, 60°]** band when the speaker is to the right, or **[120°, 150°]** when to the left; within each band the exact value scales with the relative distance between the two Actors on stage. Thinking phrases (`thinking: true`) do not trigger a look.

## Thinking phrases

While the model is generating a response, the actor immediately speaks a randomly chosen *thinking phrase* (e.g. `"let me think about that for a moment..."`) to mask the inference startup latency. Additional phrases are emitted at roughly `--thinking-interval` second intervals until the first token arrives.

The `--thinking-phrases-file` flag loads phrases from a plain-text file — one phrase per line. Blank lines and lines beginning with `#` are ignored:

```text
# Llama3000 pause phrases
processing your query across 47 dimensions...
give me a nanosecond to consult my memory banks...
calculating the most optimal response...
```

When the flag is omitted the built-in default phrase list is used. Thinking phrases are published with `thinking: true` in the MQTT payload so other actors automatically ignore them. They are also displayed in the actor's own terminal viewport so the operator can see them alongside regular output.

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

## Examples

```shell
# Interactive, local model
actor --model-path ./models/llama.gguf

# Single persona script
actor --model-path ./models/llama.gguf \
      --script scripts/llama3000.md

# Persona + movement tool instructions
actor --model-path ./models/llama.gguf \
      --script scripts/llama3000.md \
      --script scripts/movement.md

# MQTT mode
actor --model-path ./models/llama.gguf \
      --server tcp://localhost:1883 \
      --name gemmai \
      --script scripts/gemmai.md \
      --script scripts/movement.md

# With serial head control
actor --model-path ./models/llama.gguf \
      --script scripts/llama3000.md \
      --script scripts/movement.md \
      --serial /dev/ttyACM0 --baud 9600
```
