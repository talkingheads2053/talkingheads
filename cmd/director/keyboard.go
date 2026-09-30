package main

import (
	"fmt"
	"slices"
	"strings"
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
