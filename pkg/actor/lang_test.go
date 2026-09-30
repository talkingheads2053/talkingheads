package actor

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/talkingheads2053/talkingheads/pkg/commands"
)

func TestCheckLang(t *testing.T) {
	for _, lang := range []string{"en", "ja"} {
		if err := CheckLang(lang); err != nil {
			t.Errorf("CheckLang(%q) = %v", lang, err)
		}
	}
	if err := CheckLang("xx"); err == nil {
		t.Error("CheckLang(\"xx\") returned nil")
	}
}

func TestPhrasesFor_FallsBackToEnglish(t *testing.T) {
	if got := phrasesFor("xx").says; got != languages["en"].says {
		t.Errorf("phrasesFor(\"xx\").says = %q", got)
	}
}

func TestThinkingPhrasesFor(t *testing.T) {
	if got := ThinkingPhrasesFor("ja"); len(got) == 0 || got[0] != jaThinkingPhrases[0] {
		t.Errorf("ThinkingPhrasesFor(\"ja\") = %q", got)
	}
}

func TestHandleSpeak_Japanese(t *testing.T) {
	l := newTestListener("gemmai")
	l.SetLang("ja")

	payload, _ := json.Marshal(commands.Speak{Who: "phineas", What: "空は青いです。"})
	l.handleSpeak(nil, &mockMessage{payload: payload})

	select {
	case got := <-l.heard:
		if want := "phineasが言いました：空は青いです。"; got != want {
			t.Errorf("heard: got %q, want %q", got, want)
		}
	case <-time.After(time.Second):
		t.Error("timed out waiting for heard message")
	}
}

func TestHandleDirection_RespondJapanese(t *testing.T) {
	cases := []struct{ what, want string }{
		{"", "それではphineasに直接答えてください。"},
		{"反論してください。", "反論してください。phineasに直接答えてください。"},
	}
	for _, c := range cases {
		l := newTestListener("gemmai")
		l.SetLang("ja")
		l.lastSpeaker = "phineas"

		payload, _ := json.Marshal(commands.Direction{Who: "gemmai", What: c.what, Respond: true})
		l.handleDirection(nil, &mockMessage{payload: payload})

		if got := <-l.incoming; got != c.want {
			t.Errorf("incoming: got %q, want %q", got, c.want)
		}
	}
}

func TestHasLatin(t *testing.T) {
	for s, want := range map[string]bool{
		"Morning": true,
		" the":    true,
		"朝noon":   true,
		"こんにちは":   false,
		"。":       false,
		"123":     false,
		"ＴｉｎｙＧｏ":  false,
	} {
		if got := hasLatin(s); got != want {
			t.Errorf("hasLatin(%q) = %v, want %v", s, got, want)
		}
	}
}
