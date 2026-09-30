package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/talkingheads2053/talkingheads/pkg/hotmic"
)

// ── styles ────────────────────────────────────────────────────────────────────

var (
	bannerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("39")) // bright cyan

	separatorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("238"))

	statusStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241"))

	recordingStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("196")). // red
			Bold(true)

	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("196"))

	sentStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("35")) // green

	hotmicStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("214")) // orange
)

// ── messages ──────────────────────────────────────────────────────────────────

// hotmicStartedMsg is sent after StartCapture succeeds.
type hotmicStartedMsg struct{}

// hotmicTranscriptMsg carries the result of a full capture+transcribe cycle.
type hotmicTranscriptMsg struct {
	text string
	err  error
}

// questionSentMsg is sent after a question has been delivered to the
// conversation channel.
type questionSentMsg struct{ q question }

// ── commands ──────────────────────────────────────────────────────────────────

// startRecordingCmd begins audio capture and returns hotmicStartedMsg.
func startRecordingCmd(mic *hotmic.HotMic) tea.Cmd {
	return func() tea.Msg {
		if err := mic.StartCapture(); err != nil {
			return hotmicTranscriptMsg{err: err}
		}
		return hotmicStartedMsg{}
	}
}

// stopAndTranscribeCmd stops capture, transcribes the audio, and returns
// hotmicTranscriptMsg.
func stopAndTranscribeCmd(mic *hotmic.HotMic) tea.Cmd {
	return func() tea.Msg {
		samples, err := mic.StopCapture()
		if err != nil {
			return hotmicTranscriptMsg{err: err}
		}
		if len(samples) == 0 {
			return hotmicTranscriptMsg{text: ""}
		}
		text, err := mic.Transcribe(samples)
		return hotmicTranscriptMsg{text: text, err: err}
	}
}

// sendQuestionCmd delivers q to the conversation channel (non-blocking from
// the Update perspective) and returns questionSentMsg.
func sendQuestionCmd(questions chan question, q question) tea.Cmd {
	return func() tea.Msg {
		questions <- q
		return questionSentMsg{q: q}
	}
}

// ── model ─────────────────────────────────────────────────────────────────────

type tuiModel struct {
	// static header
	banner string

	// dynamic UI components
	vp    viewport.Model
	input textinput.Model

	// layout
	width, height int
	ready         bool // true after first WindowSizeMsg

	// hotmic (nil when disabled)
	mic       *hotmic.HotMic
	hotmicKey string // bubbletea key name, e.g. "f5"
	recording bool

	// conversation bridge
	questions chan question

	// accumulated output lines rendered inside the viewport
	lines []string
}

// newTUIModel constructs the initial model.
//
//   - bannerText is the pre-rendered ASCII banner string.
//   - questions is the channel to the conversation goroutine.
//   - mic may be nil if hotmic is not configured.
//   - hotmicKey is a bubbletea key name string (e.g. "f5", "ctrl+r").
func newTUIModel(bannerText string, questions chan question, mic *hotmic.HotMic, hotmicKey string) tuiModel {
	ti := textinput.New()
	ti.Placeholder = fmt.Sprintf("actor: question  (actors: %v)", actors)
	ti.Focus()
	ti.CharLimit = 512

	return tuiModel{
		banner:    bannerText,
		input:     ti,
		questions: questions,
		mic:       mic,
		hotmicKey: hotmicKey,
	}
}

func (m tuiModel) Init() tea.Cmd {
	return textinput.Blink
}

// ── update ────────────────────────────────────────────────────────────────────

func (m tuiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var (
		vpCmd    tea.Cmd
		inputCmd tea.Cmd
	)

	switch msg := msg.(type) {

	// ── window resize ────────────────────────────────────────────────────────
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

		bannerLines := strings.Count(m.banner, "\n") + 1
		// 1 separator after banner + 1 separator above input + 1 input line + 1 status bar
		reserved := bannerLines + 4
		vpHeight := m.height - reserved
		if vpHeight < 2 {
			vpHeight = 2
		}

		if !m.ready {
			m.vp = viewport.New(m.width, vpHeight)
			m.vp.SetContent("")
			m.ready = true
		} else {
			m.vp.Width = m.width
			m.vp.Height = vpHeight
		}
		m.vp, vpCmd = m.vp.Update(msg)
		return m, vpCmd

	// ── keyboard ─────────────────────────────────────────────────────────────
	case tea.KeyMsg:
		switch msg.String() {

		case "ctrl+c":
			return m, tea.Quit

		case m.hotmicKey:
			if m.mic == nil {
				break
			}
			if !m.recording {
				m.recording = true
				return m, startRecordingCmd(m.mic)
			}
			// recording → stop and transcribe
			m.recording = false
			return m, stopAndTranscribeCmd(m.mic)

		case "enter":
			text := strings.TrimSpace(m.input.Value())
			m.input.Reset()
			if text == "" {
				return m, nil
			}
			q, err := parseTypedInput(text, actors)
			if err != nil {
				m.appendLine(errorStyle.Render("error: " + err.Error()))
				return m, nil
			}
			return m, sendQuestionCmd(m.questions, q)

		default:
			m.input, inputCmd = m.input.Update(msg)
			return m, inputCmd
		}

	// ── hotmic messages ───────────────────────────────────────────────────────
	case hotmicStartedMsg:
		m.appendLine(hotmicStyle.Render("● recording… press F5 again to stop"))
		m.vp, vpCmd = m.vp.Update(msg)
		return m, vpCmd

	case hotmicTranscriptMsg:
		m.recording = false
		if msg.err != nil {
			m.appendLine(errorStyle.Render("hotmic error: " + msg.err.Error()))
			m.vp, vpCmd = m.vp.Update(msg)
			return m, vpCmd
		}
		if msg.text == "" || isWhisperFiller(msg.text) {
			m.appendLine(hotmicStyle.Render("hotmic: nothing transcribed"))
			m.vp, vpCmd = m.vp.Update(msg)
			return m, vpCmd
		}

		to, content, err := parseTranscript(msg.text)
		if err != nil {
			m.appendLine(errorStyle.Render("hotmic: " + err.Error()))
			m.vp, vpCmd = m.vp.Update(msg)
			return m, vpCmd
		}

		kind := kindDirection
		if rest, isSay := stripSayPrefix(content); isSay {
			kind = kindSay
			content = trimSurroundingQuotes(rest)
		}

		q := question{To: to, Content: content, Kind: kind}
		m.appendLine(hotmicStyle.Render(fmt.Sprintf("hotmic → %s: %q", to, content)))
		m.vp, vpCmd = m.vp.Update(msg)
		return m, tea.Batch(vpCmd, sendQuestionCmd(m.questions, q))

	// ── question confirmation ─────────────────────────────────────────────────
	case questionSentMsg:
		label := "→"
		if msg.q.Kind == kindSay {
			label = "say →"
		}
		m.appendLine(sentStyle.Render(fmt.Sprintf("%s %s: %q", label, msg.q.To, msg.q.Content)))
		m.vp, vpCmd = m.vp.Update(msg)
		return m, vpCmd
	}

	// Forward remaining messages to both viewport and input.
	m.vp, vpCmd = m.vp.Update(msg)
	m.input, inputCmd = m.input.Update(msg)
	return m, tea.Batch(vpCmd, inputCmd)
}

// appendLine adds a line to the viewport content and scrolls to the bottom.
func (m *tuiModel) appendLine(line string) {
	m.lines = append(m.lines, line)
	m.vp.SetContent(strings.Join(m.lines, "\n"))
	m.vp.GotoBottom()
}

// ── view ──────────────────────────────────────────────────────────────────────

func (m tuiModel) View() string {
	if !m.ready {
		return "initialising…\n"
	}

	sep := separatorStyle.Render(strings.Repeat("─", m.width))

	// Status bar: actors list + hotmic hint
	var statusParts []string
	statusParts = append(statusParts, fmt.Sprintf("actors: %v", actors))
	if m.mic != nil {
		if m.recording {
			statusParts = append(statusParts, recordingStyle.Render("● RECORDING (F5 to stop)"))
		} else {
			statusParts = append(statusParts, statusStyle.Render(m.hotmicKey+": record"))
		}
	}
	statusBar := statusStyle.Render(strings.Join(statusParts, "  ·  "))

	return strings.Join([]string{
		bannerStyle.Render(m.banner),
		sep,
		m.vp.View(),
		sep,
		m.input.View(),
		statusBar,
	}, "\n")
}
