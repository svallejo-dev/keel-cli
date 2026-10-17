package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/svallejo-dev/keel-cli/internal/domain"
)

func newSqsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sqs",
		Short: "SQS operations: send, receive, purge, attributes",
	}
	cmd.AddCommand(sqsSendCmd(), sqsReceiveCmd(), sqsPurgeCmd(), sqsAttrsCmd())
	return cmd
}

func sqsSendCmd() *cobra.Command {
	var message string
	cmd := &cobra.Command{
		Use:   "send <instance>",
		Short: "Send a message to the queue",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if message == "" {
				return exitErr(fmt.Errorf("--message is required"))
			}
			mgr, inst, err := opsInstance(args[0], domain.KindSQS)
			if err != nil {
				return exitErr(err)
			}
			id, err := mgr.Ops().SQS.Send(cmd.Context(), inst, message)
			if err != nil {
				return exitErr(err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s sent (MessageId %s)\n", styleOK.Render("✓"), id)
			return nil
		},
	}
	cmd.Flags().StringVarP(&message, "message", "m", "", "message body")
	return cmd
}

func sqsReceiveCmd() *cobra.Command {
	var max, wait int
	cmd := &cobra.Command{
		Use:   "receive <instance>",
		Short: "Receive messages from the queue",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mgr, inst, err := opsInstance(args[0], domain.KindSQS)
			if err != nil {
				return exitErr(err)
			}
			msgs, err := mgr.Ops().SQS.Receive(cmd.Context(), inst, max, wait)
			if err != nil {
				return exitErr(err)
			}
			if len(msgs) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), dim("no messages"))
				return nil
			}
			for _, mmsg := range msgs {
				fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", dim(mmsg.ID+":"), mmsg.Body)
			}
			return nil
		},
	}
	cmd.Flags().IntVarP(&max, "max", "n", 1, "max messages to receive")
	cmd.Flags().IntVar(&wait, "wait", 0, "long-poll wait seconds")
	return cmd
}

func sqsPurgeCmd() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "purge <instance>",
		Short: "Delete all messages in the queue",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mgr, inst, err := opsInstance(args[0], domain.KindSQS)
			if err != nil {
				return exitErr(err)
			}
			if !yes && !confirm("purge all messages in "+args[0]+"?") {
				return nil
			}
			if err := mgr.Ops().SQS.Purge(cmd.Context(), inst); err != nil {
				return exitErr(err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s purged\n", styleOK.Render("✓"))
			return nil
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "skip confirmation")
	return cmd
}

func sqsAttrsCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "attrs <instance>",
		Aliases: []string{"attributes"},
		Short:   "Show queue attributes",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mgr, inst, err := opsInstance(args[0], domain.KindSQS)
			if err != nil {
				return exitErr(err)
			}
			attrs, err := mgr.Ops().SQS.Attributes(cmd.Context(), inst)
			if err != nil {
				return exitErr(err)
			}
			tw := newTabWriter(cmd.OutOrStdout())
			for _, k := range sortedKeys(attrs) {
				fmt.Fprintf(tw, "%s\t%s\n", k, attrs[k])
			}
			tw.Flush()
			return nil
		},
	}
}
