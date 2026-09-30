package banner

import (
	"strings"
	"testing"
)

func TestGenerateNonEmpty(t *testing.T) {
	out := Generate("HI")
	if out == "" {
		t.Fatal("Generate returned empty string")
	}
	if !strings.HasPrefix(out, "\n") || !strings.HasSuffix(out, "\n") {
		t.Errorf("Generate output should start and end with newline, got %q", out)
	}
	if !strings.Contains(out, "#") {
		t.Errorf("Generate output should contain '#' characters, got:\n%s", out)
	}
}

func TestGenerateMultipleLines(t *testing.T) {
	out := Generate("HI")
	// FIGlet banner font glyphs are 7 rows tall; with leading + trailing
	// newlines the split should yield at least 9 elements.
	lines := strings.Split(out, "\n")
	if len(lines) < 9 {
		t.Errorf("expected at least 9 lines, got %d:\n%s", len(lines), out)
	}
}

func TestGenerateDifferentInputsDiffer(t *testing.T) {
	a := Generate("ACTOR")
	b := Generate("DIALOGUE")
	if a == b {
		t.Error("Generate produced identical output for different inputs")
	}
}

func TestGenerateEmpty(t *testing.T) {
	// Empty input should not panic and should still return a string
	// containing only the leading and trailing newlines plus blank rows.
	out := Generate("")
	if out == "" {
		t.Error("Generate(\"\") returned empty string")
	}
}

func TestGenerateWithFont(t *testing.T) {
	standard := GenerateWithFont("HI", "standard")
	bannerFont := GenerateWithFont("HI", "banner")
	if standard == "" || bannerFont == "" {
		t.Fatal("GenerateWithFont returned empty string")
	}
	if standard == bannerFont {
		t.Error("expected different output for standard vs banner fonts")
	}
}

func TestGenerateWithFontMatchesGenerate(t *testing.T) {
	if Generate("HI") != GenerateWithFont("HI", "banner") {
		t.Error("Generate should match GenerateWithFont with the banner font")
	}
}

func TestTrimRightLines(t *testing.T) {
	in := "foo   \nbar\t\n  baz  \n"
	want := "foo\nbar\n  baz\n"
	if got := TrimRightLines(in); got != want {
		t.Errorf("TrimRightLines(%q) = %q, want %q", in, got, want)
	}
}

func TestTrimRightLinesNoTrailing(t *testing.T) {
	in := "foo\nbar\nbaz"
	if got := TrimRightLines(in); got != in {
		t.Errorf("TrimRightLines should be a no-op when there is no trailing whitespace, got %q", got)
	}
}

func TestTrimRightLinesEmpty(t *testing.T) {
	if got := TrimRightLines(""); got != "" {
		t.Errorf("TrimRightLines(\"\") = %q, want empty string", got)
	}
}

func TestTrimRightLinesPreservesLeading(t *testing.T) {
	in := "   indented   \n\tmixed\t  \n"
	want := "   indented\n\tmixed\n"
	if got := TrimRightLines(in); got != want {
		t.Errorf("TrimRightLines(%q) = %q, want %q", in, got, want)
	}
}

func TestGenerateKatakana(t *testing.T) {
	out := GenerateKatakana("ジェマイ")
	if !strings.HasPrefix(out, "\n") || !strings.HasSuffix(out, "\n") {
		t.Errorf("GenerateKatakana output should start and end with newline, got %q", out)
	}
	if !strings.Contains(out, "#") {
		t.Errorf("GenerateKatakana output should contain '#' characters, got:\n%s", out)
	}
	if GenerateKatakana("ジェマイ") == GenerateKatakana("フィニアス") {
		t.Error("GenerateKatakana produced identical output for different inputs")
	}
}

func TestGenerateKatakanaSkipsUnknown(t *testing.T) {
	if got, want := GenerateKatakana("ジェabcマイ"), GenerateKatakana("ジェマイ"); got != want {
		t.Errorf("unknown runes should be skipped, got:\n%s\nwant:\n%s", got, want)
	}
}
