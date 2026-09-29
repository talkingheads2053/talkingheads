package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/talkingheads2053/talkingheads/pkg/dialogue"
	"github.com/urfave/cli/v2"
)

var version = "dev"

func main() {
	app := &cli.App{
		Name:  "dialogue",
		Usage: "text-to-speech via configurable voices",
		Authors: []*cli.Author{
			{Name: "deadprogram"},
		},
		Commands: []*cli.Command{
			{
				Name:  "serve",
				Usage: "connect to an MQTT server and process speak messages",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:     "server",
						Usage:    "MQTT server URL (e.g. tcp://localhost:1883)",
						Required: true,
						Aliases:  []string{"s"},
					},
					&cli.StringSliceFlag{
						Name:     "voice",
						Usage:    "voice specification in name:lang:model format, or name:voicevox:<style id>, repeatable",
						Required: true,
						Aliases:  []string{"v"},
					},
					&cli.StringFlag{
						Name:    "data",
						Usage:   "data directory containing voice model files",
						Value:   "./voices",
						Aliases: []string{"d"},
					},
					&cli.StringFlag{
						Name:  "voicevox-data",
						Usage: "VOICEVOX runtime directory created by the VOICEVOX downloader",
						Value: "./voicevox_core",
					},
					&cli.BoolFlag{
						Name:  "gpu",
						Usage: "use GPU acceleration for TTS",
					},
				},
				Action: serveAction,
			},
			{
				Name:  "say",
				Usage: "say something once with a single voice and exit",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:     "name",
						Usage:    "name of the voice speaker",
						Required: true,
						Aliases:  []string{"n"},
					},
					&cli.StringFlag{
						Name:     "lang",
						Usage:    "language code (e.g. en_US), or voicevox",
						Required: true,
						Aliases:  []string{"l"},
					},
					&cli.StringFlag{
						Name:     "voice",
						Usage:    "voice model name, or style id for voicevox",
						Required: true,
						Aliases:  []string{"v"},
					},
					&cli.StringFlag{
						Name:    "data",
						Usage:   "data directory containing voice model files",
						Value:   "./voices",
						Aliases: []string{"d"},
					},
					&cli.StringFlag{
						Name:  "voicevox-data",
						Usage: "VOICEVOX runtime directory created by the VOICEVOX downloader",
						Value: "./voicevox_core",
					},
					&cli.StringFlag{
						Name:     "say",
						Usage:    "text to speak",
						Required: true,
					},
					&cli.BoolFlag{
						Name:  "gpu",
						Usage: "use GPU acceleration for TTS",
					},
					&cli.BoolFlag{
						Name:    "verbose",
						Usage:   "enable verbose logging",
						Value:   false,
						Aliases: []string{"V"},
					},
				},
				Action: sayAction,
			},
		},
	}

	if err := app.Run(os.Args); err != nil {
		log.Fatal(err)
	}
}

func serveAction(c *cli.Context) error {
	server := c.String("server")
	gpu := c.Bool("gpu")
	verbose := c.Bool("verbose")

	var voiceNames []string
	voices := make(map[string]*dialogue.Voice)
	for _, spec := range c.StringSlice("voice") {
		parts := strings.SplitN(spec, ":", 3)
		if len(parts) != 3 {
			return cli.Exit(fmt.Sprintf("invalid voice format %q: expected name:lang:model", spec), 1)
		}
		name, lang, model := parts[0], parts[1], parts[2]
		v, err := dialogue.NewVoice(name, lang, model, voiceDataDir(c, lang), gpu)
		if err != nil {
			return cli.Exit(fmt.Sprintf("failed to create voice %q: %v", name, err), 1)
		}
		voices[name] = v
		voiceNames = append(voiceNames, name)
	}

	listener, err := dialogue.NewListener("dialogue", server, voices, verbose)
	if err != nil {
		return cli.Exit(err, 1)
	}
	defer listener.Close()

	eventsCh := make(chan string, 64)
	listener.SetEventsCh(eventsCh)

	go listener.Listen()

	// Silence log output once the TUI takes over — meaningful events are
	// already forwarded through eventsCh.
	log.SetOutput(io.Discard)

	m := newTUIModel(banner, eventsCh, voiceNames)
	p := tea.NewProgram(m, tea.WithAltScreen())

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	go func() {
		<-ctx.Done()
		p.Send(tea.Quit())
	}()

	_, err = p.Run()
	return err
}

func sayAction(c *cli.Context) error {
	name := c.String("name")
	lang := c.String("lang")
	model := c.String("voice")
	gpu := c.Bool("gpu")
	text := c.String("say")

	v, err := dialogue.NewVoice(name, lang, model, voiceDataDir(c, lang), gpu)
	if err != nil {
		return cli.Exit(fmt.Sprintf("failed to create voice: %v", err), 1)
	}

	return v.SayOnce(text)
}

func voiceDataDir(c *cli.Context, lang string) string {
	if lang == dialogue.Voicevox {
		return c.String("voicevox-data")
	}
	return c.String("data")
}
