package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/svallejo-dev/keel-cli/internal/domain"
)

// dashboard is the instance list screen.
type dashboard struct {
	tbl   table.Model
	ready bool
}

func newDashboard() *dashboard { return &dashboard{} }

func (d *dashboard) Title() string { return "Dashboard" }

func (d *dashboard) Init(a *App) tea.Cmd { return nil }

func (d *dashboard) ensure(a *App) {
	if d.ready {
		d.tbl.SetWidth(a.width)
		d.tbl.SetHeight(a.bodyHeight() - 1)
		return
	}
	w := a.width
	cols := []table.Column{
		{Title: "NAME", Width: colw(w, 0.20, 12)},
		{Title: "KIND", Width: colw(w, 0.12, 8)},
		{Title: "PROVIDER", Width: colw(w, 0.12, 9)},
		{Title: "STATUS", Width: colw(w, 0.10, 8)},
		{Title: "HEALTH", Width: colw(w, 0.12, 9)},
		{Title: "ENDPOINTS", Width: colw(w, 0.30, 14)},
	}
	t := table.New(table.WithColumns(cols), table.WithFocused(true), table.WithHeight(a.bodyHeight()-1))
	st := table.DefaultStyles()
	st.Header = st.Header.Bold(true).Foreground(lipglossColor6)
	st.Selected = selectedStyle
	t.SetStyles(st)
	d.tbl = t
	d.ready = true
}

func (d *dashboard) rows(a *App) []table.Row {
	rows := make([]table.Row, 0, len(a.insts))
	for _, ih := range a.insts {
		i := ih.Instance
		health := "healthy"
		if i.Status == domain.StatusStopped {
			health = "stopped"
		} else if !ih.Result.OK {
			health = "unhealthy"
		}
		rows = append(rows, table.Row{i.Name, string(i.Kind), string(i.Provider), string(i.Status), health, endpointsStr(i)})
	}
	return rows
}

func (d *dashboard) selected(a *App) *domain.Instance {
	if len(a.insts) == 0 || !d.ready {
		return nil
	}
	row := d.tbl.SelectedRow()
	if row == nil {
		return nil
	}
	for _, ih := range a.insts {
		if ih.Instance.Name == row[0] {
			return ih.Instance
		}
	}
	return nil
}

func (d *dashboard) Update(a *App, msg tea.Msg) (screen, tea.Cmd) {
	d.ensure(a)
	switch msg := msg.(type) {
	case instancesMsg, tea.WindowSizeMsg:
		cur := d.tbl.Cursor() // preserve selection across auto-refresh
		d.ensure(a)
		d.tbl.SetRows(d.rows(a))
		d.tbl.SetCursor(cur)
		return d, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "enter":
			if inst := d.selected(a); inst != nil {
				return d, push(newDetail(inst))
			}
		case "n":
			return d, push(newCreate())
		case "o":
			if inst := d.selected(a); inst != nil {
				return d, push(newMenu(inst))
			}
		case "i":
			if inst := d.selected(a); inst != nil {
				return d, push(newInject(inst))
			}
		case "l":
			if inst := d.selected(a); inst != nil {
				return d, push(newLogs(inst))
			}
		case "u":
			if inst := d.selected(a); inst != nil {
				return d, runActionCmd(a, inst, byID(lifecycleActions(), "start"), nil, 0)
			}
		case "s":
			if inst := d.selected(a); inst != nil {
				return d, runActionCmd(a, inst, byID(lifecycleActions(), "stop"), nil, 0)
			}
		case "R":
			if inst := d.selected(a); inst != nil {
				return d, runActionCmd(a, inst, byID(lifecycleActions(), "restart"), nil, 0)
			}
		case "d":
			if inst := d.selected(a); inst != nil {
				return d, push(newConfirm(inst, byID(lifecycleActions(), "delete"), nil, 1))
			}
		case "r":
			return d, a.refreshCmd()
		}
	}
	var cmd tea.Cmd
	d.tbl, cmd = d.tbl.Update(msg)
	return d, cmd
}

func (d *dashboard) View(a *App) string {
	d.ensure(a)
	if len(a.insts) == 0 {
		return "\n" + dimStyle.Render("  No instances. Press n to create one.")
	}
	return d.tbl.View()
}

func (d *dashboard) Footer(a *App) string {
	return "↑/↓ move · enter detail · o operations · n new · u/s/R start/stop/restart · d delete · i inject · l logs · r refresh · q quit"
}

// --- helpers ---

func byID(acts []action, id string) action {
	for _, a := range acts {
		if a.id == id {
			return a
		}
	}
	return action{}
}

func endpointsStr(i *domain.Instance) string {
	roles := make([]domain.Role, 0, len(i.Endpoints))
	for r := range i.Endpoints {
		roles = append(roles, r)
	}
	sort.Slice(roles, func(a, b int) bool { return roles[a] < roles[b] })
	parts := make([]string, 0, len(roles))
	for _, r := range roles {
		ep := i.Endpoints[r]
		if r == domain.RoleDefault {
			parts = append(parts, fmt.Sprintf("%s:%d", ep.Host, ep.Port))
		} else {
			parts = append(parts, fmt.Sprintf("%s=%s:%d", r, ep.Host, ep.Port))
		}
	}
	return strings.Join(parts, " ")
}

func colw(total int, frac float64, min int) int {
	w := int(float64(total) * frac)
	if w < min {
		return min
	}
	return w
}
