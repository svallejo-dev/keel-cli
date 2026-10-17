package tui

import (
	"bytes"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/svallejo-dev/keel-cli/internal/domain"
)

// logsScreen shows an instance's recent logs.
type logsScreen struct {
	inst    *domain.Instance
	vp      viewport.Model
	init    bool
	content string
}

type logsLoaded struct {
	text string
	err  error
}

func newLogs(inst *domain.Instance) *logsScreen { return &logsScreen{inst: inst} }

func (s *logsScreen) Title() string { return "Logs" }

func (s *logsScreen) Init(a *App) tea.Cmd {
	inst := s.inst
	return func() tea.Msg {
		var buf bytes.Buffer
		err := a.mgr.Logs(a.ctx, inst.Name, false, &buf)
		return logsLoaded{text: buf.String(), err: err}
	}
}

func (s *logsScreen) ensure(a *App) {
	if !s.init {
		s.vp = viewport.New(a.width, a.bodyHeight())
		s.content = dimStyle.Render("loading logs…")
		s.init = true
	}
	s.vp.Width = a.width
	s.vp.Height = a.bodyHeight()
	s.vp.SetContent(s.content)
}

func (s *logsScreen) Update(a *App, msg tea.Msg) (screen, tea.Cmd) {
	s.ensure(a)
	if m, ok := msg.(logsLoaded); ok {
		if m.err != nil {
			s.content = badStyle.Render(m.err.Error())
		} else if m.text == "" {
			s.content = dimStyle.Render("(no logs)")
		} else {
			s.content = m.text
		}
		s.vp.SetContent(s.content)
		return s, nil
	}
	var cmd tea.Cmd
	s.vp, cmd = s.vp.Update(msg)
	return s, cmd
}

func (s *logsScreen) View(a *App) string {
	s.ensure(a)
	return s.vp.View()
}

func (s *logsScreen) Footer(a *App) string {
	return "↑/↓ scroll · esc back"
}
