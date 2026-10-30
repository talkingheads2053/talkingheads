#!/bin/bash

# Finds the tempo and the first beat of an mp3 or wav file.
# Needs ffmpeg and Go.

if [ $# -ne 1 ]; then
    echo "usage: $0 <song.mp3|song.wav>"
    exit 1
fi

if [ ! -f "$1" ]; then
    echo "file not found: $1"
    exit 1
fi

song="$(realpath "$1")"
cd "$(dirname "$0")/.." && exec go run ./tools/findbpm "$song"
