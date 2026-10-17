package cli

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/svallejo-dev/keel-cli/internal/configstore"
	"github.com/svallejo-dev/keel-cli/internal/domain"
)

func newVaultCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "vault",
		Short: "Vault operations: status, kv list/get/put/delete",
	}
	kv := &cobra.Command{
		Use:   "kv",
		Short: "KV v2 secrets of a vault instance",
	}
	kv.AddCommand(vaultKVListCmd(), vaultKVGetCmd(), vaultKVPutCmd(), vaultKVDeleteCmd())
	cmd.AddCommand(vaultStatusCmd(), kv)
	return cmd
}

func vaultStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status <instance>",
		Short: "Show whether Vault is initialized and unsealed",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mgr, inst, err := opsInstance(args[0], domain.KindVault)
			if err != nil {
				return exitErr(err)
			}
			st, err := mgr.Ops().Vault.Status(cmd.Context(), inst)
			if err != nil {
				return exitErr(err)
			}
			tw := newTabWriter(cmd.OutOrStdout())
			fmt.Fprintf(tw, "version\t%s\n", st.Version)
			fmt.Fprintf(tw, "initialized\t%t\n", st.Initialized)
			fmt.Fprintf(tw, "sealed\t%t\n", st.Sealed)
			fmt.Fprintf(tw, "kv mount\t%s\n", inst.Resources["mount"])
			return tw.Flush()
		},
	}
}

func vaultKVListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list <instance> [prefix]",
		Short: "List secret paths (folders end with /)",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			mgr, inst, err := opsInstance(args[0], domain.KindVault)
			if err != nil {
				return exitErr(err)
			}
			prefix := ""
			if len(args) == 2 {
				prefix = args[1]
			}
			keys, err := mgr.Ops().Vault.List(cmd.Context(), inst, prefix)
			if err != nil {
				return exitErr(err)
			}
			if len(keys) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), dim("no secrets"))
				return nil
			}
			for _, k := range keys {
				fmt.Fprintln(cmd.OutOrStdout(), k)
			}
			return nil
		},
	}
}

func vaultKVGetCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "get <instance> <path>",
		Short: "Show the key/values stored at a path",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			mgr, inst, err := opsInstance(args[0], domain.KindVault)
			if err != nil {
				return exitErr(err)
			}
			data, err := mgr.Ops().Vault.Get(cmd.Context(), inst, args[1])
			if err != nil {
				return exitErr(err)
			}
			if asJSON {
				b, _ := json.MarshalIndent(data, "", "  ")
				fmt.Fprintln(cmd.OutOrStdout(), string(b))
				return nil
			}
			tw := newTabWriter(cmd.OutOrStdout())
			for _, k := range configstore.SortedKeys(data) {
				fmt.Fprintf(tw, "%s\t%s\n", k, data[k])
			}
			return tw.Flush()
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "output as JSON")
	return cmd
}

func vaultKVPutCmd() *cobra.Command {
	var merge bool
	cmd := &cobra.Command{
		Use:   "put <instance> <path> <KEY=VALUE>...",
		Short: "Write key/values at a path (replaces the secret unless --merge)",
		Args:  cobra.MinimumNArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			data, err := parseParams(args[2:])
			if err != nil {
				return exitErr(err)
			}
			mgr, inst, err := opsInstance(args[0], domain.KindVault)
			if err != nil {
				return exitErr(err)
			}
			if err := mgr.Ops().Vault.Put(cmd.Context(), inst, args[1], data, merge); err != nil {
				return exitErr(err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s %s/%s\n", styleOK.Render("✓ written"), inst.Resources["mount"], args[1])
			return nil
		},
	}
	cmd.Flags().BoolVar(&merge, "merge", false, "keep existing keys not given (like `vault kv patch`)")
	return cmd
}

func vaultKVDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <instance> <path>",
		Short: "Permanently delete a secret (all versions)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			mgr, inst, err := opsInstance(args[0], domain.KindVault)
			if err != nil {
				return exitErr(err)
			}
			if err := mgr.Ops().Vault.Delete(cmd.Context(), inst, args[1]); err != nil {
				return exitErr(err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s %s/%s\n", styleOK.Render("✓ deleted"), inst.Resources["mount"], args[1])
			return nil
		},
	}
}
