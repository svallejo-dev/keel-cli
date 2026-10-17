// Package tui is the interactive, multi-view dashboard. It is a thin presentation
// layer over control.Manager + ops: every action it performs maps to the same
// operations the CLI exposes, so the two interfaces stay at parity.
//
// Navigation is a stack of screens. The root App routes global keys and async
// results; each screen owns its own widgets and state.
package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/svallejo-dev/keel-cli/internal/config"
	"github.com/svallejo-dev/keel-cli/internal/control"
)

// Run starts the dashboard and blocks until the user quits.
func Run(ctx context.Context, mgr *control.Manager) error {
	return run(ctx, mgr, newDashboard())
}

// RunHome starts the interactive home launcher (the front door of Keel).
func RunHome(ctx context.Context, mgr *control.Manager) error {
	return run(ctx, mgr, newHome())
}

func run(ctx context.Context, mgr *control.Manager, root screen) error {
	p := tea.NewProgram(newApp(ctx, mgr, root), tea.WithAltScreen(), tea.WithContext(ctx))
	_, err := p.Run()
	return err
}

// App is the root model holding the navigation stack and shared state.
type App struct {
	ctx   context.Context
	mgr   *control.Manager
	stack []screen

	width, height int

	insts   []control.InstanceHealth // shared cache (dashboard + detail)
	loaded  bool
	toast   string
	toastOK bool

	engine config.Engine // resolved once at startup (avoids `docker info` per render)
	spin   spinner.Model
	busy   int // number of in-flight async actions
}

func newApp(ctx context.Context, mgr *control.Manager, root screen) *App {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = spinnerStyle
	a := &App{ctx: ctx, mgr: mgr, spin: sp}
	a.engine = mgr.Config().ResolveEngine() // resolve engine label once, not per frame
	a.push(root)
	return a
}

func (a *App) Init() tea.Cmd {
	// The spinner only animates while an action is in flight (see runStart), so we
	// don't kick off spinner.Tick here — idle frames stay cheap.
	return tea.Batch(a.refreshCmd(), tickEvery())
}

// --- navigation ---

func (a *App) push(s screen)       { a.stack = append(a.stack, s) }
func (a *App) top() screen         { return a.stack[len(a.stack)-1] }
func (a *App) replaceTop(s screen) { a.stack[len(a.stack)-1] = s }
func (a *App) pop() {
	if len(a.stack) > 1 {
		a.stack = a.stack[:len(a.stack)-1]
	}
}

// bodyHeight is the room available to a screen between header and footer.
func (a *App) bodyHeight() int {
	h := a.height - 4 // header (2) + footer (2)
	if h < 3 {
		h = 3
	}
	return h
}

// setToast records a transient status line.
func (a *App) setToast(msg string, ok bool) { a.toast, a.toastOK = msg, ok }

func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width, a.height = msg.Width, msg.Height
		// let the current screen re-lay out
		s, cmd := a.top().Update(a, msg)
		a.replaceTop(s)
		return a, cmd

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			return a, tea.Quit
		case "q":
			// q quits only from the root screen; deeper screens use esc to go back
			// (and text inputs keep 'q' as a normal character).
			if len(a.stack) == 1 {
				return a, tea.Quit
			}
		case "esc":
			if len(a.stack) > 1 {
				a.pop()
				return a, nil
			}
			return a, tea.Quit
		}
		s, cmd := a.top().Update(a, msg)
		a.replaceTop(s)
		return a, cmd

	case navPush:
		a.push(msg.s)
		return a, msg.s.Init(a)
	case navPop:
		a.pop()
		if msg.toast != "" {
			a.setToast(msg.toast, !msg.isErr)
		}
		return a, msg.reload
	case navReplace:
		a.replaceTop(msg.s)
		return a, msg.s.Init(a)

	case instancesMsg:
		a.insts = msg
		a.loaded = true
		s, cmd := a.top().Update(a, msg) // let the dashboard rebuild its rows
		a.replaceTop(s)
		return a, cmd
	case toastMsg:
		a.setToast(msg.text, msg.ok)
		return a, nil
	case tickMsg:
		return a, tea.Batch(a.refreshCmd(), tickEvery())

	case runStart:
		a.busy++
		if a.busy == 1 {
			return a, a.spin.Tick // start animating only when work begins
		}
		return a, nil
	case runResult:
		if a.busy > 0 {
			a.busy--
		}
		return a, a.handleResult(msg)

	case spinner.TickMsg:
		if a.busy == 0 {
			return a, nil // stop ticking once idle
		}
		var cmd tea.Cmd
		a.spin, cmd = a.spin.Update(msg)
		return a, cmd
	}

	// delegate everything else to the active screen
	s, cmd := a.top().Update(a, msg)
	a.replaceTop(s)
	return a, cmd
}

// handleResult centralizes what to do after an async action completes: show its
// output, toast, and/or refresh.
func (a *App) handleResult(r runResult) tea.Cmd {
	if r.err != nil {
		// Errors are often long (SDK/HTTP details); show them in a scrollable,
		// word-wrapped screen so nothing is cut off — not a one-line toast.
		a.setToast("✗ "+r.label+" falló — esc para volver", false)
		a.push(newOutput(r.label+" — error", result{text: "✗ " + r.label + "\n\n" + r.err.Error()}))
		return a.top().Init(a)
	}
	var cmds []tea.Cmd
	if r.res.refresh {
		cmds = append(cmds, a.refreshCmd())
	}
	if r.popTo > 0 {
		for i := 0; i < r.popTo && len(a.stack) > 1; i++ {
			a.pop()
		}
	}
	if r.res.text != "" || r.res.table != nil {
		a.push(newOutput(r.label, r.res))
		cmds = append(cmds, a.top().Init(a))
	} else if r.res.toast != "" {
		a.setToast("✓ "+r.res.toast, true)
	} else {
		a.setToast("✓ "+r.label, true)
	}
	return tea.Batch(cmds...)
}

func (a *App) View() string {
	if a.width == 0 {
		return "loading…"
	}
	header := a.renderHeader()
	body := a.top().View(a)
	footer := a.renderFooter()
	return lipgloss.JoinVertical(lipgloss.Left, header, body, footer)
}

func (a *App) renderHeader() string {
	left := titleStyle.Render(" ⚓ keel ")
	crumbs := make([]string, 0, len(a.stack))
	for _, s := range a.stack {
		crumbs = append(crumbs, s.Title())
	}
	path := "  " + headingStyle.Render(strings.Join(crumbs, dimStyle.Render(" › ")))

	// Engine status dot: green when real, amber when mock/simulated.
	dot := okStyle.Render("●")
	if a.engine != config.EngineReal {
		dot = warnStyle.Render("●")
	}
	meta := dot + dimStyle.Render(fmt.Sprintf(" %s · %d instancias ", a.engine, len(a.insts)))

	gap := a.width - lipgloss.Width(left) - lipgloss.Width(path) - lipgloss.Width(meta)
	if gap < 1 {
		gap = 1
	}
	return left + path + strings.Repeat(" ", gap) + meta + "\n"
}

func (a *App) renderFooter() string {
	status := ""
	if a.busy > 0 {
		status = a.spin.View() + " working… "
	} else if a.toast != "" {
		// Keep the toast to a single line; full detail lives in the output screen.
		t := truncate(a.toast, a.width-1)
		if a.toastOK {
			status = okStyle.Render(t)
		} else {
			status = badStyle.Render(t)
		}
	}
	help := a.top().Footer(a)
	line := helpStyle.Render(help)
	return "\n" + status + "\n" + line
}

// --- shared commands ---

func (a *App) refreshCmd() tea.Cmd {
	return func() tea.Msg {
		return instancesMsg(a.mgr.HealthAll(a.ctx))
	}
}

func tickEvery() tea.Cmd {
	return tea.Tick(6*time.Second, func(time.Time) tea.Msg { return tickMsg{} })
}
