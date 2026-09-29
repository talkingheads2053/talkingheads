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

	for _, in := range []string{"こんにちは", "jamie tell me"} {
		if _, _, err := parseTranscript(in); err == nil {
			t.Errorf("parseTranscript(%q) returned nil error", in)
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
