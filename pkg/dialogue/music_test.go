package dialogue

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/talkingheads2053/talkingheads/pkg/commands"
)

// fakePlayer plays for length, or until canceled.
type fakePlayer struct {
	length time.Duration
}

func (p *fakePlayer) Play(ctx context.Context, _ []byte, _ string, started func()) error {
	started()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(p.length):
		return nil
	}
}

func newTestMusic(t *testing.T, length time.Duration) (*Music, *mockMQTTClient) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "song.wav"), []byte("RIFF"), 0o644); err != nil {
		t.Fatal(err)
	}
	client := &mockMQTTClient{}
	return &Music{client: client, dir: dir, player: &fakePlayer{length: length}, emit: func(string) {}, moves: defaultMoves}, client
}

func dances(t *testing.T, client *mockMQTTClient) []commands.Dance {
	var out []commands.Dance
	for _, msg := range client.published() {
		if msg.topic != "dance" {
			t.Errorf("published to %q, want dance", msg.topic)
		}
		var d commands.Dance
		if err := json.Unmarshal(msg.payload, &d); err != nil {
			t.Fatal(err)
		}
		out = append(out, d)
	}
	return out
}

func waitStopped(t *testing.T, m *Music) {
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		m.mu.Lock()
		playing := m.cancel != nil
		m.mu.Unlock()
		if !playing {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("song did not stop")
}

func TestBeatTime(t *testing.T) {
	tests := []struct {
		n           int
		bpm, offset float64
		want        time.Duration
	}{
		{1, 120, 0, 0},
		{2, 120, 0, 500 * time.Millisecond},
		{3, 120, 0.25, 1250 * time.Millisecond},
		{2, 90, 0, 666666666},
		{1, 120, -0.1, -100 * time.Millisecond},
	}
	for _, tt := range tests {
		if got := beatTime(tt.n, tt.bpm, tt.offset); got != tt.want {
			t.Errorf("beatTime(%d, %g, %g) = %v, want %v", tt.n, tt.bpm, tt.offset, got, tt.want)
		}
	}
}

func TestMusicLoad_Errors(t *testing.T) {
	m, _ := newTestMusic(t, 0)
	tests := []struct {
		file string
		bpm  float64
	}{
		{"../song.wav", 120},
		{"song.wav", 0},
		{"song.ogg", 120},
		{"missing.wav", 120},
	}
	for _, tt := range tests {
		if err := m.Load(tt.file, tt.bpm, 0); err == nil {
			t.Errorf("Load(%q, %g) expected an error", tt.file, tt.bpm)
		}
	}
}

func TestMusicPlay_NeedsSong(t *testing.T) {
	m, _ := newTestMusic(t, 0)
	if err := m.Play(); err == nil {
		t.Error("expected an error with no song loaded")
	}
}

func TestMusicPlay_BeatsThenStops(t *testing.T) {
	m, client := newTestMusic(t, 110*time.Millisecond)
	// 1200 bpm is a beat every 50ms, so beats land at 0, 50 and 100ms.
	if err := m.Load("song.wav", 1200, 0); err != nil {
		t.Fatal(err)
	}
	if err := m.Play(); err != nil {
		t.Fatal(err)
	}
	waitStopped(t, m)

	got := dances(t, client)
	want := []commands.Dance{
		{Status: commands.StatusDancing, Beat: 1, Move: commands.MoveLeft},
		{Status: commands.StatusDancing, Beat: 2, Move: commands.MoveRight},
		{Status: commands.StatusDancing, Beat: 3, Move: commands.MoveLeft},
		{Status: commands.StatusStopped},
	}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("message %d = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestMusicMoves_RepeatPattern(t *testing.T) {
	m, _ := newTestMusic(t, 0)
	if err := m.SetMoves([]string{"L", "L", "R"}); err != nil {
		t.Fatal(err)
	}
	var got []string
	for n := 1; n <= 7; n++ {
		got = append(got, m.move(n))
	}
	if want := "L L R L L R L"; strings.Join(got, " ") != want {
		t.Errorf("got %v, want %s", got, want)
	}
}

func TestMusicMoves_Errors(t *testing.T) {
	m, _ := newTestMusic(t, 0)
	for _, moves := range [][]string{nil, {"L", "X"}} {
		if err := m.SetMoves(moves); err == nil {
			t.Errorf("expected an error for %v", moves)
		}
	}
	if got := m.move(2); got != commands.MoveRight {
		t.Errorf("default move for beat 2 = %q, want R", got)
	}
}

func TestMusicStop(t *testing.T) {
	m, client := newTestMusic(t, time.Minute)
	if err := m.Load("song.wav", 60, 0); err != nil {
		t.Fatal(err)
	}
	if err := m.Play(); err != nil {
		t.Fatal(err)
	}
	if err := m.Play(); err == nil {
		t.Error("expected an error when already playing")
	}
	m.Stop()
	waitStopped(t, m)

	got := dances(t, client)
	if len(got) == 0 || got[len(got)-1].Status != commands.StatusStopped {
		t.Errorf("expected the dance to stop, got %v", got)
	}
}

func TestHandleMusic(t *testing.T) {
	m, client := newTestMusic(t, time.Minute)
	send := func(c commands.Music) {
		payload, _ := json.Marshal(c)
		m.handleMusic(nil, &mockMessage{payload: payload, topic: "music"})
	}

	send(commands.Music{Command: commands.MusicMoves, Moves: "R R L"})
	if got := m.move(1); got != commands.MoveRight {
		t.Errorf("first move = %q, want R", got)
	}
	send(commands.Music{Command: commands.MusicLoad, File: "song.wav", BPM: 60})
	send(commands.Music{Command: commands.MusicPlay})
	send(commands.Music{Command: commands.MusicStop})
	waitStopped(t, m)

	got := dances(t, client)
	if len(got) == 0 || got[len(got)-1].Status != commands.StatusStopped {
		t.Errorf("expected the song to play and stop, got %v", got)
	}
}
