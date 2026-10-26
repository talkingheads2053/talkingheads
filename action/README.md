# Hardware

## Flashing the code

```shell
tinygo flash -target arduino-uno-q -size short .
```

## API

These are the commands that can be sent to the action system running on the microcontroller using the serial interface.

### `look [angle]`

Turns the head to look at a specific angle.

```
look 135
```

### `wait`

Causes the head to move a very small amount right or left one time per 5 seconds when waiting to speak.

### `headshake`

Causes the head to move back and forth 3 times to indicate a "No" response.

### `slowlook [angle]`

Turns the head slowly to look at a specific angle. Probably used as a comedic effect.

```
slowlook 135
```

### `doubletake [angle]`

Looks at a specific angle, glances back, then snaps to the angle again. Used as a comedic effect.

```
doubletake 135
```

### `speak`

Causes the head to move a very small amount right or left one time per second while speaking.

### `stop`

Causes the head to stop moving.

### `theme [colorname]`

Sets the personality color used by the head LED and matrix at runtime. Valid colors are `red`, `green`, `blue`, `purple`, `orange`, and `yellow`. The change takes effect on the next lighting tick.

```
theme green
```
