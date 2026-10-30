package main

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/talkingheads2053/talkingheads/pkg/commands"
)

// parseTypedInput parses a line of text typed by the user into a question.
// The expected format is "<actor>[: ,] <content>", optionally prefixed by
// "say" to produce a kindSay question. Returns an error if the actor cannot
// be identified.
func parseTypedInput(text string, actorList []string) (question, error) {
	if len(text) == 0 {
		return question{}, fmt.Errorf("empty input")
	}

	first := strings.Split(text, " ")[0]
	to := strings.TrimSuffix(first, ":")
	to = strings.TrimSuffix(to, ",")
	to = strings.ToLower(to)

	if to == "dance" && !slices.Contains(actorList, to) {
		return parseDance(strings.TrimPrefix(text, first))
	}

	if !slices.Contains(actorList, to) {
		return question{}, fmt.Errorf("unknown actor %q — expected one of %v", to, actorList)
	}

	query := strings.TrimSpace(strings.TrimPrefix(text, first))
	kind := kindDirection
	content := query
	var respondTo string
	if rest, ok := stripSayPrefix(content); ok {
		kind = kindSay
		content = trimSurroundingQuotes(rest)
	} else if rest, ok := stripRespondPrefix(content); ok {
		kind = kindRespond
		respondTo, content = stripRespondTarget(rest, actorList)
	}

	return question{To: to, Content: content, Kind: kind, RespondTo: respondTo}, nil
}

var errDanceUsage = errors.New("usage: dance <file> <bpm> [offset], dance moves [L|C|R ...], dance start, or dance stop")

// defaultDanceMoves is the pattern used when "dance moves" names none.
const defaultDanceMoves = "L R"

// parseDance parses the words after "dance" into a music question.
func parseDance(s string) (question, error) {
	fields := strings.Fields(s)
	if len(fields) > 0 && strings.EqualFold(fields[0], "moves") {
		return parseDanceMoves(fields[1:])
	}
	if len(fields) == 1 {
		switch strings.ToLower(fields[0]) {
		case "start":
			return question{Kind: kindDance, Music: commands.Music{Command: commands.MusicPlay}}, nil
		case "stop":
			return question{Kind: kindDance, Music: commands.Music{Command: commands.MusicStop}}, nil
		}
	}
	if len(fields) < 2 || len(fields) > 3 {
		return question{}, errDanceUsage
	}
	bpm, err := strconv.ParseFloat(fields[1], 64)
	if err != nil || bpm <= 0 {
		return question{}, fmt.Errorf("invalid bpm %q", fields[1])
	}
	var offset float64
	if len(fields) == 3 {
		if offset, err = strconv.ParseFloat(fields[2], 64); err != nil {
			return question{}, fmt.Errorf("invalid offset %q", fields[2])
		}
	}
	music := commands.Music{Command: commands.MusicLoad, File: fields[0], BPM: bpm, Offset: offset}
	return question{Kind: kindDance, Music: music}, nil
}

// parseDanceMoves turns moves such as "L R L L" into a music question.
func parseDanceMoves(fields []string) (question, error) {
	moves := make([]string, 0, len(fields))
	for _, f := range fields {
		move := strings.ToUpper(f)
		if move != commands.MoveLeft && move != commands.MoveCenter && move != commands.MoveRight {
			return question{}, fmt.Errorf("invalid dance move %q, expected L, C or R", f)
		}
		moves = append(moves, move)
	}
	pattern := defaultDanceMoves
	if len(moves) > 0 {
		pattern = strings.Join(moves, " ")
	}
	return question{Kind: kindDance, Music: commands.Music{Command: commands.MusicMoves, Moves: pattern}}, nil
}

// stripRespondTarget splits an actor name, optionally after "to", off the
// front of s, as in "qwentin keep it short" or "to qwentin".
func stripRespondTarget(s string, actorList []string) (string, string) {
	fields := strings.Fields(s)
	if len(fields) > 1 && strings.EqualFold(fields[0], "to") {
		fields = fields[1:]
	}
	if len(fields) == 0 {
		return "", strings.TrimSpace(s)
	}
	name := strings.ToLower(strings.TrimRight(fields[0], ":,"))
	if !slices.Contains(actorList, name) {
		return "", strings.TrimSpace(s)
	}
	return name, strings.Join(fields[1:], " ")
}

// stripSayPrefix returns the remainder of s with the leading "say" word
// removed (case-insensitive) and reports whether the prefix was present.
// Trailing punctuation directly after "say" (e.g. "say:" or "say,") is
// tolerated.
func stripSayPrefix(s string) (string, bool) {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return s, false
	}
	space := strings.IndexAny(trimmed, " \t")
	var word, rest string
	if space < 0 {
		word = trimmed
		rest = ""
	} else {
		word = trimmed[:space]
		rest = trimmed[space+1:]
	}
	word = strings.TrimRight(word, ":,")
	if !strings.EqualFold(word, "say") {
		return s, false
	}
	return strings.TrimSpace(rest), true
}

// stripRespondPrefix returns the remainder of s with the leading "respond"
// word removed (case-insensitive) and reports whether the prefix was present.
// Trailing punctuation directly after "respond" (e.g. "respond:" or
// "respond,") is tolerated. The remainder is an optional extra prompt; an
// empty remainder is valid and means the Actor responds without additional guidance.
func stripRespondPrefix(s string) (string, bool) {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return s, false
	}
	space := strings.IndexAny(trimmed, " \t")
	var word, rest string
	if space < 0 {
		word = trimmed
		rest = ""
	} else {
		word = trimmed[:space]
		rest = trimmed[space+1:]
	}
	word = strings.TrimRight(word, ":,")
	if !strings.EqualFold(word, "respond") {
		return s, false
	}
	return strings.TrimSpace(rest), true
}
