package main

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// transcriptSeparators end the actor name in a transcript, including Japanese punctuation.
const transcriptSeparators = ":,?. \t、，。？：　"

// honorifics may follow a Japanese actor name, as in ジェマイさん.
var honorifics = []string{"さん", "くん", "ちゃん", "様"}

// parseTranscript splits a hot mic transcript into the actor it addresses and
// the rest. Japanese transcripts often have no separator after the name.
func parseTranscript(text string) (string, string, error) {
	text = strings.TrimSpace(text)
	if idx := strings.IndexAny(text, transcriptSeparators); idx >= 0 {
		_, size := utf8.DecodeRuneInString(text[idx:])
		if to, ok := matchActor(strings.ToLower(strings.TrimSpace(text[:idx]))); ok {
			return to, strings.TrimSpace(text[idx+size:]), nil
		}
	}
	if to, rest, ok := matchActorPrefix(text); ok {
		return to, rest, nil
	}
	return "", "", fmt.Errorf("unknown actor in %q", text)
}

// matchActorPrefix finds the longest actor name or alias that starts text and
// returns the rest without a leading honorific or separator.
func matchActorPrefix(text string) (string, string, bool) {
	names := make(map[string]string)
	for _, p := range actors {
		names[normalise(p)] = p
	}
	for actor, aliases := range actorAliases {
		for _, alias := range aliases {
			names[normalise(alias)] = actor
		}
	}

	var b strings.Builder
	found, end := "", 0
	for i, r := range text {
		n, ok := normaliseRune(r)
		if !ok {
			continue
		}
		b.WriteRune(n)
		actor, ok := names[b.String()]
		if !ok {
			continue
		}
		next := i + utf8.RuneLen(r)
		if next < len(text) && isASCIIAlnum(text[next]) {
			continue
		}
		found, end = actor, next
	}
	if found == "" {
		return "", "", false
	}

	rest := strings.TrimLeftFunc(text[end:], isSeparator)
	for _, h := range honorifics {
		if strings.HasPrefix(rest, h) {
			rest = strings.TrimLeftFunc(rest[len(h):], isSeparator)
			break
		}
	}
	return found, strings.TrimSpace(rest), true
}

func isASCIIAlnum(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

func isSeparator(r rune) bool {
	return unicode.IsSpace(r) || unicode.IsPunct(r)
}

// matchActor finds the actor whose name best matches spoken by alias, exact
// name, substring, and then fuzzy edit distance, all after normalising.
func matchActor(spoken string) (string, bool) {
	norm := normalise(spoken)
	if norm == "" {
		return "", false
	}
	// 0. Alias match: check explicit alternate names before anything else.
	for actor, aliases := range actorAliases {
		for _, alias := range aliases {
			if normalise(alias) == norm {
				return actor, true
			}
		}
	}
	// 1. Exact match after normalisation.
	for _, p := range actors {
		if normalise(p) == norm {
			return p, true
		}
	}
	// 2. Substring fallback.
	for _, p := range actors {
		np := normalise(p)
		if strings.Contains(np, norm) || strings.Contains(norm, np) {
			return p, true
		}
	}
	// 3. Fuzzy fallback: pick the actor with the smallest edit distance.
	best, bestDist := "", int(^uint(0)>>1)
	for _, p := range actors {
		if d := levenshtein(norm, normalise(p)); d < bestDist {
			bestDist = d
			best = p
		}
	}
	if best != "" {
		maxLen := utf8.RuneCountInString(norm)
		if n := utf8.RuneCountInString(normalise(best)); n > maxLen {
			maxLen = n
		}
		if float64(bestDist) <= float64(maxLen)*fuzzyThreshold {
			return best, true
		}
	}
	return "", false
}

// levenshtein returns the edit distance in runes between as and bs.
func levenshtein(as, bs string) int {
	a, b := []rune(as), []rune(bs)
	if len(a) == 0 {
		return len(b)
	}
	if len(b) == 0 {
		return len(a)
	}
	prev := make([]int, len(b)+1)
	curr := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i, ra := range a {
		curr[0] = i + 1
		for j, rb := range b {
			if ra == rb {
				curr[j+1] = prev[j]
			} else {
				curr[j+1] = 1 + minInt(prev[j+1], curr[j], prev[j])
			}
		}
		prev, curr = curr, prev
	}
	return prev[len(b)]
}

func minInt(a, b, c int) int {
	if b < a {
		a = b
	}
	if c < a {
		a = c
	}
	return a
}

// normalise lowercases s, turns hiragana into katakana, and strips everything
// that is not a letter or digit.
func normalise(s string) string {
	var b strings.Builder
	for _, r := range s {
		if n, ok := normaliseRune(r); ok {
			b.WriteRune(n)
		}
	}
	return b.String()
}

func normaliseRune(r rune) (rune, bool) {
	r = unicode.ToLower(r)
	if r >= 'ぁ' && r <= 'ゖ' {
		r += 'ァ' - 'ぁ'
	}
	return r, unicode.IsLetter(r) || unicode.IsDigit(r)
}
