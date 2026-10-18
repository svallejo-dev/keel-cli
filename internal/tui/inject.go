package tui

import (
	"fmt"
	"os"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/svallejo-dev/keel-cli/internal/domain"
	"github.com/svallejo-dev/keel-cli/internal/inject"
)

// injectScreen previews and writes connection config for an instance.
type injectScreen struct {
	inst    *domain.Instance
	formats []inject.Format
	fIdx    int
	mode    inject.Mode
	vp      viewport.Model
	init    bool
	note    string
}

func newInject(inst *domain.Instance) *injectScreen {
	return &injectScreen{
		inst:    inst,
		formats: []inject.Format{inject.FormatDotenv, inject.FormatEnv, inject.FormatJSON, inject.FormatYAML, inject.FormatPowerShell},
		mode:    inject.ModeHost,
	}
}

func (s *injectScreen) Title() string       { return "Inject" }
func (s *injectScreen) Init(a *App) tea.Cmd { return nil }

func (s *injectScreen) format() inject.Format { return s.formats[s.fIdx] }

func (s *injectScreen) render(a *App) string {
	bundle := inject.Build([]*domain.Instance{s.inst}, a.mgr.Config(), s.mode)
	out, err := bundle.Render(s.format())
	if err != nil {
		return badStyle.Render(err.Error())
	}
	return out
}

func (s *injectScreen) ensure(a *App) {
	if !s.init {
		s.vp = viewport.New(a.width, a.bodyHeight()-1)
		s.init = true
	}
	s.vp.Width = a.width
	s.vp.Height = a.bodyHeight() - 1
	s.vp.SetContent(s.render(a))
}

func (s *injectScreen) Update(a *App, msg tea.Msg) (screen, tea.Cmd) {
	s.ensure(a)
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "f":
			s.fIdx = (s.fIdx + 1) % len(s.formats)
			s.ensure(a)
			return s, nil
		case "m":
			if s.mode == inject.ModeHost {
				s.mode = inject.ModeDocker
			} else {
				s.mode = inject.ModeHost
			}
			s.ensure(a)
			return s, nil
		case "w":
			file := s.inst.Name + fileExt(s.format())
			if err := os.WriteFile(file, []byte(s.render(a)), 0o600); err != nil {
				s.note = badStyle.Render("write failed: " + err.Error())
			} else {
				s.note = okStyle.Render("wrote " + file)
			}
			return s, nil
		}
	}
	var cmd tea.Cmd
	s.vp, cmd = s.vp.Update(msg)
	return s, cmd
}

func (s *injectScreen) View(a *App) string {
	s.ensure(a)
	bar := fmt.Sprintf("  format: %s   mode: %s   %s",
		accentStyle.Render(string(s.format())), accentStyle.Render(string(s.mode)), s.note)
	return bar + "\n" + s.vp.View()
}

func (s *injectScreen) Footer(a *App) string {
	return "f format · m host/docker · w write file · ↑/↓ scroll · esc back"
}

func fileExt(f inject.Format) string {
	switch f {
	case inject.FormatJSON:
		return ".json"
	case inject.FormatYAML:
		return ".yaml"
	default:
		return ".env"
	}
}
