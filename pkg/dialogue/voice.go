package dialogue

import (
	"fmt"
	"log"
	"strconv"
	"sync"

	"github.com/talkingheads2053/sayanything/pkg/say"
	"github.com/talkingheads2053/sayanything/pkg/tts"
)

var (
	sharedPlayer     *say.Player
	sharedPlayerOnce sync.Once
)

func getSharedPlayer() *say.Player {
	sharedPlayerOnce.Do(func() {
		sharedPlayer = say.NewPlayer("wav")
	})
	return sharedPlayer
}

type Voice struct {
	Name string
	t    tts.Speaker
	p    *say.Player
}

// Voicevox is the lang value that selects VOICEVOX, with the voice as a style id.
const Voicevox = "voicevox"

type gpuSpeaker interface {
	tts.Speaker
	UseGPU(bool)
}

func NewVoice(name, lang, voice, dataDir string, gpu bool) (*Voice, error) {
	t, err := newSpeaker(lang, voice)
	if err != nil {
		return nil, err
	}

	t.UseGPU(gpu)
	if err := t.Connect(dataDir); err != nil {
		t.Close()
		return nil, err
	}

	return &Voice{Name: name, t: t, p: getSharedPlayer()}, nil
}

func newSpeaker(lang, voice string) (gpuSpeaker, error) {
	if lang != Voicevox {
		return tts.NewPiper(lang, voice), nil
	}

	style, err := strconv.ParseUint(voice, 10, 32)
	if err != nil {
		return nil, fmt.Errorf("voicevox voice must be a style id number, got %q", voice)
	}
	return tts.NewVoicevox(uint32(style)), nil
}

var speaking = 0

// SayOnce speaks the given text synchronously, blocking until playback is complete.
func (v *Voice) SayOnce(what string) error {
	if len(what) == 0 {
		return nil
	}

	log.Printf("%s says: %s", v.Name, what)

	b, err := v.t.Speech(what)
	if err != nil {
		return err
	}

	return v.p.Say(b)
}

func (v *Voice) SayAnything(what string) error {
	if len(what) == 0 {
		return nil
	}

	log.Printf("%s says: %s", v.Name, what)

	data, err := v.t.Speech(what)
	if err != nil {
		return err
	}

	speaking++

	go func() {
		v.p.Say(data)
		speaking--
	}()

	return nil
}
