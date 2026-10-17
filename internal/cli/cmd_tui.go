package cli

import (
	"github.com/spf13/cobra"

	"github.com/svallejo-dev/keel-cli/internal/tui"
)

func newTUICmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "tui",
		Aliases: []string{"dashboard", "status"},
		Short:   "Open the interactive dashboard (all components in one integrated view)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			// Quiet: progress writes to stderr would garble the alt-screen; the
			// TUI header already shows the engine.
			mgr, err := buildManagerQuiet()
			if err != nil {
				return exitErr(err)
			}
			return tui.Run(cmd.Context(), mgr)
		},
	}
	return cmd
}
