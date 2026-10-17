package cli

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/spf13/cobra"

	"github.com/svallejo-dev/keel-cli/internal/domain"
	"github.com/svallejo-dev/keel-cli/internal/inject"
	"github.com/svallejo-dev/keel-cli/internal/ops"
)

func newDescribeCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:     "describe <name>",
		Aliases: []string{"info", "show"},
		Short:   "Show full detail for an instance (endpoints, credentials, resources, operations)",
		Args:    cobra.ExactArgs(1),
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
			if asJSON {
				b, _ := json.MarshalIndent(inst, "", "  ")
				fmt.Fprintln(out, string(b))
				return nil
			}

			printInstanceSummary(out, inst)

			if len(inst.Resources) > 0 {
				fmt.Fprintln(out, "\n"+heading("Resources:"))
				tw := newTabWriter(out)
				for _, k := range sortedKeys(inst.Resources) {
					fmt.Fprintf(tw, "  %s\t%s\n", k, inst.Resources[k])
				}
				tw.Flush()
			}
			if len(inst.Params) > 0 {
				fmt.Fprintln(out, "\n"+heading("Params:"))
				tw := newTabWriter(out)
				for _, k := range sortedKeys(inst.Params) {
					fmt.Fprintf(tw, "  %s\t%s\n", k, inst.Params[k])
				}
				tw.Flush()
			}
			if inst.Notes != "" {
				fmt.Fprintf(out, "\n%s %s\n", heading("Notes:"), inst.Notes)
			}

			// Health.
			res, _ := mgr.Health(cmd.Context(), inst.Name)
			fmt.Fprintf(out, "\n%s %s (%s)\n", heading("Health:"), healthText(res.OK), dim(res.Detail))

			// Available component operations.
			if caps := ops.Capabilities(inst.Kind); len(caps) > 0 {
				fmt.Fprintln(out, "\n"+heading("Operations available:"))
				for _, c := range caps {
					fmt.Fprintf(out, "  • %s\n", c.Label)
				}
				fmt.Fprintf(out, "%s\n", dim("  run: keel "+opHint(inst.Kind)))
			}

			// Connection env preview.
			fmt.Fprintln(out, "\n"+heading("Connection (dotenv, host mode):"))
			bundle := inject.Build([]*domain.Instance{inst}, mgr.Config(), inject.ModeHost)
			s, _ := bundle.Render(inject.FormatDotenv)
			fmt.Fprint(out, s)
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "output as JSON")
	return cmd
}

func sortedKeys(m map[string]string) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

// opHint maps a kind to its CLI operations group for the describe hint.
func opHint(kind domain.Kind) string {
	switch kind {
	case domain.KindKafka:
		return "kafka --help"
	case domain.KindPostgres:
		return "pg --help"
	case domain.KindRedis:
		return "redis --help"
	case domain.KindS3:
		return "s3 --help"
	case domain.KindSNS:
		return "sns --help"
	case domain.KindSQS:
		return "sqs --help"
	case domain.KindSecrets:
		return "secret --help"
	case domain.KindVault:
		return "vault --help"
	case domain.KindGCPIdentity:
		return "idp --help"
	default:
		return "--help"
	}
}
