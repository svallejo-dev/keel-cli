package cli

import (
	"github.com/spf13/cobra"
)

func newLogsCmd() *cobra.Command {
	var follow bool
	cmd := &cobra.Command{
		Use:   "logs <name>",
		Short: "Show logs for an instance",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mgr, err := buildManager()
			if err != nil {
				return exitErr(err)
			}
			if err := mgr.Logs(cmd.Context(), args[0], follow, cmd.OutOrStdout()); err != nil {
				return exitErr(err)
			}
			return nil
		},
	}
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "follow log output")
	return cmd
}
