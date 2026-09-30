package main

import (
	"strings"

	bannerpkg "github.com/talkingheads2053/talkingheads/pkg/banner"
)

// jaNames holds the katakana banner names of the cast.
var jaNames = map[string]string{
	"gemmai":  "ジェマイ",
	"phineas": "フィニアス",
	"qwentin": "クエンティン",
}

// makeBanner renders the startup banner. In ja mode it uses bannerName,
// or the cast's katakana name, and falls back to the English name.
func makeBanner(name, bannerName, lang string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "ACTOR"
	}
	if lang == "ja" {
		kana := strings.TrimSpace(bannerName)
		if kana == "" {
			kana = jaNames[strings.ToLower(name)]
		}
		if out := bannerpkg.GenerateKatakana(kana); strings.Contains(out, "#") {
			return out
		}
	}
	return bannerpkg.Generate(strings.ToUpper(name))
}
