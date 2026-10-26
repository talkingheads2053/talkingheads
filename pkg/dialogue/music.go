package dialogue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/talkingheads2053/talkingheads/pkg/commands"
)

type musicPlayer interface {
	Play(ctx context.Context, b []byte, format string, started func()) error
}

type song struct {
	file   string
	format string
	data   []byte
	bpm    float64
	offset float64
}

// Music plays songs from a directory when Director asks, and publishes a dance
// beat on every beat of the song so the Actors can move in time.
type Music struct {
	client mqtt.Client
	dir    string
	player musicPlayer
	emit   func(string)

	mu     sync.Mutex
	song   *song
	cancel context.CancelFunc
}

// NewMusic subscribes to the music topic using the Listener's MQTT connection.
// Songs are loaded from dir.
func NewMusic(l *Listener, dir string) (*Music, error) {
	m := &Music{client: l.client, dir: dir, player: getSharedPlayer(), emit: l.emit}
	token := l.client.Subscribe("music", 0, m.handleMusic)
	if token.Wait() && token.Error() != nil {
		return nil, token.Error()
	}
	log.Printf("Subscribed to topic music\n")
	return m, nil
}

func (m *Music) handleMusic(_ mqtt.Client, msg mqtt.Message) {
	var c commands.Music
	if err := json.Unmarshal(msg.Payload(), &c); err != nil {
		log.Printf("Failed to unmarshal music message: %v\n", err)
		return
	}
	switch c.Command {
	case commands.MusicLoad:
		if err := m.Load(c.File, c.BPM, c.Offset); err != nil {
			m.emit(fmt.Sprintf("WARNING: music %v", err))
			return
		}
		m.emit(fmt.Sprintf("♪ loaded %s at %g bpm", c.File, c.BPM))
	case commands.MusicPlay:
		if err := m.Play(); err != nil {
			m.emit(fmt.Sprintf("WARNING: music %v", err))
		}
	case commands.MusicStop:
		m.Stop()
	default:
		m.emit(fmt.Sprintf("WARNING: unknown music command %q", c.Command))
	}
}

// Load reads a wav or mp3 file from the music directory to play next.
func (m *Music) Load(file string, bpm, offset float64) error {
	if !filepath.IsLocal(file) {
		return fmt.Errorf("file %q must be inside the music directory", file)
	}
	if bpm <= 0 {
		return errors.New("bpm must be more than 0")
	}
	format := strings.TrimPrefix(strings.ToLower(filepath.Ext(file)), ".")
	if format != "wav" && format != "mp3" {
		return fmt.Errorf("file %q must be wav or mp3", file)
	}
	data, err := os.ReadFile(filepath.Join(m.dir, file))
	if err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.song = &song{file: file, format: format, data: data, bpm: bpm, offset: offset}
	return nil
}

// Play starts the loaded song. It publishes a beat on every beat until the
// song ends or Stop is called, then publishes that the dance stopped.
func (m *Music) Play() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.song == nil {
		return errors.New("no song loaded")
	}
	if m.cancel != nil {
		return errors.New("already playing")
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	s := m.song

	go func() {
		var beats sync.WaitGroup
		m.emit(fmt.Sprintf("♪ playing %s", s.file))
		err := m.player.Play(ctx, s.data, s.format, func() {
			start := time.Now()
			beats.Go(func() { m.beat(ctx, start, s.bpm, s.offset) })
		})
		cancel()
		beats.Wait()
		m.publish(commands.Dance{Status: commands.StatusStopped})

		m.mu.Lock()
		m.cancel = nil
		m.mu.Unlock()
		if err != nil && !errors.Is(err, context.Canceled) {
			m.emit(fmt.Sprintf("WARNING: music %v", err))
			return
		}
		m.emit(fmt.Sprintf("♪ stopped %s", s.file))
	}()
	return nil
}

// Stop ends the song that is playing.
func (m *Music) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cancel != nil {
		m.cancel()
	}
}

func (m *Music) beat(ctx context.Context, start time.Time, bpm, offset float64) {
	t := time.NewTimer(0)
	defer t.Stop()
	<-t.C
	for n := 1; ; n++ {
		at := beatTime(n, bpm, offset)
		if at < 0 {
			continue
		}
		t.Reset(time.Until(start.Add(at)))
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		m.publish(commands.Dance{Status: commands.StatusDancing, Beat: n})
	}
}

// beatTime returns when beat n starts, counting from 1, measured from the
// start of the song.
func beatTime(n int, bpm, offset float64) time.Duration {
	return time.Duration((offset + float64(n-1)*60/bpm) * float64(time.Second))
}

// publish sends without waiting, so a slow broker does not delay the beat.
func (m *Music) publish(d commands.Dance) {
	payload, err := json.Marshal(d)
	if err != nil {
		log.Printf("Failed to marshal dance message: %v\n", err)
		return
	}
	m.client.Publish("dance", 0, false, payload)
}
