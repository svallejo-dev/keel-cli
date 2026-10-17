package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newHealthCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "health [name...]",
		Short: "Probe the health of every component, or the named one(s)",
		RunE: func(cmd *cobra.Command, args []string) error {
			mgr, err := buildManager()
			if err != nil {
				return exitErr(err)
			}
			results := mgr.HealthAll(cmd.Context())
			if len(args) > 0 {
				want := map[string]bool{}
				for _, a := range args {
					want[a] = true
				}
				filtered := results[:0]
				for _, r := range results {
					if want[r.Instance.Name] {
						filtered = append(filtered, r)
					}
				}
				results = filtered
			}
			if len(results) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), dim("no instances to check"))
				return nil
			}

			tw := newTabWriter(cmd.OutOrStdout())
			fmt.Fprintln(tw, heading("NAME\tKIND\tHEALTH\tLATENCY\tDETAIL"))
			unhealthy := 0
			for _, r := range results {
				if !r.Result.OK {
					unhealthy++
				}
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
					r.Instance.Name, r.Instance.Kind, healthText(r.Result.OK),
					r.Result.Latency.Round(1e6), dim(r.Result.Detail))
			}
			tw.Flush()
			if unhealthy > 0 {
				return fmt.Errorf("%d of %d instance(s) unhealthy", unhealthy, len(results))
			}
			return nil
		},
	}
	return cmd
}
