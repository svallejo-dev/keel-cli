package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/svallejo-dev/keel-cli/internal/catalog"
	"github.com/svallejo-dev/keel-cli/internal/domain"
)

func newCreateCmd() *cobra.Command {
	var (
		name         string
		params       []string
		roles        []string
		providerFlag string
	)
	cmd := &cobra.Command{
		Use:   "create <kind> --name <instance>",
		Short: "Create an isolated instance of a blueprint (e.g. 3 separate Postgres)",
		Long: `Create provisions a new, isolated instance of a catalog blueprint.

Every instance is independent: its own container(s), ports and credentials — so
three microservices can each own a dedicated Postgres.

Engine options are passed with --param; see the accepted ones per engine with
` + "`keel catalog <kind>`" + ` (unknown keys are rejected).

Examples:
  keel create postgres --name orders-db
  keel create postgres --name billing-db --param dbname=entdb_billing
  keel create redis    --name session-cache
  keel create kafka    --name event-bus --param topics=pagos:3,envios:6
  keel create sqs      --name work-queue --param visibility_timeout=45
  keel create dynamodb --name events --param pk=eventId --param gsi_name=byUser --param gsi_pk=userId
  keel create secrets  --name gateway-creds`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			kind := domain.Kind(args[0])
			if _, ok := catalog.Get(kind); !ok {
				return exitErr(fmt.Errorf("unknown blueprint %q (run `keel catalog`)", kind))
			}
			if name == "" {
				name = string(kind)
			}
			pmap, err := parseParams(params)
			if err != nil {
				return exitErr(err)
			}
			// Fail loudly on a typo instead of silently ignoring the param.
			if err := catalog.ValidateParams(kind, pmap); err != nil {
				return exitErr(err)
			}
			var rroles []domain.Role
			for _, r := range roles {
				rroles = append(rroles, domain.Role(r))
			}

			mgr, err := buildManager()
			if err != nil {
				return exitErr(err)
			}
			inst, err := mgr.Create(cmd.Context(), kind, name, pmap, rroles, domain.ProviderName(providerFlag))
			if err != nil {
				return exitErr(err)
			}
			fmt.Fprintln(cmd.OutOrStdout(), styleOK.Render("✓ created"))
			printInstanceSummary(cmd.OutOrStdout(), inst)
			fmt.Fprintln(cmd.OutOrStdout())
			fmt.Fprintln(cmd.OutOrStdout(), dim("Wire it into an app:  keel inject "+name+" --out .env"))
			return nil
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "instance name (unique; defaults to the kind)")
	cmd.Flags().StringArrayVar(&params, "param", nil, "engine param key=value (repeatable)")
	cmd.Flags().StringSliceVar(&roles, "role", nil, "override roles (e.g. write,read)")
	cmd.Flags().StringVar(&providerFlag, "provider", "", "override driver: aws-floci|docker (default: blueprint's)")
	return cmd
}

func parseParams(pairs []string) (map[string]string, error) {
	if len(pairs) == 0 {
		return nil, nil
	}
	m := make(map[string]string, len(pairs))
	for _, p := range pairs {
		k, v, ok := strings.Cut(p, "=")
		if !ok || k == "" {
			return nil, fmt.Errorf("invalid %q (want key=value)", p)
		}
		m[strings.TrimSpace(k)] = v
	}
	return m, nil
}
