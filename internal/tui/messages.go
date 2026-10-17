package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/svallejo-dev/keel-cli/internal/control"
	"github.com/svallejo-dev/keel-cli/internal/domain"
)

// screen is one view in the navigation stack.
type screen interface {
	Init(a *App) tea.Cmd
	Update(a *App, msg tea.Msg) (screen, tea.Cmd)
	View(a *App) string
	Title() string
	Footer(a *App) string
}

// --- navigation messages ---

type navPush struct{ s screen }
type navReplace struct{ s screen }
type navPop struct {
	toast  string
	isErr  bool
	reload tea.Cmd
}

func push(s screen) tea.Cmd    { return func() tea.Msg { return navPush{s} } }
func replace(s screen) tea.Cmd { return func() tea.Msg { return navReplace{s} } }
func popCmd() tea.Cmd          { return func() tea.Msg { return navPop{} } }

// --- data messages ---

type instancesMsg []control.InstanceHealth
type tickMsg struct{}
type toastMsg struct {
	text string
	ok   bool
}

// --- async action plumbing ---

type runStart struct{}
type runResult struct {
	label string
	res   result
	err   error
	popTo int // transient screens (form/confirm) to pop before showing output
}

// tableData is generic tabular output.
type tableData struct {
	cols []string
	rows [][]string
}

// result is what an action returns for display.
type result struct {
	toast   string     // short success line
	text    string     // free text (viewport)
	table   *tableData // tabular output
	refresh bool       // refresh instance list afterwards
}

// field is one input collected by a form before running an action.
type field struct {
	key         string
	label       string
	placeholder string
	value       string
	multiline   bool
	required    bool
}

// action is a single operation the user can run on an instance. The same run
// functions back the CLI, keeping TUI and CLI at parity.
type action struct {
	id     string
	label  string
	danger bool
	fields []field
	run    func(ctx context.Context, mgr *control.Manager, inst *domain.Instance, in map[string]string) (result, error)
}

// runActionCmd executes an action asynchronously.
func runActionCmd(a *App, inst *domain.Instance, act action, in map[string]string, popTo int) tea.Cmd {
	start := func() tea.Msg { return runStart{} }
	do := func() tea.Msg {
		res, err := act.run(a.ctx, a.mgr, inst, in)
		return runResult{label: act.label, res: res, err: err, popTo: popTo}
	}
	return tea.Batch(start, do)
}
