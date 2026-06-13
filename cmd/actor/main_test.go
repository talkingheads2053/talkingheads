package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadThinkingPhrases_BasicPhrases(t *testing.T) {
	f := writeTempFile(t, "let me think...\ngive me a moment...\nhold on...\n")
	words, err := loadThinkingPhrases(f)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"let me think...", "give me a moment...", "hold on..."}
	if len(words) != len(want) {
		t.Fatalf("got %d words, want %d: %v", len(words), len(want), words)
	}
	for i, w := range want {
		if words[i] != w {
			t.Errorf("[%d] got %q, want %q", i, words[i], w)
		}
	}
}

func TestLoadThinkingPhrases_SkipsBlankLines(t *testing.T) {
	f := writeTempFile(t, "first phrase...\n\n\nsecond phrase...\n")
	words, err := loadThinkingPhrases(f)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(words) != 2 {
		t.Fatalf("got %d words, want 2: %v", len(words), words)
	}
}

func TestLoadThinkingPhrases_SkipsCommentLines(t *testing.T) {
	f := writeTempFile(t, "# this is a comment\nreal phrase...\n# another comment\n")
	words, err := loadThinkingPhrases(f)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(words) != 1 || words[0] != "real phrase..." {
		t.Fatalf("got %v, want [real phrase...]", words)
	}
}

func TestLoadThinkingPhrases_TrimsWhitespace(t *testing.T) {
	f := writeTempFile(t, "  leading spaces...\ntrailing spaces...  \n  both ends...  \n")
	words, err := loadThinkingPhrases(f)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"leading spaces...", "trailing spaces...", "both ends..."}
	for i, w := range want {
		if words[i] != w {
			t.Errorf("[%d] got %q, want %q", i, words[i], w)
		}
	}
}

func TestLoadThinkingPhrases_EmptyFile(t *testing.T) {
	f := writeTempFile(t, "")
	words, err := loadThinkingPhrases(f)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(words) != 0 {
		t.Errorf("expected empty slice, got %v", words)
	}
}

func TestLoadThinkingPhrases_FileNotFound(t *testing.T) {
	_, err := loadThinkingPhrases("/nonexistent/path/to/file.txt")
	if err == nil {
		t.Error("expected error for missing file, got nil")
	}
}

func writeTempFile(t *testing.T, content string) string {
	t.Helper()
	f := filepath.Join(t.TempDir(), "thinking_phrases.txt")
	if err := os.WriteFile(f, []byte(content), 0600); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}
	return f
}
