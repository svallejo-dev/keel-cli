package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/svallejo-dev/keel-cli/internal/manifest"
)

func newApplyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "apply <manifest.yaml>",
		Short: "Provision a declarative set of instances from a manifest (idempotent)",
		Long: `Apply reads a YAML manifest describing a set of infra instances and creates any
that don't already exist. Re-running skips existing instances, so it is safe to
apply repeatedly. Manifests are microservice-agnostic templates.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			m, err := manifest.Load(args[0])
			if err != nil {
				return exitErr(err)
			}
			mgr, err := buildManager()
			if err != nil {
				return exitErr(err)
			}
			out := cmd.OutOrStdout()
			if m.Name != "" {
				fmt.Fprintf(out, "%s %s\n", heading("applying stack:"), bold(m.Name))
			}
			st := provisionManifest(cmd.Context(), mgr, m, out, false)
			fmt.Fprintf(out, "\n%s created=%d skipped=%d failed=%d\n", dim("summary:"), st.created, st.skipped, st.failed)
			if st.failed > 0 {
				return fmt.Errorf("%d instance(s) failed", st.failed)
			}
			return nil
		},
	}
	return cmd
}
