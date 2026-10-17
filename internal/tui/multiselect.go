package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// toggle is one on/off option in a multiselect, optionally grouped by section.
type toggle struct {
	key     string
	label   string
	desc    string
	group   string
	checked bool
}

// multiselect is a keyboard-driven checklist: space toggles, enter confirms.
// onDone receives the keys of the checked items.
type multiselect struct {
	title  string
	banner string
	items  []toggle
	footer string
	cursor int
	onDone func(a *App, selected []string) tea.Cmd
}

func (m *multiselect) Title() string { return m.title }

func (m *multiselect) Init(a *App) tea.Cmd { return nil }

func (m *multiselect) selected() []string {
	var out []string
	for _, it := range m.items {
		if it.checked {
			out = append(out, it.key)
		}
	}
	return out
}

func (m *multiselect) Update(a *App, msg tea.Msg) (screen, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch key.String() {
	case "up", "k":
		m.cursor = max(0, m.cursor-1)
	case "down", "j":
		m.cursor = min(len(m.items)-1, m.cursor+1)
	case " ", "x":
		m.items[m.cursor].checked = !m.items[m.cursor].checked
	case "enter":
		if m.onDone != nil {
			return m, m.onDone(a, m.selected())
		}
	}
	return m, nil
}

func (m *multiselect) View(a *App) string {
	var b strings.Builder
	if m.banner != "" {
		b.WriteString(m.banner + "\n\n")
	}
	group := ""
	for i, it := range m.items {
		if it.group != group {
			group = it.group
			fmt.Fprintf(&b, "  %s\n", headingStyle.Render(group))
		}
		box := dimStyle.Render("[ ]")
		if it.checked {
			box = okStyle.Render("[x]")
		}
		label := it.label
		cursor := "  "
		if i == m.cursor {
			cursor = accentStyle.Render("▸ ")
			label = labelStyle.Render(label)
		}
		fmt.Fprintf(&b, "  %s%s %s", cursor, box, label)
		if it.desc != "" {
			fmt.Fprintf(&b, "  %s", dimStyle.Render("· "+it.desc))
		}
		b.WriteString("\n")
	}
	return b.String()
}

func (m *multiselect) Footer(a *App) string {
	if m.footer != "" {
		return m.footer
	}
	return "↑/↓ move · space alternar · enter continuar · esc atrás"
}
