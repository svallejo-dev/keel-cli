package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/svallejo-dev/keel-cli/internal/control"
)

func newUpCmd() *cobra.Command {
	var all bool
	cmd := &cobra.Command{
		Use:   "up [name...]",
		Short: "Start all services, or the named one(s)",
		Long:  "Start instances. With no arguments (or --all) it starts everything.",
		RunE: func(cmd *cobra.Command, args []string) error {
			mgr, err := buildManager()
			if err != nil {
				return exitErr(err)
			}
			names, err := targets(mgr, args, all, "up")
			if err != nil {
				return exitErr(err)
			}
			return runOverInstances(cmd.Context(), cmd, mgr, names, "started", mgr.Up)
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "start every instance")
	return cmd
}

func newDownCmd() *cobra.Command {
	var all bool
	cmd := &cobra.Command{
		Use:   "down [name...]",
		Short: "Stop all services, or the named one(s) (data preserved)",
		RunE: func(cmd *cobra.Command, args []string) error {
			mgr, err := buildManager()
			if err != nil {
				return exitErr(err)
			}
			names, err := targets(mgr, args, all, "down")
			if err != nil {
				return exitErr(err)
			}
			return runOverInstances(cmd.Context(), cmd, mgr, names, "stopped", mgr.Down)
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "stop every instance")
	return cmd
}

func newRestartCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "restart <name...>",
		Short: "Restart the named instance(s)",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mgr, err := buildManager()
			if err != nil {
				return exitErr(err)
			}
			return runOverInstances(cmd.Context(), cmd, mgr, args, "restarted", mgr.Restart)
		},
	}
	return cmd
}

func newDestroyCmd() *cobra.Command {
	var all, purge, yes bool
	cmd := &cobra.Command{
		Use:     "destroy [name...]",
		Aliases: []string{"rm"},
		Short:   "Remove instance(s) and free their ports (use --purge to delete data)",
		RunE: func(cmd *cobra.Command, args []string) error {
			mgr, err := buildManager()
			if err != nil {
				return exitErr(err)
			}
			names, err := targets(mgr, args, all, "destroy")
			if err != nil {
				return exitErr(err)
			}
			if len(names) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), dim("nothing to destroy"))
				return nil
			}
			if !yes {
				action := "destroy"
				if purge {
					action = "destroy and PURGE DATA of"
				}
				if !confirm(fmt.Sprintf("%s %d instance(s): %s?", action, len(names), strings.Join(names, ", "))) {
					fmt.Fprintln(cmd.OutOrStdout(), dim("aborted"))
					return nil
				}
			}
			return runOverInstances(cmd.Context(), cmd, mgr, names, "destroyed", func(ctx context.Context, name string) error {
				return mgr.Destroy(ctx, name, purge)
			})
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "destroy every instance")
	cmd.Flags().BoolVar(&purge, "purge", false, "also delete backing data/volumes")
	cmd.Flags().BoolVar(&yes, "yes", false, "skip confirmation")
	return cmd
}

// targets resolves which instance names an action applies to.
func targets(mgr *control.Manager, args []string, all bool, action string) ([]string, error) {
	if len(args) > 0 && all {
		return nil, fmt.Errorf("cannot combine explicit names with --all")
	}
	if len(args) > 0 {
		return args, nil
	}
	// No names: default to all instances (friendly for `up`/`down`).
	var names []string
	for _, inst := range mgr.List() {
		names = append(names, inst.Name)
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("no instances exist (create one with `keel create <kind> --name <n>`)")
	}
	return names, nil
}

// runOverInstances applies fn to each name, printing per-instance results and
// returning the first error.
func runOverInstances(ctx context.Context, cmd *cobra.Command, _ *control.Manager, names []string, pastTense string, fn func(context.Context, string) error) error {
	var firstErr error
	for _, name := range names {
		if err := fn(ctx, name); err != nil {
			fmt.Fprintf(cmd.OutOrStdout(), "%s %s: %s\n", styleBad.Render("✗"), name, err.Error())
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		fmt.Fprintf(cmd.OutOrStdout(), "%s %s %s\n", styleOK.Render("✓"), name, dim(pastTense))
	}
	return firstErr
}

func confirm(prompt string) bool {
	fmt.Fprintf(os.Stderr, "%s [y/N] ", prompt)
	sc := bufio.NewScanner(os.Stdin)
	if !sc.Scan() {
		return false
	}
	ans := strings.ToLower(strings.TrimSpace(sc.Text()))
	return ans == "y" || ans == "yes"
}
