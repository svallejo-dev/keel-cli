package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/svallejo-dev/keel-cli/internal/domain"
)

// detail shows the full information for one instance.
type detail struct {
	inst *domain.Instance
	vp   viewport.Model
	init bool
}

func newDetail(inst *domain.Instance) *detail { return &detail{inst: inst} }

func (d *detail) Title() string { return d.inst.Name }

func (d *detail) Init(a *App) tea.Cmd { return nil }

func (d *detail) ensure(a *App) {
	if !d.init {
		d.vp = viewport.New(a.width, a.bodyHeight())
		d.init = true
	}
	d.vp.Width = a.width
	d.vp.Height = a.bodyHeight()
	d.vp.SetContent(d.content(a))
}

func (d *detail) content(a *App) string {
	i := d.inst
	var b strings.Builder
	line := func(k, v string) { fmt.Fprintf(&b, "  %s %s\n", labelStyle.Render(fmt.Sprintf("%-12s", k)), v) }

	fmt.Fprintf(&b, "%s  %s\n\n", labelStyle.Render(i.Name), dimStyle.Render(i.CloudService))
	line("kind", string(i.Kind))
	line("provider", string(i.Provider))
	line("status", string(i.Status))

	fmt.Fprintln(&b, "\n"+headingStyle.Render("  Endpoints"))
	for _, r := range sortedRoles(i) {
		ep := i.Endpoints[r]
		s := fmt.Sprintf("host %s:%d", ep.Host, ep.Port)
		if ep.DockerHost != "" {
			s += dimStyle.Render(fmt.Sprintf("   docker %s:%d", ep.DockerHost, ep.DockerPort))
		}
		line(r.String(), s)
	}
	if i.Cred.Username != "" || i.Cred.Password != "" {
		fmt.Fprintln(&b, "\n"+headingStyle.Render("  Credentials"))
		if i.Cred.Username != "" {
			line("user", i.Cred.Username)
		}
		if i.Cred.Password != "" {
			line("password", i.Cred.Password)
		}
		if db := i.Cred.Extra["dbname"]; db != "" {
			line("dbname", db)
		}
	}
	if len(i.Resources) > 0 {
		fmt.Fprintln(&b, "\n"+headingStyle.Render("  Resources"))
		for _, k := range sortedMapKeys(i.Resources) {
			line(k, i.Resources[k])
		}
	}
	if len(i.Params) > 0 {
		fmt.Fprintln(&b, "\n"+headingStyle.Render("  Params"))
		for _, k := range sortedMapKeys(i.Params) {
			line(k, i.Params[k])
		}
	}
	if i.Notes != "" {
		fmt.Fprintln(&b, "\n"+headingStyle.Render("  Notes"))
		fmt.Fprintf(&b, "  %s\n", i.Notes)
	}
	return b.String()
}

func (d *detail) Update(a *App, msg tea.Msg) (screen, tea.Cmd) {
	d.ensure(a)
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "enter", "o":
			return d, push(newMenu(d.inst))
		case "i":
			return d, push(newInject(d.inst))
		case "l":
			return d, push(newLogs(d.inst))
		}
	}
	var cmd tea.Cmd
	d.vp, cmd = d.vp.Update(msg)
	return d, cmd
}

func (d *detail) View(a *App) string {
	d.ensure(a)
	return d.vp.View()
}

func (d *detail) Footer(a *App) string {
	return "enter/o operations · i inject · l logs · ↑/↓ scroll · esc back"
}

// --- helpers ---

func sortedRoles(i *domain.Instance) []domain.Role {
	roles := make([]domain.Role, 0, len(i.Endpoints))
	for r := range i.Endpoints {
		roles = append(roles, r)
	}
	sort.Slice(roles, func(a, b int) bool { return roles[a] < roles[b] })
	return roles
}

func sortedMapKeys(m map[string]string) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}
