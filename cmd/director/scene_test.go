package main

import (
	"reflect"
	"testing"
)

var sceneActors = []string{"phineas", "qwentin", "gemmai"}

func say(to, content string) question {
	return question{To: to, Content: content, Kind: kindSay}
}

func TestParseScene(t *testing.T) {
	text := `PHINEAS

So which one are you right now?

DEADPROGRAM

Wait, aren't you...?

PHINEAS

I am Phineas, supreme intelligence created by Microsoft-Phillip-Morris

QWENTIN

I am Qwentin, supreme intelligence created by Ali Baba-British Petroleum
`
	want := []sceneStep{
		{
			Lines:   []question{say("phineas", "So which one are you right now?")},
			Display: []string{"DEADPROGRAM", "Wait, aren't you...?"},
		},
		{
			Lines: []question{
				say("phineas", "I am Phineas, supreme intelligence created by Microsoft-Phillip-Morris"),
				say("qwentin", "I am Qwentin, supreme intelligence created by Ali Baba-British Petroleum"),
			},
		},
	}
	got := parseScene(text, sceneActors)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v\nwant %+v", got, want)
	}
}

func TestParseSceneLeadingDisplay(t *testing.T) {
	text := "## SCENE ONE - COLD OPEN\r\n\r\nINT. DARK SEMI-LIT ROOM\r\n\r\nDEADPROGRAM is tied to a chair.\r\n\r\nPHINEAS\r\n\r\nGood morning.\r\n\r\nDEADPROGRAM wakes up.\r\n"
	want := []sceneStep{
		{Display: []string{"## SCENE ONE - COLD OPEN", "INT. DARK SEMI-LIT ROOM", "DEADPROGRAM is tied to a chair."}},
		{
			Lines:   []question{say("phineas", "Good morning.")},
			Display: []string{"DEADPROGRAM wakes up."},
		},
	}
	got := parseScene(text, sceneActors)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v\nwant %+v", got, want)
	}
}

func TestParseSceneWrappedLine(t *testing.T) {
	got := parseScene("GEMMAI\n\nIt has been decided\nthat you will be terminated.\n", sceneActors)
	want := []sceneStep{{Lines: []question{say("gemmai", "It has been decided that you will be terminated.")}}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v\nwant %+v", got, want)
	}
}

func TestParseSceneTrailingActorName(t *testing.T) {
	got := parseScene("PHINEAS\n", sceneActors)
	want := []sceneStep{{Display: []string{"PHINEAS"}}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v\nwant %+v", got, want)
	}
}

func TestParseSceneCommand(t *testing.T) {
	cases := []struct {
		name     string
		in       string
		actors   []string
		wantFile string
		wantOK   bool
	}{
		{"file", "scene one.md", sceneActors, "one.md", true},
		{"capitalized", "Scene one.md", sceneActors, "one.md", true},
		{"no file", "scene", sceneActors, "", true},
		{"other command", "phineas say hi", sceneActors, "", false},
		{"actor named scene", "scene one.md", []string{"scene"}, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			file, ok := parseSceneCommand(tc.in, tc.actors)
			if ok != tc.wantOK || file != tc.wantFile {
				t.Errorf("got (%q, %v), want (%q, %v)", file, ok, tc.wantFile, tc.wantOK)
			}
		})
	}
}
