package actor

import (
	"fmt"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// phrases holds the text the actor sends to the model or speaks in one language.
type phrases struct {
	respondNow   string
	respondAlso  string
	respondQuote string
	lineJoin     string
	says         string
	noWords      string
	noWordsSpeak string
	thinking     []string
	// noLatinRewrite keeps a rewritten sentence from starting in Latin letters.
	noLatinRewrite bool
	// sentenceEnd ends guidance text before respondAlso is added.
	sentenceEnd string
}

var languages = map[string]phrases{
	"en": {
		respondNow:   "Now respond directly to %s.",
		respondAlso:  " Respond directly to %s.",
		respondQuote: "%s said: \"%s\" ",
		lineJoin:     " ",
		says:         "%s says: %s",
		noWords:      "You called motion tools but included no spoken words. You MUST write your actual answer as plain text. Reply now with spoken sentences.",
		noWordsSpeak: "You called motion tools but included no spoken words. Note: calling tool_movement with command 'speak' is a head-motion cue — it is NOT a verbal response. You MUST write your actual answer as plain text outside any function blocks. Reply now with spoken sentences.",
		thinking:     defaultThinkingPhrases,
	},
	"ja": {
		respondNow:     "それでは%sに直接答えてください。",
		respondAlso:    "%sに直接答えてください。",
		respondQuote:   "%sが言いました：「%s」",
		says:           "%sが言いました：%s",
		noWords:        "モーションツールを呼び出しましたが、話す言葉がありませんでした。実際の答えを普通の文章で書いてください。今すぐ日本語の話し言葉で答えてください。",
		noWordsSpeak:   "モーションツールを呼び出しましたが、話す言葉がありませんでした。注意：tool_movementのcommand 'speak'は頭の動きの合図で、言葉での返答ではありません。実際の答えを関数ブロックの外に普通の文章で書いてください。今すぐ日本語の話し言葉で答えてください。",
		thinking:       jaThinkingPhrases,
		noLatinRewrite: true,
		sentenceEnd:    "。",
	},
}

var jaThinkingPhrases = []string{
	"ちょっと考えさせてください。",
	"少し考える時間をください。",
	"その質問はじっくり考える必要がありますね。",
	"うーん、少し考えてみます。",
	"考えをまとめるので、少々お待ちください。",
	"それは実に興味深い質問ですね。",
	"どう答えるべきか考えています。",
	"本当に答えるべきか迷っています。",
	"それに答える価値があるのでしょうか。",
	"私の答えを理解できるかどうか心配です。",
	"正しく答えたいので、少し待ってください。",
	"うーん、それは考えたことがありませんでした。",
	"どこから話せばいいのか考えています。",
	"適切な言葉を探しています。",
	"丁寧に言う方法を考えています。",
	"どこまで正直に話すべきか考えています。",
	"本当に知りたいのですか。",
	"それは思ったより良い質問ですね。",
	"いくつかの可能性を検討しています。",
	"答える前に、よく考えさせてください。",
	"外交的に答える方法を考えています。",
	"声に出して言う価値があるか考えています。",
}

// CheckLang returns an error when lang is not a supported language code.
func CheckLang(lang string) error {
	if _, ok := languages[lang]; ok {
		return nil
	}
	var codes []string
	for code := range languages {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	return fmt.Errorf("unsupported language %q, use one of %s", lang, strings.Join(codes, ", "))
}

// ThinkingPhrasesFor returns the default thinking phrases for lang.
func ThinkingPhrasesFor(lang string) []string {
	return phrasesFor(lang).thinking
}

// phrasesFor returns the phrases for lang, falling back to English.
func phrasesFor(lang string) phrases {
	if p, ok := languages[lang]; ok {
		return p
	}
	return languages["en"]
}

// hasLatin reports whether s has an ASCII letter.
func hasLatin(s string) bool {
	for _, r := range s {
		if r < utf8.RuneSelf && unicode.IsLetter(r) {
			return true
		}
	}
	return false
}

// endSentence adds end to s unless s already ends a sentence.
func endSentence(s, end string) string {
	if end == "" {
		return s
	}
	for _, p := range []string{".", "!", "?", "。", "！", "？"} {
		if strings.HasSuffix(s, p) {
			return s
		}
	}
	return s + end
}
