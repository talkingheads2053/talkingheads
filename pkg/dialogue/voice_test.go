package dialogue

import "testing"

func TestSayAnythingEmptyString(t *testing.T) {
	v := &Voice{Name: "test"}
	if err := v.SayAnything(""); err != nil {
		t.Errorf("SayAnything(\"\") returned unexpected error: %v", err)
	}
}

func TestNewSpeaker(t *testing.T) {
	if _, err := newSpeaker(Voicevox, "3", 0.85); err != nil {
		t.Errorf("newSpeaker(voicevox, 3) = %v", err)
	}
	if _, err := newSpeaker(Voicevox, "zundamon", 0); err == nil {
		t.Error("newSpeaker(voicevox, zundamon) returned nil error")
	}
	if _, err := newSpeaker("en_US", "hfc_female-medium", 0); err != nil {
		t.Errorf("newSpeaker(en_US) = %v", err)
	}
}
