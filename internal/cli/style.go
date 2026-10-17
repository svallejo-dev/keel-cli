package cli

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/svallejo-dev/keel-cli/internal/domain"
)

var (
	styleBold    = lipgloss.NewStyle().Bold(true)
	styleDim     = lipgloss.NewStyle().Faint(true)
	styleHeading = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6")) // cyan
	styleOK      = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))            // green
	styleBad     = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))            // red
	styleWarn    = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))            // yellow
	styleInfo    = lipgloss.NewStyle().Foreground(lipgloss.Color("4"))            // blue
)

func bold(s string) string    { return styleBold.Render(s) }
func dim(s string) string     { return styleDim.Render(s) }
func heading(s string) string { return styleHeading.Render(s) }
func warn(s string) string    { return styleWarn.Render("! " + s) }

// statusText renders a colored lifecycle status.
func statusText(s domain.Status) string {
	switch s {
	case domain.StatusRunning:
		return styleOK.Render(string(s))
	case domain.StatusStopped:
		return styleWarn.Render(string(s))
	case domain.StatusError:
		return styleBad.Render(string(s))
	default:
		return styleDim.Render(string(s))
	}
}

// healthText renders a colored health verdict.
func healthText(ok bool) string {
	if ok {
		return styleOK.Render("healthy")
	}
	return styleBad.Render("unhealthy")
}
