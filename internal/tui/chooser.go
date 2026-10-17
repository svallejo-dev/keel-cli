package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// chooserItem is one selectable entry in a chooser menu.
type chooserItem struct {
	icon  string
	title string
	desc  string
	// do returns the command to run when the item is chosen (e.g. push a screen).
	do func(a *App) tea.Cmd
}

// chooser is a reusable, keyboard-driven vertical menu with an optional banner.
// It backs both the home launcher and the new-project preset picker so they look
// and behave identically.
type chooser struct {
	title  string
	banner string // optional multi-line banner rendered above the items
	items  []chooserItem
	footer string
	cursor int
}

func (c *chooser) Title() string { return c.title }

func (c *chooser) Init(a *App) tea.Cmd { return nil }

func (c *chooser) Update(a *App, msg tea.Msg) (screen, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return c, nil
	}
	switch key.String() {
	case "up", "k":
		c.cursor = max(0, c.cursor-1)
	case "down", "j":
		c.cursor = min(len(c.items)-1, c.cursor+1)
	case "home", "g":
		c.cursor = 0
	case "end", "G":
		c.cursor = len(c.items) - 1
	case "enter", "l", "right", " ":
		return c, c.items[c.cursor].do(a)
	default:
		// number shortcuts 1..9 jump-and-select
		if n := int(key.String()[0]) - '1'; len(key.String()) == 1 && n >= 0 && n < len(c.items) {
			c.cursor = n
			return c, c.items[n].do(a)
		}
	}
	return c, nil
}

func (c *chooser) View(a *App) string {
	var b strings.Builder
	if c.banner != "" {
		b.WriteString(c.banner)
		b.WriteString("\n\n")
	}
	for i, it := range c.items {
		num := dimStyle.Render(fmt.Sprintf("%d", i+1))
		head := it.icon + " " + it.title
		if i == c.cursor {
			fmt.Fprintf(&b, " %s %s %s\n", accentStyle.Render("▸"), num, selectedStyle.Render(" "+head+" "))
		} else {
			fmt.Fprintf(&b, "   %s %s\n", num, labelStyle.Render(head))
		}
		if it.desc != "" {
			fmt.Fprintf(&b, "      %s\n", dimStyle.Render(it.desc))
		}
		b.WriteString("\n")
	}
	return b.String()
}

func (c *chooser) Footer(a *App) string {
	if c.footer != "" {
		return c.footer
	}
	return "↑/↓ move · 1–9 quick · enter choose · esc back"
}
