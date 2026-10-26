# action

TinyGo firmware for the STM32 microcontroller on the Arduino UNO Q. It moves The Head servo and drives the head LEDs and LED matrix, using commands sent over serial by Actor.

## Hardware

| Part | Connection |
|---|---|
| Feetech serial servo | `UART1` (build tag `feetech`) |
| Standard PWM servo | `D3` (default build) |
| WS2812 head LEDs | `D2` |
| LED matrix | onboard |

Build with the `noservo` tag to run without a servo.

## Flashing the code

```shell
tinygo flash -target arduino-uno-q -tags feetech -size short .
```

The default personality color can be set at build time:

```shell
tinygo flash -target arduino-uno-q -tags feetech -ldflags="-X main.PersonalityColor=blue" .
```

The `build-action` and `flash-action` targets in the top level `Makefile` do the same.

## API

These are the commands that can be sent to the action system running on the microcontroller using the serial interface. Each command ends with `\r`. An invalid command gets a reply like `error: unknown command`.

The head stops and returns to center if no command arrives for 5 seconds, so Actor sends the current mode again every 2 seconds.

Angles are in degrees. `0` is full right, `90` is center, and `180` is full left.

### `look [angle]`

Turns the head to an angle right away, then waits there.

```
look 135
```

### `slowlook [angle]`

Turns the head slowly to an angle, 15 degrees at a time. Used as a comedic effect.

```
slowlook 135
```

### `doubletake [angle]`

Looks at an angle, glances back, then snaps to the angle again and waits there. Used as a comedic effect.

```
doubletake 135
```

### `wait`

Moves the head a small amount right or left about once a second while listening. The head LEDs show the personality color.

### `speak`

Moves the head right or left about twice a second while speaking. The head LEDs alternate colors and the matrix shows a VU meter.

### `headshake`

Moves the head back and forth 3 times to indicate a "No" response, then stops at center. The head LEDs turn red.

### `dance [angle]`

Turns the head to an angle right away and holds still there until the next `dance` command. Actor sends one on every beat of a song. The head LEDs alternate colors and the matrix shows a VU meter.

```
dance 60
```

### `stop`

Stops moving and returns the head to center.

### `theme [colorname]`

Sets the personality color used by the head LED and matrix at runtime. Valid colors are `red`, `green`, `blue`, `purple`, `orange`, and `yellow`. The change takes effect on the next lighting tick.

```
theme green
```

## Actor movements

Actor turns each `tool_movement` call from the model into these commands. See [pkg/actor](../pkg/actor/README.md#movement-tool).

| Movement | Serial commands |
|---|---|
| `talk` | `look <angle>` when a target is given, then `speak` |
| `listen` | `look <angle>`, then `wait` |
| `look` | `look <angle>` |
| `slowlook` | `slowlook <angle>` |
| `doubletake` | `doubletake <angle>` |
| `facecrowd` | `slowlook 90` |
| `headshake` | `headshake` |

While the heads dance, Actor skips movement calls from the model and sends `dance 60` and `dance 120` on alternate beats instead.
