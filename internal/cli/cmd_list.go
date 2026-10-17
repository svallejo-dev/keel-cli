package cli

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
)

func newListCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls", "ps"},
		Short:   "List all provisioned instances with status and endpoints",
		RunE: func(cmd *cobra.Command, _ []string) error {
			mgr, err := buildManager()
			if err != nil {
				return exitErr(err)
			}
			insts := mgr.List()
			if asJSON {
				b, err := json.MarshalIndent(insts, "", "  ")
				if err != nil {
					return exitErr(err)
				}
				fmt.Fprintln(cmd.OutOrStdout(), string(b))
				return nil
			}
			if len(insts) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), dim("no instances — create one with `keel create <kind> --name <n>`"))
				return nil
			}
			tw := newTabWriter(cmd.OutOrStdout())
			fmt.Fprintln(tw, heading("NAME\tKIND\tPROVIDER\tSTATUS\tENDPOINTS\tCLOUD SERVICE"))
			for _, inst := range insts {
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n",
					inst.Name, inst.Kind, inst.Provider, statusText(inst.Status), endpointsString(inst), inst.CloudService)
			}
			tw.Flush()
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "output as JSON")
	return cmd
}
