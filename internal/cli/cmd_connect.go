package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/svallejo-dev/keel-cli/internal/domain"
	"github.com/svallejo-dev/keel-cli/internal/inject"
)

func newConnectCmd() *cobra.Command {
	var mode string
	cmd := &cobra.Command{
		Use:   "connect <name>",
		Short: "Show ready-to-use connection details for an instance",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mgr, err := buildManager()
			if err != nil {
				return exitErr(err)
			}
			inst, err := mgr.Get(args[0])
			if err != nil {
				return exitErr(err)
			}
			out := cmd.OutOrStdout()
			printInstanceSummary(out, inst)
			fmt.Fprintln(out)
			fmt.Fprintln(out, heading("Environment variables:"))
			bundle := inject.Build([]*domain.Instance{inst}, mgr.Config(), inject.Mode(mode))
			s, err := bundle.Render(inject.FormatDotenv)
			if err != nil {
				return exitErr(err)
			}
			fmt.Fprint(out, s)
			return nil
		},
	}
	cmd.Flags().StringVar(&mode, "mode", string(inject.ModeHost), "address mode: host|docker")
	return cmd
}

func newInjectCmd() *cobra.Command {
	var (
		format string
		mode   string
		out    string
		all    bool
	)
	cmd := &cobra.Command{
		Use:   "inject [name...]",
		Short: "Emit connection config for a consuming repo (implements the injection pattern)",
		Long: `Inject renders resolved connection configuration (endpoints + secrets) that a
consuming application injects at bootstrap — the platform's implementation of
CONNECTION_INJECTION_PATTERN.md.

Formats: dotenv (KEY=VALUE), env (export KEY=VALUE), json, yaml.
Modes:   host (localhost:mappedPort) for host apps, docker (network hostname) for
         containerized apps.

Examples:
  keel inject orders-db session-cache --format dotenv --out .env
  keel inject --all --format yaml --mode docker --out infra.yaml`,
		RunE: func(cmd *cobra.Command, args []string) error {
			mgr, err := buildManager()
			if err != nil {
				return exitErr(err)
			}
			var insts []*domain.Instance
			if all || len(args) == 0 {
				insts = mgr.List()
			} else {
				for _, name := range args {
					inst, err := mgr.Get(name)
					if err != nil {
						return exitErr(err)
					}
					insts = append(insts, inst)
				}
			}
			if len(insts) == 0 {
				return exitErr(fmt.Errorf("no instances to inject"))
			}

			bundle := inject.Build(insts, mgr.Config(), inject.Mode(mode))
			rendered, err := bundle.Render(inject.Format(format))
			if err != nil {
				return exitErr(err)
			}
			if out == "" || out == "-" {
				fmt.Fprint(cmd.OutOrStdout(), rendered)
			} else {
				if err := os.WriteFile(out, []byte(rendered), 0o600); err != nil {
					return exitErr(fmt.Errorf("write %s: %w", out, err))
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s wrote %d var(s) for %d instance(s) to %s\n",
					styleOK.Render("✓"), len(bundle.Vars), len(insts), bold(out))
			}
			// Audit the injection (config was materialized to disk/stdout).
			mgr.Store().AuditOK("inject", out, "", string(mgr.Config().Engine), map[string]string{"format": format, "mode": mode})
			return nil
		},
	}
	cmd.Flags().StringVar(&format, "format", string(inject.FormatDotenv), "output format: dotenv|env|json|yaml")
	cmd.Flags().StringVar(&mode, "mode", string(inject.ModeHost), "address mode: host|docker")
	cmd.Flags().StringVarP(&out, "out", "o", "", "output file (default: stdout)")
	cmd.Flags().BoolVar(&all, "all", false, "inject every instance")
	return cmd
}
