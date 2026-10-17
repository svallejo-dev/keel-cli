package tui

import (
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
)

// output shows an action's result as scrollable text or a table.
type output struct {
	label string
	res   result
	vp    viewport.Model
	tbl   table.Model
	init  bool
}

func newOutput(label string, res result) *output {
	return &output{label: label, res: res}
}

func (o *output) Title() string { return o.label }

func (o *output) Init(a *App) tea.Cmd { return nil }

func (o *output) content(w int) string {
	if o.res.text == "" {
		return dimStyle.Render("(empty)")
	}
	return wrapText(o.res.text, w)
}

func (o *output) ensure(a *App) {
	if o.init {
		if o.res.table == nil {
			if o.vp.Width != a.width {
				o.vp.Width = a.width
				o.vp.SetContent(o.content(a.width)) // re-wrap to the new width
			}
			o.vp.Height = a.bodyHeight()
		}
		return
	}
	if o.res.table != nil {
		cols := make([]table.Column, len(o.res.table.cols))
		w := a.width / max1(len(o.res.table.cols))
		for i, c := range o.res.table.cols {
			cols[i] = table.Column{Title: c, Width: w - 2}
		}
		rows := make([]table.Row, len(o.res.table.rows))
		for i, r := range o.res.table.rows {
			rows[i] = table.Row(r)
		}
		t := table.New(table.WithColumns(cols), table.WithRows(rows), table.WithFocused(true), table.WithHeight(a.bodyHeight()-1))
		st := table.DefaultStyles()
		st.Header = st.Header.Bold(true).Foreground(lipglossColor6)
		st.Selected = selectedStyle
		t.SetStyles(st)
		o.tbl = t
	} else {
		vp := viewport.New(a.width, a.bodyHeight())
		vp.SetContent(o.content(a.width)) // word-wrap so nothing is cut off
		o.vp = vp
	}
	o.init = true
}

func (o *output) Update(a *App, msg tea.Msg) (screen, tea.Cmd) {
	o.ensure(a)
	var cmd tea.Cmd
	if o.res.table != nil {
		o.tbl, cmd = o.tbl.Update(msg)
	} else {
		o.vp, cmd = o.vp.Update(msg)
	}
	return o, cmd
}

func (o *output) View(a *App) string {
	o.ensure(a)
	if o.res.table != nil {
		return o.tbl.View()
	}
	return o.vp.View()
}

func (o *output) Footer(a *App) string {
	return "↑/↓ scroll · esc back"
}

func max1(n int) int {
	if n < 1 {
		return 1
	}
	return n
}
