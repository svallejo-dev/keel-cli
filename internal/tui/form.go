package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/svallejo-dev/keel-cli/internal/domain"
)

// form collects field values for an action, then runs it (or confirms first).
type form struct {
	inst   *domain.Instance
	act    action
	inputs []*fieldInput
	focus  int
	err    string
	init   bool
}

type fieldInput struct {
	f         field
	ti        textinput.Model
	ta        textarea.Model
	multiline bool
}

func newForm(inst *domain.Instance, act action) *form {
	return &form{inst: inst, act: act}
}

func (f *form) Title() string { return f.act.label }

func (f *form) Init(a *App) tea.Cmd { return textinput.Blink }

func (f *form) ensure(a *App) {
	if f.init {
		return
	}
	for _, fl := range f.act.fields {
		fi := &fieldInput{f: fl, multiline: fl.multiline}
		if fl.multiline {
			ta := textarea.New()
			ta.Placeholder = fl.placeholder
			ta.SetValue(fl.value)
			ta.SetWidth(a.width - 4)
			ta.SetHeight(4)
			fi.ta = ta
		} else {
			ti := textinput.New()
			ti.Placeholder = fl.placeholder
			ti.SetValue(fl.value)
			ti.Width = a.width - 20
			fi.ti = ti
		}
		f.inputs = append(f.inputs, fi)
	}
	f.init = true
	f.setFocus(0)
}

func (f *form) setFocus(i int) {
	if i < 0 {
		i = 0
	}
	if i >= len(f.inputs) {
		i = len(f.inputs) - 1
	}
	f.focus = i
	for j, fi := range f.inputs {
		if fi.multiline {
			if j == i {
				fi.ta.Focus()
			} else {
				fi.ta.Blur()
			}
		} else {
			if j == i {
				fi.ti.Focus()
			} else {
				fi.ti.Blur()
			}
		}
	}
}

func (f *form) values() map[string]string {
	m := map[string]string{}
	for _, fi := range f.inputs {
		if fi.multiline {
			m[fi.f.key] = fi.ta.Value()
		} else {
			m[fi.f.key] = fi.ti.Value()
		}
	}
	return m
}

func (f *form) submit(a *App) (screen, tea.Cmd) {
	vals := f.values()
	for _, fi := range f.inputs {
		if fi.f.required && strings.TrimSpace(vals[fi.f.key]) == "" {
			f.err = fi.f.label + " is required"
			return f, nil
		}
	}
	if f.act.danger {
		return f, push(newConfirm(f.inst, f.act, vals, 2)) // pop confirm + form
	}
	return f, runActionCmd(a, f.inst, f.act, vals, 1) // pop form
}

func (f *form) Update(a *App, msg tea.Msg) (screen, tea.Cmd) {
	f.ensure(a)
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "ctrl+s":
			return f.submit(a)
		case "tab", "down":
			f.setFocus(f.focus + 1)
			return f, nil
		case "shift+tab", "up":
			f.setFocus(f.focus - 1)
			return f, nil
		case "enter":
			// single-line fields submit on enter; textarea keeps enter for newlines
			if !f.inputs[f.focus].multiline {
				if f.focus < len(f.inputs)-1 {
					f.setFocus(f.focus + 1)
					return f, nil
				}
				return f.submit(a)
			}
		}
	}
	// route to the focused input
	var cmd tea.Cmd
	fi := f.inputs[f.focus]
	if fi.multiline {
		fi.ta, cmd = fi.ta.Update(msg)
	} else {
		fi.ti, cmd = fi.ti.Update(msg)
	}
	return f, cmd
}

func (f *form) View(a *App) string {
	f.ensure(a)

	// Render each field as its own block, then show a window around the focused
	// one: engines like DynamoDB declare many params and would overflow the screen.
	blocks := make([]string, len(f.inputs))
	for i, fi := range f.inputs {
		var b strings.Builder
		marker := "  "
		if i == f.focus {
			marker = accentStyle.Render("▶ ")
		}
		req := ""
		if fi.f.required {
			req = badStyle.Render("*")
		}
		fmt.Fprintf(&b, "%s%s%s\n", marker, labelStyle.Render(fi.f.label), req)
		if fi.multiline {
			fmt.Fprintln(&b, fi.ta.View())
		} else {
			fmt.Fprintf(&b, "  %s\n", fi.ti.View())
		}
		blocks[i] = b.String()
	}

	header := "  " + dimStyle.Render("Fill the fields, then Ctrl+S to submit")
	budget := a.bodyHeight() - 2 // header + error/hint line
	if f.err != "" {
		budget--
	}
	first, last := visibleWindow(blocks, f.focus, budget)

	var out strings.Builder
	out.WriteString(header + "\n")
	if first > 0 {
		fmt.Fprintf(&out, "  %s\n", dimStyle.Render(fmt.Sprintf("↑ %d more", first)))
	}
	for i := first; i < last; i++ {
		out.WriteString(blocks[i])
	}
	if last < len(blocks) {
		fmt.Fprintf(&out, "  %s\n", dimStyle.Render(fmt.Sprintf("↓ %d more", len(blocks)-last)))
	}
	if f.err != "" {
		fmt.Fprintf(&out, "  %s\n", badStyle.Render(f.err))
	}
	return out.String()
}

// visibleWindow picks the contiguous range of blocks to render so the focused one
// is always on screen and the total height stays within budget.
func visibleWindow(blocks []string, focus, budget int) (first, last int) {
	if budget < 1 {
		budget = 1
	}
	height := func(i int) int { return strings.Count(blocks[i], "\n") }

	first, last = focus, focus+1
	used := height(focus)
	// Grow downwards first (fields below are the ones you tab into next).
	for last < len(blocks) && used+height(last) <= budget {
		used += height(last)
		last++
	}
	for first > 0 && used+height(first-1) <= budget {
		first--
		used += height(first)
	}
	return first, last
}

func (f *form) Footer(a *App) string {
	return "tab/↑↓ move · ctrl+s submit · esc cancel"
}
