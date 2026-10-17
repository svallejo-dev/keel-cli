package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newAuditCmd() *cobra.Command {
	var limit int
	cmd := &cobra.Command{
		Use:   "audit",
		Short: "Show the append-only audit log of lifecycle actions",
		RunE: func(cmd *cobra.Command, _ []string) error {
			mgr, err := buildManager()
			if err != nil {
				return exitErr(err)
			}
			recs, err := mgr.Store().ReadAudit(limit)
			if err != nil {
				return exitErr(err)
			}
			if len(recs) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), dim("no audit records yet"))
				return nil
			}
			tw := newTabWriter(cmd.OutOrStdout())
			fmt.Fprintln(tw, heading("TIME\tACTION\tTARGET\tKIND\tRESULT\tDETAIL"))
			for _, r := range recs {
				result := styleOK.Render(r.Result)
				detail := ""
				if r.Result != "ok" {
					result = styleBad.Render(r.Result)
					detail = r.Error
				}
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n",
					r.Time.Format("2006-01-02 15:04:05"), r.Action, r.Target, r.Kind, result, dim(detail))
			}
			tw.Flush()
			return nil
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 50, "max records to show (0 = all)")
	return cmd
}
