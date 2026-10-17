package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/svallejo-dev/keel-cli/internal/domain"
)

func newSnsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sns",
		Short: "SNS operations: publish, subscriptions",
	}
	cmd.AddCommand(snsPublishCmd(), snsSubscriptionsCmd())
	return cmd
}

func snsPublishCmd() *cobra.Command {
	var message, subject string
	cmd := &cobra.Command{
		Use:   "publish <instance>",
		Short: "Publish a message to the topic",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if message == "" {
				return exitErr(fmt.Errorf("--message is required"))
			}
			mgr, inst, err := opsInstance(args[0], domain.KindSNS)
			if err != nil {
				return exitErr(err)
			}
			id, err := mgr.Ops().SNS.Publish(cmd.Context(), inst, subject, message)
			if err != nil {
				return exitErr(err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s published (MessageId %s)\n", styleOK.Render("✓"), id)
			return nil
		},
	}
	cmd.Flags().StringVarP(&message, "message", "m", "", "message body")
	cmd.Flags().StringVar(&subject, "subject", "", "message subject (optional)")
	return cmd
}

func snsSubscriptionsCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "subscriptions <instance>",
		Aliases: []string{"subs"},
		Short:   "List topic subscriptions",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mgr, inst, err := opsInstance(args[0], domain.KindSNS)
			if err != nil {
				return exitErr(err)
			}
			subs, err := mgr.Ops().SNS.ListSubscriptions(cmd.Context(), inst)
			if err != nil {
				return exitErr(err)
			}
			if len(subs) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), dim("no subscriptions"))
				return nil
			}
			tw := newTabWriter(cmd.OutOrStdout())
			fmt.Fprintln(tw, heading("PROTOCOL\tENDPOINT\tARN"))
			for _, s := range subs {
				fmt.Fprintf(tw, "%s\t%s\t%s\n", s.Protocol, s.Endpoint, s.Arn)
			}
			tw.Flush()
			return nil
		},
	}
}
