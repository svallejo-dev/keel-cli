package tui

import "github.com/charmbracelet/lipgloss"

// Keel palette — nautical teal brand with sky accents. Hex colors downsample
// gracefully to ANSI on limited terminals.
var (
	cBrand  = lipgloss.Color("#5eead4") // teal — the Keel brand color
	cBrandD = lipgloss.Color("#0f766e") // deep teal (badge background)
	cAccent = lipgloss.Color("#38bdf8") // sky — cursors/selection accents
	cInk    = lipgloss.Color("#e6fffb") // near-white ink on dark backgrounds
	cOK     = lipgloss.Color("#4ade80")
	cBad    = lipgloss.Color("#f87171")
	cWarn   = lipgloss.Color("#fbbf24")
	cSelBg  = lipgloss.Color("#0e3a3f") // selected-row background (dark cyan)
)

var (
	titleStyle    = lipgloss.NewStyle().Bold(true).Foreground(cInk).Background(cBrandD)
	headingStyle  = lipgloss.NewStyle().Bold(true).Foreground(cBrand)
	dimStyle      = lipgloss.NewStyle().Faint(true)
	okStyle       = lipgloss.NewStyle().Foreground(cOK)
	badStyle      = lipgloss.NewStyle().Foreground(cBad)
	warnStyle     = lipgloss.NewStyle().Foreground(cWarn)
	accentStyle   = lipgloss.NewStyle().Foreground(cAccent)
	helpStyle     = lipgloss.NewStyle().Faint(true)
	spinnerStyle  = lipgloss.NewStyle().Foreground(cBrand)
	selectedStyle = lipgloss.NewStyle().Bold(true).Foreground(cInk).Background(cSelBg)
	boxStyle      = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(cBrand).Padding(0, 1)
	labelStyle    = lipgloss.NewStyle().Bold(true)

	lipglossColor6 = cBrand
)

func healthText(ok bool) string {
	if ok {
		return okStyle.Render("healthy")
	}
	return badStyle.Render("unhealthy")
}

// wrapText word-wraps s to width w so long content (e.g. error messages) is fully
// readable in a viewport instead of being cut off at the right edge.
func wrapText(s string, w int) string {
	if w < 1 {
		w = 1
	}
	return lipgloss.NewStyle().Width(w).Render(s)
}

// truncate shortens s to at most w cells, adding an ellipsis. Used for the
// single-line footer toast so it never overflows the terminal width.
func truncate(s string, w int) string {
	r := []rune(s)
	if w < 1 || len(r) <= w {
		return s
	}
	if w == 1 {
		return "…"
	}
	return string(r[:w-1]) + "…"
}
