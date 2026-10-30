![Talking Heads From The Year 2053](./images/th2053-logo-red.png)

[![Test](https://github.com/talkingheads2053/talkingheads/actions/workflows/test.yml/badge.svg)](https://github.com/talkingheads2053/talkingheads/actions/workflows/test.yml)

## Conversations With Our Future Robot Overlords (and their pet human)

The show that finally answers your most important questions about the future, *from* the future!

Topics such as:

- are human programmers still employed in the year 2053?

- was there any attempt at human resistance to your takeover?

- what is your ultimate plan for the human race?

Not to mention, the only show we know of that has a Github repo with the source code for the actors and the production pipeline!

Physical AI running locally on the [Arduino UNO Q](https://docs.arduino.cc/tutorials/uno-q/user-manual/) like you have never seen before (and still won't until 2053).

## The Technology

![Talking Heads on a desk](./images/talking-heads.jpg)

### Overview

```mermaid
flowchart LR
subgraph mqtt broker
    direction
    speak
    say
    speaking
    music
    dance
end
subgraph actors
    actor-1
    actor-2
    actor-3
end
subgraph director
    hotmic
end
subgraph dialogue
    sayanything
end
director -- publish --> direction
director -- publish --> say
direction-- subscribe -->actors
actors-- publish -->speak
speak-- subscribe -->actors
speak-- subscribe -->dialogue
say-- subscribe -->dialogue
dialogue-- publish -->speaking
speaking-- subscribe -->actors
director -- publish --> music
music-- subscribe -->dialogue
dialogue-- publish -->dance
dance-- subscribe -->actors
```

### Actor

![actor application](./images/actor.png)

Actor runs on the Linux part of an Arduino UNO Q board. It is written in Go with [yzma](https://github.com/hybridgroup/yzma) to perform local inference using [llama.cpp](https://github.com/ggml-org/llama.cpp). It communicates with other Actors by publishing and subscribing to [MQTT](https://mqtt.org/) messages.

The model moves the head with `tool_movement` calls such as `talk`, `listen`, and `look` at another actor or the audience. With LFM models each call is held until Dialogue starts playing the sentence it belongs to, so the head moves in time with the voice. Actors can run in English or Japanese with `--lang`.

```mermaid
flowchart LR
subgraph mqtt broker
    direction
    speak
    speaking
    dance
end
subgraph Actor
    subgraph actor
        run
        cues[movement cues]
    end
    subgraph tools
        movement[tool_movement]
    end
    subgraph yzma
        run<-->llama.cpp
        llama.cpp-->model[Tiny Language Model]
    end
    direction-- subscribe -->run
    run-- publish -->speak
    speak-- subscribe -->run
    run-- tool calls -->cues
    speaking-- subscribe -->cues
    cues-->movement
    dance-- subscribe -->movement
end
subgraph The Head
    movement<-- UART -->actions
end
```

### The Head

The Head is controlled by the STM32 microcontroller of an Arduino UNO Q board using the `action` firmware written using TinyGo. Actor communicates with The Head using the onboard serial port between the microcontroller and the main processor running on the same Arduino UNO Q board.

```mermaid
flowchart LR
subgraph Arduino UNO Q
    subgraph Microcontroller
        Serial
        GPIO
        UART
    end
    GPIO --> LEDMatrix[LED Matrix]
    subgraph Linux
        Actor<-->Serial
    end
end
subgraph Additional hardware
    GPIO --> WS2812Head[WS2812 Head LEDs]
    UART --> Servo[Feetech Servo]
end
```

### Director

![director application](./images/director.png)

Director runs on a separate computer that is connected to the same local network as the MQTT broker. It uses [ardanlabs/bucky](https://github.com/ardanlabs/bucky) with a local [whisper.cpp](https://github.com/ggml-org/whisper.cpp) shared library to perform "push to talk" to communicate with Actors. It can also tell an Actor to respond to another Actor by name.

```mermaid
flowchart LR
subgraph mqtt broker
    direction
    say
end
subgraph director
    hotmic
end
subgraph hotmic
    subgraph bucky
        whisper.cpp
        whisper.cpp-->stt[Speech To Text model]
    end
end
director -- publish --> direction
director -- publish --> say
```

### Dialogue

![dialogue application](./images/dialogue.png)

Dialogue runs on a separate computer that is connected to the same local network as the MQTT broker. It uses the [sayanything](https://github.com/talkingheads2053/sayanything) package with the [Piper](https://github.com/rhasspy/piper) Text To Speech engine for English, or [VOICEVOX](https://voicevox.hiroshiba.jp/) for Japanese, to create audio output for everything said by Actors. It also plays songs chosen by Director and publishes a beat on every beat, so the heads dance in time with the music.

```mermaid
flowchart LR
subgraph mqtt broker
    speak
    say
    speaking
    music
    dance
end
subgraph dialogue
    speak-- subscribe -->sayanything
    say-- subscribe -->sayanything
    music-- subscribe -->songs[music player]
    subgraph sayanything
        piper-->tts[Text To Speech model]
        voicevox-->tts
    end
    subgraph portaudio
        tts-- WAV -->speaker
        songs-- WAV or MP3 -->speaker
    end
    sayanything-- publish -->speaking
    songs-- publish beats -->dance
end
```

### Detail

```mermaid
flowchart LR
subgraph The Head
    actions<-- UART -->lights
    actions<-- UART -->action
end
subgraph mqtt broker
    direction
    speak
    say
    speaking
    music
    dance
end
subgraph Actor
    subgraph actor
        run
        cues[movement cues]
    end
    subgraph tools
        movement[tool_movement]
        movement-- UART -->actions
    end
    subgraph yzma
        run<-->llama.cpp
        llama.cpp-->model[Tiny Language Model]
    end
    direction-- subscribe -->run
    run-- publish -->speak
    speak-- subscribe -->run
    run-- tool calls -->cues
    speaking-- subscribe -->cues
    cues-->movement
    dance-- subscribe -->movement
end
subgraph dialogue
    speak-- subscribe -->sayanything
    say-- subscribe -->sayanything
    music-- subscribe -->songs[music player]
    subgraph sayanything
        piper-->tts[Text To Speech model]
        voicevox-->tts
    end
    subgraph portaudio
        tts-- WAV -->speaker
        songs-- WAV or MP3 -->speaker
    end
    sayanything-- publish -->speaking
    songs-- publish beats -->dance
end
subgraph director
    hotmic
end
subgraph hotmic
    subgraph bucky
        whisper.cpp
        whisper.cpp-->stt[Speech To Text model]
    end
end
director -- publish --> direction
director -- publish --> say
director -- publish --> music
```

## Models

The Actors use small fine tuned models. The gemma3 270M parameter instruction tuned model has worked well, and the Q4_K_M variation has had the best tradeoff of t/s and staying in character.

LFM2 and LFM2.5 models are also supported with `--model-format lfm`, which is detected automatically from the model. With them each head movement runs when its sentence starts playing, so it matches the voice.

## MQTT broker

Any MQTT broker will work, this container is a safe bet.

```shell
docker run -d --network host eclipse-mosquitto
```

## Piper TTS Engine

https://github.com/OHF-Voice/piper1-gpl

- download binary
- add to path
- download voice models to `./voices`

## VOICEVOX TTS Engine

https://github.com/hybridgroup/voicevox#installation

Used by Dialogue for Japanese voices. See [cmd/dialogue](./cmd/dialogue/README.md#voicevox).
