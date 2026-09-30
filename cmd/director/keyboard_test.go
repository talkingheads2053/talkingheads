package main

import "testing"

func TestStripSayPrefix(t *testing.T) {
	cases := []struct {
		name     string
		in       string
		wantRest string
		wantOK   bool
	}{
		{"plain", "say hello world", "hello world", true},
		{"capitalized", "Say hello", "hello", true},
		{"colon suffix", "say: hello", "hello", true},
		{"comma suffix", "say, hello", "hello", true},
		{"with quotes", `say "hello world"`, `"hello world"`, true},
		{"leading space", "   say hello", "hello", true},
		{"only say", "say", "", true},
		{"not say", "tell me a joke", "tell me a joke", false},
		{"say embedded", "please say hi", "please say hi", false},
		{"empty", "", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rest, ok := stripSayPrefix(tc.in)
			if ok != tc.wantOK {
				t.Fatalf("ok: got %v, want %v", ok, tc.wantOK)
			}
			if rest != tc.wantRest {
				t.Errorf("rest: got %q, want %q", rest, tc.wantRest)
			}
		})
	}
}

func TestTrimSurroundingQuotes(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{`"hello"`, "hello"},
		{`  "hello world"  `, "hello world"},
		{`hello`, "hello"},
		{`"hello`, `"hello`},
		{`hello"`, `hello"`},
		{`""`, ""},
		{`"`, `"`},
		{``, ``},
		{`"a "quoted" b"`, `a "quoted" b`},
	}
	for _, tc := range cases {
		got := trimSurroundingQuotes(tc.in)
		if got != tc.want {
			t.Errorf("trimSurroundingQuotes(%q): got %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestStripRespondPrefix(t *testing.T) {
	cases := []struct {
		name     string
		in       string
		wantRest string
		wantOK   bool
	}{
		{"bare respond", "respond", "", true},
		{"capitalized", "Respond", "", true},
		{"with extra guidance", "respond keep it brief", "keep it brief", true},
		{"colon suffix", "respond: keep it brief", "keep it brief", true},
		{"comma suffix", "respond, keep it brief", "keep it brief", true},
		{"leading space", "   respond now", "now", true},
		{"not respond", "tell me a joke", "tell me a joke", false},
		{"respond embedded", "please respond here", "please respond here", false},
		{"empty", "", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rest, ok := stripRespondPrefix(tc.in)
			if ok != tc.wantOK {
				t.Fatalf("ok: got %v, want %v", ok, tc.wantOK)
			}
			if rest != tc.wantRest {
				t.Errorf("rest: got %q, want %q", rest, tc.wantRest)
			}
		})
	}
}

func TestParseTypedInput_RespondKind(t *testing.T) {
	actors := []string{"gemmai", "phineas", "qwentin"}

	cases := []struct {
		name          string
		in            string
		wantTo        string
		wantContent   string
		wantKind      questionKind
		wantRespondTo string
	}{
		{
			name:        "bare respond",
			in:          "gemmai respond",
			wantTo:      "gemmai",
			wantContent: "",
			wantKind:    kindRespond,
		},
		{
			name:        "respond with guidance",
			in:          "phineas respond keep it short",
			wantTo:      "phineas",
			wantContent: "keep it short",
			wantKind:    kindRespond,
		},
		{
			name:        "respond colon",
			in:          "gemmai respond: now please",
			wantTo:      "gemmai",
			wantContent: "now please",
			wantKind:    kindRespond,
		},
		{
			name:          "respond to actor",
			in:            "gemmai respond qwentin",
			wantTo:        "gemmai",
			wantKind:      kindRespond,
			wantRespondTo: "qwentin",
		},
		{
			name:          "respond to actor with guidance",
			in:            "gemmai respond to qwentin keep it short",
			wantTo:        "gemmai",
			wantContent:   "keep it short",
			wantKind:      kindRespond,
			wantRespondTo: "qwentin",
		},
		{
			name:        "respond to non actor",
			in:          "gemmai respond to the crowd",
			wantTo:      "gemmai",
			wantContent: "to the crowd",
			wantKind:    kindRespond,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			q, err := parseTypedInput(tc.in, actors)
			if err != nil {
				t.Fatalf("parseTypedInput: %v", err)
			}
			if q.To != tc.wantTo {
				t.Errorf("To: got %q, want %q", q.To, tc.wantTo)
			}
			if q.Content != tc.wantContent {
				t.Errorf("Content: got %q, want %q", q.Content, tc.wantContent)
			}
			if q.Kind != tc.wantKind {
				t.Errorf("Kind: got %v, want %v", q.Kind, tc.wantKind)
			}
			if q.RespondTo != tc.wantRespondTo {
				t.Errorf("RespondTo: got %q, want %q", q.RespondTo, tc.wantRespondTo)
			}
		})
	}
}
