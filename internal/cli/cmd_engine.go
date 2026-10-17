package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/svallejo-dev/keel-cli/internal/floci"
)

// newEngineCmd groups commands that manage the real substrate (the floci
// emulator): bringing it up from zero, checking it and stopping it.
func newEngineCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "engine",
		Aliases: []string{"floci"},
		Short:   "Manage the local engine (floci emulator): up, status, down",
		Long: `engine manages the substrate that backs real infrastructure: the floci emulator.

` + "`keel engine up`" + ` works from zero — it creates the docker network, pulls the
image if missing, starts the container and waits until the AWS API answers. You do
not have to run it by hand: ` + "`keel create`" + ` brings floci up automatically when needed.`,
	}
	cmd.AddCommand(newEngineUpCmd(), newEngineStatusCmd(), newEngineDownCmd())
	return cmd
}

func newEngineUpCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "up",
		Short: "Start the floci emulator (creates network/image/container as needed)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg := loadConfig()
			out := cmd.OutOrStdout()
			log := func(msg string) { fmt.Fprintln(out, dim("• "+msg)) }

			if err := floci.EnsureUp(cmd.Context(), cfg, log); err != nil {
				return exitErr(err)
			}
			fmt.Fprintln(out, styleOK.Render("✓ engine ready")+"  "+dim(cfg.FlociEndpoint))
			fmt.Fprintln(out, dim("create infrastructure with:  keel create postgres --name my-db"))
			return nil
		},
	}
}

func newEngineStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "status",
		Aliases: []string{"st"},
		Short:   "Show whether Docker and the floci emulator are up",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg := loadConfig()
			st := floci.Probe(cmd.Context(), cfg)
			out := cmd.OutOrStdout()

			tw := newTabWriter(out)
			fmt.Fprintf(tw, "docker\t%s\n", boolText(st.DockerAvailable, "available", "not available"))
			fmt.Fprintf(tw, "container\t%s\n", string(st.Container))
			fmt.Fprintf(tw, "endpoint\t%s\n", st.Endpoint)
			fmt.Fprintf(tw, "reachable\t%s\n", boolText(st.Reachable, "yes", "no"))
			tw.Flush()

			if !st.Reachable {
				fmt.Fprintln(out)
				fmt.Fprintln(out, warn("engine is not reachable — start it with `keel engine up`"))
			}
			return nil
		},
	}
}

func newEngineDownCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "down",
		Short: "Stop the floci emulator (data volume is kept)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()
			if err := floci.Down(cmd.Context(), func(msg string) { fmt.Fprintln(out, dim("• "+msg)) }); err != nil {
				return exitErr(err)
			}
			fmt.Fprintln(out, styleOK.Render("✓ engine stopped"))
			return nil
		},
	}
}

func boolText(ok bool, yes, no string) string {
	if ok {
		return styleOK.Render(yes)
	}
	return styleBad.Render(no)
}
