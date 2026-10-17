package tui

import (
	"context"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/svallejo-dev/keel-cli/internal/catalog"
	"github.com/svallejo-dev/keel-cli/internal/control"
	"github.com/svallejo-dev/keel-cli/internal/domain"
)

// create is the new-instance wizard: pick a blueprint, then fill a form.
type create struct {
	list list.Model
	init bool
}

func newCreate() *create { return &create{} }

func (c *create) Title() string { return "New instance" }

func (c *create) Init(a *App) tea.Cmd { return nil }

type kindItem struct{ bp domain.Blueprint }

func (k kindItem) Title() string       { return string(k.bp.Kind) }
func (k kindItem) Description() string { return k.bp.CloudService }
func (k kindItem) FilterValue() string { return string(k.bp.Kind) + " " + k.bp.CloudService }

func (c *create) ensure(a *App) {
	if c.init {
		c.list.SetSize(a.width, a.bodyHeight())
		return
	}
	var items []list.Item
	for _, bp := range catalog.Active() {
		items = append(items, kindItem{bp})
	}
	l := list.New(items, list.NewDefaultDelegate(), a.width, a.bodyHeight())
	l.Title = "Choose a blueprint"
	l.SetShowTitle(false)
	l.SetShowStatusBar(false)
	l.SetShowHelp(false)
	c.list = l
	c.init = true
}

func (c *create) Update(a *App, msg tea.Msg) (screen, tea.Cmd) {
	c.ensure(a)
	if key, ok := msg.(tea.KeyMsg); ok && key.String() == "enter" {
		if it, ok := c.list.SelectedItem().(kindItem); ok {
			placeholder := &domain.Instance{Kind: it.bp.Kind}
			return c, push(newForm(placeholder, createAction(it.bp)))
		}
	}
	var cmd tea.Cmd
	c.list, cmd = c.list.Update(msg)
	return c, cmd
}

func (c *create) View(a *App) string { c.ensure(a); return c.list.View() }

func (c *create) Footer(a *App) string {
	return "↑/↓ move · enter choose · / filter · esc cancel"
}

// createAction builds the action that provisions a new instance of a blueprint.
// Its form is generated from the blueprint's declared params (catalog is the
// single source of truth), so every engine — and every new param — is
// configurable from the TUI without touching this code.
func createAction(bp domain.Blueprint) action {
	fields := []field{
		{key: "name", label: "Instance name", required: true},
		{key: "provider", label: "Provider override", placeholder: "blank = default (" + string(bp.Provider) + ")"},
	}

	params := catalog.ParamsFor(bp.Kind, bp.Provider)
	// Required params first, then optional ones.
	ordered := make([]domain.Param, 0, len(params))
	for _, p := range params {
		if p.Required {
			ordered = append(ordered, p)
		}
	}
	for _, p := range params {
		if !p.Required {
			ordered = append(ordered, p)
		}
	}
	for _, p := range ordered {
		fields = append(fields, field{
			key:         p.Key,
			label:       p.Label,
			placeholder: paramHint(p),
			required:    p.Required,
			multiline:   p.Multiline,
		})
	}

	return action{
		id:     "create",
		label:  "Create " + string(bp.Kind),
		fields: fields,
		run: func(ctx context.Context, m *control.Manager, _ *domain.Instance, in map[string]string) (result, error) {
			// Forward every declared param the user filled in.
			vals := map[string]string{}
			for _, p := range params {
				v := in[p.Key]
				if !p.Multiline {
					v = strings.TrimSpace(v)
				}
				if v != "" {
					vals[p.Key] = v
				}
			}
			name := strings.TrimSpace(in["name"])
			_, err := m.Create(ctx, bp.Kind, name, vals, nil, domain.ProviderName(strings.TrimSpace(in["provider"])))
			if err != nil {
				return result{}, err
			}
			return result{toast: "created " + name, refresh: true}, nil
		},
	}
}

// paramHint renders a param's default/allowed values/help as the input's
// placeholder, so the form documents itself.
func paramHint(p domain.Param) string {
	var parts []string
	if p.Default != "" {
		parts = append(parts, "default: "+p.Default)
	}
	if len(p.Enum) > 0 {
		parts = append(parts, strings.Join(p.Enum, "|"))
	}
	if p.Help != "" {
		parts = append(parts, p.Help)
	}
	if len(parts) == 0 {
		return "optional"
	}
	return strings.Join(parts, " · ")
}
