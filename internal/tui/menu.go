package tui

import (
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/svallejo-dev/keel-cli/internal/domain"
)

// menu lists the runnable actions for an instance.
type menu struct {
	inst *domain.Instance
	list list.Model
	init bool
}

func newMenu(inst *domain.Instance) *menu { return &menu{inst: inst} }

func (m *menu) Title() string { return "Operations" }

func (m *menu) Init(a *App) tea.Cmd { return nil }

type menuItem struct{ act action }

func (mi menuItem) Title() string {
	if mi.act.danger {
		return badStyle.Render(mi.act.label)
	}
	return mi.act.label
}
func (mi menuItem) Description() string {
	switch {
	case mi.act.danger:
		return "destructive — asks for confirmation"
	case len(mi.act.fields) > 0:
		return "opens a form"
	default:
		return "runs immediately"
	}
}
func (mi menuItem) FilterValue() string { return mi.act.label }

func (m *menu) ensure(a *App) {
	if m.init {
		m.list.SetSize(a.width, a.bodyHeight())
		return
	}
	acts := actionsFor(m.inst)
	items := make([]list.Item, len(acts))
	for i, act := range acts {
		items[i] = menuItem{act}
	}
	l := list.New(items, list.NewDefaultDelegate(), a.width, a.bodyHeight())
	l.Title = m.inst.Name
	l.SetShowTitle(false)
	l.SetShowStatusBar(false)
	l.SetShowHelp(false)
	m.list = l
	m.init = true
}

func (m *menu) Update(a *App, msg tea.Msg) (screen, tea.Cmd) {
	m.ensure(a)
	if key, ok := msg.(tea.KeyMsg); ok && key.String() == "enter" {
		if it, ok := m.list.SelectedItem().(menuItem); ok {
			act := it.act
			switch {
			case len(act.fields) > 0:
				return m, push(newForm(m.inst, act))
			case act.danger:
				return m, push(newConfirm(m.inst, act, nil, 1)) // pop the confirm
			default:
				return m, runActionCmd(a, m.inst, act, nil, 0)
			}
		}
	}
	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m *menu) View(a *App) string { m.ensure(a); return m.list.View() }

func (m *menu) Footer(a *App) string {
	return "↑/↓ move · enter run · / filter · esc back"
}
