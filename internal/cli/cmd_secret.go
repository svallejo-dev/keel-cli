package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/svallejo-dev/keel-cli/internal/domain"
)

func newSecretCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "secret",
		Short: "Secrets Manager operations: get/set/describe",
	}
	cmd.AddCommand(secretGetCmd(), secretSetCmd(), secretDescribeCmd())
	return cmd
}

func secretGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <instance>",
		Short: "Get the secret value",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mgr, inst, err := opsInstance(args[0], domain.KindSecrets)
			if err != nil {
				return exitErr(err)
			}
			v, err := mgr.Ops().Secrets.Get(cmd.Context(), inst)
			if err != nil {
				return exitErr(err)
			}
			fmt.Fprintln(cmd.OutOrStdout(), v)
			return nil
		},
	}
}

func secretSetCmd() *cobra.Command {
	var value, file string
	cmd := &cobra.Command{
		Use:   "set <instance>",
		Short: "Set the secret value (from --value or --file)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mgr, inst, err := opsInstance(args[0], domain.KindSecrets)
			if err != nil {
				return exitErr(err)
			}
			var val string
			switch {
			case file != "":
				b, err := os.ReadFile(file)
				if err != nil {
					return exitErr(err)
				}
				val = string(b)
			case value != "":
				val = value
			default:
				return exitErr(fmt.Errorf("provide --value or --file"))
			}
			if err := mgr.Ops().Secrets.Set(cmd.Context(), inst, val); err != nil {
				return exitErr(err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s secret updated\n", styleOK.Render("✓"))
			return nil
		},
	}
	cmd.Flags().StringVar(&value, "value", "", "inline secret value")
	cmd.Flags().StringVar(&file, "file", "", "read secret value from a file")
	return cmd
}

func secretDescribeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "describe <instance>",
		Short: "Show secret metadata",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mgr, inst, err := opsInstance(args[0], domain.KindSecrets)
			if err != nil {
				return exitErr(err)
			}
			m, err := mgr.Ops().Secrets.Describe(cmd.Context(), inst)
			if err != nil {
				return exitErr(err)
			}
			tw := newTabWriter(cmd.OutOrStdout())
			for _, k := range sortedKeys(m) {
				fmt.Fprintf(tw, "%s\t%s\n", k, m[k])
			}
			tw.Flush()
			return nil
		},
	}
}
