package main

import "testing"

func withActors(t *testing.T, list []string, aliases map[string][]string) {
	t.Helper()
	oldActors, oldAliases := actors, actorAliases
	actors, actorAliases = list, aliases
	t.Cleanup(func() { actors, actorAliases = oldActors, oldAliases })
}

func TestParseTranscript(t *testing.T) {
	withActors(t, []string{"gemmai", "phineas", "qwentin"}, map[string][]string{
		"gemmai":  {"jami", "ジェマイ", "ジェマ"},
		"phineas": {"フィニアス"},
		"qwentin": {"クエンティン"},
	})

	cases := []struct{ in, to, content string }{
		{"gemmai, tell me a joke", "gemmai", "tell me a joke"},
		{"Jami: hello", "gemmai", "hello"},
		{"ジェマイ、自己紹介してください。", "gemmai", "自己紹介してください。"},
		{"ジェマイ自己紹介してください。", "gemmai", "自己紹介してください。"},
		{"じぇまいさん、こんにちは", "gemmai", "こんにちは"},
		{"ジェマイさんこんにちは", "gemmai", "こんにちは"},
		{"フィニアス　どう思いますか？", "phineas", "どう思いますか？"},
		{"クエンティンはどう思う？", "qwentin", "はどう思う？"},
		{"クエンティンサン、おはようございます。", "qwentin", "おはようございます。"},
	}
	for _, c := range cases {
		to, content, err := parseTranscript(c.in)
		if err != nil {
			t.Errorf("parseTranscript(%q): %v", c.in, err)
			continue
		}
		if to != c.to || content != c.content {
			t.Errorf("parseTranscript(%q) = %q, %q, want %q, %q", c.in, to, content, c.to, c.content)
		}
	}

	if _, _, err := parseTranscript("こんにちは"); err == nil {
		t.Error("parseTranscript(こんにちは) returned nil error")
	}
	if _, _, ok := matchActorPrefix("jamie tell me"); ok {
		t.Error("matchActorPrefix matched jami inside jamie")
	}
}

func TestParseTranscriptFuzzyAlias(t *testing.T) {
	withActors(t, []string{"gemmai", "phineas", "qwentin"}, map[string][]string{
		"gemmai":  {"ジェマイ"},
		"phineas": {"フィニアス"},
		"qwentin": {"クエンティン"},
	})
	old := fuzzyThreshold
	fuzzyThreshold = 0.4
	t.Cleanup(func() { fuzzyThreshold = old })

	to, content, err := parseTranscript("コヘンティンさん、こんにちは。")
	if err != nil {
		t.Fatal(err)
	}
	if to != "qwentin" || content != "こんにちは。" {
		t.Errorf("got %q, %q", to, content)
	}
}

func TestIsWhisperFiller(t *testing.T) {
	for _, in := range []string{"ご視聴ありがとうございました。", " Thank you for watching! ", "you"} {
		if !isWhisperFiller(in) {
			t.Errorf("isWhisperFiller(%q) = false", in)
		}
	}
	for _, in := range []string{"ジェマイ、ご視聴ありがとうございました。", "gemmai, thank you"} {
		if isWhisperFiller(in) {
			t.Errorf("isWhisperFiller(%q) = true", in)
		}
	}
}

func TestLevenshteinRunes(t *testing.T) {
	if d := levenshtein("ジェマイ", "ジェマ"); d != 1 {
		t.Errorf("levenshtein = %d, want 1", d)
	}
	if d := levenshtein("kitten", "sitting"); d != 3 {
		t.Errorf("levenshtein = %d, want 3", d)
	}
}

func TestNormaliseHiragana(t *testing.T) {
	if got := normalise("じぇまい"); got != "ジェマイ" {
		t.Errorf("normalise = %q", got)
	}
}
