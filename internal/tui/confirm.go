package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/svallejo-dev/keel-cli/internal/domain"
)

// confirm is a yes/no gate before a destructive action.
type confirm struct {
	inst   *domain.Instance
	act    action
	values map[string]string
	popTo  int
}

func newConfirm(inst *domain.Instance, act action, values map[string]string, popTo int) *confirm {
	return &confirm{inst: inst, act: act, values: values, popTo: popTo}
}

func (c *confirm) Title() string        { return "Confirm" }
func (c *confirm) Init(a *App) tea.Cmd  { return nil }
func (c *confirm) Footer(a *App) string { return "y confirm · n/esc cancel" }

func (c *confirm) Update(a *App, msg tea.Msg) (screen, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "y", "Y":
			return c, runActionCmd(a, c.inst, c.act, c.values, c.popTo)
		case "n", "N":
			return c, popCmd()
		}
	}
	return c, nil
}

func (c *confirm) View(a *App) string {
	msg := fmt.Sprintf("%s  %s  on  %s ?",
		warnStyle.Render("⚠"), badStyle.Render(c.act.label), labelStyle.Render(c.inst.Name))
	return "\n\n  " + msg + "\n\n  " + dimStyle.Render("This cannot be undone.")
}
