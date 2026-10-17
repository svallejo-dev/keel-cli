package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/svallejo-dev/keel-cli/internal/catalog"
)

func newUpdateCmd() *cobra.Command {
	var (
		params  []string
		note    string
		restart bool
	)
	cmd := &cobra.Command{
		Use:   "update <name>",
		Short: "Edit an instance's saved params/notes (optionally restart to apply)",
		Long: `Update edits the stored parameters and notes of an instance. Some engine params
only take effect after a restart or recreate; pass --restart to restart now.

Examples:
  keel update orders-db --note "owned by payments team"
  keel update work-queue --param visibility_timeout=60 --restart`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			pmap, err := parseParams(params)
			if err != nil {
				return exitErr(err)
			}
			if len(pmap) == 0 && note == "" && !restart {
				return exitErr(fmt.Errorf("nothing to update (pass --param, --note or --restart)"))
			}
			mgr, err := buildManager()
			if err != nil {
				return exitErr(err)
			}
			// Validate against the instance's own blueprint (kind known only now).
			if len(pmap) > 0 {
				cur, err := mgr.Get(args[0])
				if err != nil {
					return exitErr(err)
				}
				if err := catalog.ValidateParams(cur.Kind, pmap); err != nil {
					return exitErr(err)
				}
			}
			inst, err := mgr.Update(cmd.Context(), args[0], pmap, note, restart)
			if err != nil {
				return exitErr(err)
			}
			fmt.Fprintln(cmd.OutOrStdout(), styleOK.Render("✓ updated"))
			printInstanceSummary(cmd.OutOrStdout(), inst)
			return nil
		},
	}
	cmd.Flags().StringArrayVar(&params, "param", nil, "set a param key=value (repeatable)")
	cmd.Flags().StringVar(&note, "note", "", "set a free-text note")
	cmd.Flags().BoolVar(&restart, "restart", false, "restart the instance after updating")
	return cmd
}
