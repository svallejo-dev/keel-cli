package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/svallejo-dev/keel-cli/internal/configstore"
	"github.com/svallejo-dev/keel-cli/internal/control"
	"github.com/svallejo-dev/keel-cli/internal/domain"
	"github.com/svallejo-dev/keel-cli/internal/manifest"
)

// configTarget is where a `keel config` command reads/writes: the project's
// store from keel.yaml, or an explicit --store/--path.
type configTarget struct {
	store, path, file string
}

func newConfigCmd() *cobra.Command {
	var t configTarget
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage the app's configuration in its store (Vault or Secrets Manager)",
		Long: `config manages the application configuration a project keeps in its config
store during development — the values its use cases need (feature flags, limits,
third-party keys…). Infrastructure connection details are not here: keel emits
those with sync/inject.

The store is the one declared in the project's keel.yaml:

  config:
    store: orders-vault     # a vault or secrets instance declared under instances
    path: orders            # Vault KV path (default: project name) | secret id
    seed: config.dev.env    # dev values sync adds when missing
    resolve: false          # true: sync also writes the values into .env

Outside a project, point at a store with --store (and --path for Vault).

Examples:
  keel config list
  keel config set FEATURE_X=on MAX_RETRIES=3
  keel config get MAX_RETRIES
  keel config unset FEATURE_X
  keel config import config.dev.env --overwrite
  keel config export --format json
  keel config list --store team-vault --path payments`,
	}
	cmd.PersistentFlags().StringVar(&t.store, "store", "", "store instance (default: config.store from keel.yaml)")
	cmd.PersistentFlags().StringVar(&t.path, "path", "", "Vault KV path or secret id (default: from keel.yaml)")
	cmd.PersistentFlags().StringVarP(&t.file, "file", "f", "", "project manifest (default: ./keel.yaml)")

	cmd.AddCommand(
		configListCmd(&t), configGetCmd(&t), configSetCmd(&t), configUnsetCmd(&t),
		configImportCmd(&t), configExportCmd(&t), configWhereCmd(&t),
	)
	return cmd
}

// open resolves the target store, preferring explicit flags over keel.yaml.
func (t *configTarget) open() (*control.Manager, configstore.Store, error) {
	store, path := t.store, t.path
	if store == "" {
		file := t.file
		if file == "" {
			cwd, err := os.Getwd()
			if err != nil {
				return nil, nil, err
			}
			if file = manifest.FindProject(cwd); file == "" {
				return nil, nil, fmt.Errorf("no keel.yaml here — pass --store <instance> (and --path for Vault), or create one with `keel init`")
			}
		}
		m, err := manifest.Load(file)
		if err != nil {
			return nil, nil, err
		}
		if m.Config == nil {
			return nil, nil, fmt.Errorf("%s has no config section — add one (see `keel config --help`) or pass --store", file)
		}
		store = m.Config.Store
		if path == "" {
			path = m.ConfigPath()
		}
	}
	mgr, err := buildManagerProgress()
	if err != nil {
		return nil, nil, err
	}
	s, err := mgr.ConfigStore(store, path)
	if err != nil {
		return nil, nil, err
	}
	return mgr, s, nil
}

// audit records a config mutation by key name only — values may be secrets.
func (t *configTarget) audit(mgr *control.Manager, s configstore.Store, action string, keys []string) {
	mgr.Store().AuditOK("config."+action, s.Location(), "", s.Backend(),
		map[string]string{"keys": strings.Join(keys, ",")})
}

func configListCmd(t *configTarget) *cobra.Command {
	var reveal bool
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List the stored keys (values that look secret are masked)",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, s, err := t.open()
			if err != nil {
				return exitErr(err)
			}
			values, err := s.Read(cmd.Context())
			if err != nil {
				return exitErr(err)
			}
			out := cmd.OutOrStdout()
			fmt.Fprintln(out, dim(s.Location()))
			if len(values) == 0 {
				fmt.Fprintln(out, dim("empty — add values with `keel config set KEY=VALUE`"))
				return nil
			}
			tw := newTabWriter(out)
			fmt.Fprintln(tw, heading("KEY\tVALUE"))
			for _, k := range configstore.SortedKeys(values) {
				v := values[k]
				if !reveal && looksSecret(k) {
					v = maskValue(v)
				}
				fmt.Fprintf(tw, "%s\t%s\n", k, v)
			}
			return tw.Flush()
		},
	}
	cmd.Flags().BoolVar(&reveal, "reveal", false, "show values of secret-looking keys too")
	return cmd
}

func configGetCmd(t *configTarget) *cobra.Command {
	return &cobra.Command{
		Use:   "get <KEY>",
		Short: "Print one value",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, s, err := t.open()
			if err != nil {
				return exitErr(err)
			}
			values, err := s.Read(cmd.Context())
			if err != nil {
				return exitErr(err)
			}
			v, ok := values[args[0]]
			if !ok {
				return exitErr(fmt.Errorf("%s is not set in %s", args[0], s.Location()))
			}
			fmt.Fprintln(cmd.OutOrStdout(), v)
			return nil
		},
	}
}

func configSetCmd(t *configTarget) *cobra.Command {
	return &cobra.Command{
		Use:   "set <KEY=VALUE>...",
		Short: "Set one or more values (other keys are kept)",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			values, err := parseParams(args)
			if err != nil {
				return exitErr(err)
			}
			mgr, s, err := t.open()
			if err != nil {
				return exitErr(err)
			}
			changed, err := configstore.Set(cmd.Context(), s, values)
			if err != nil {
				return exitErr(err)
			}
			if len(changed) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), dim("no changes"))
				return nil
			}
			t.audit(mgr, s, "set", changed)
			fmt.Fprintf(cmd.OutOrStdout(), "%s %s %s\n", styleOK.Render("✓ updated"), strings.Join(changed, ", "), dim("in "+s.Location()))
			return nil
		},
	}
}

func configUnsetCmd(t *configTarget) *cobra.Command {
	return &cobra.Command{
		Use:   "unset <KEY>...",
		Short: "Remove one or more keys",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mgr, s, err := t.open()
			if err != nil {
				return exitErr(err)
			}
			removed, err := configstore.Unset(cmd.Context(), s, args)
			if err != nil {
				return exitErr(err)
			}
			if len(removed) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), dim("none of those keys were set"))
				return nil
			}
			t.audit(mgr, s, "unset", removed)
			fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", styleOK.Render("✓ removed"), strings.Join(removed, ", "))
			return nil
		},
	}
}

func configImportCmd(t *configTarget) *cobra.Command {
	var overwrite bool
	cmd := &cobra.Command{
		Use:   "import <file.env>",
		Short: "Load KEY=VALUE pairs from a dotenv file (missing keys only, unless --overwrite)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			values, err := configstore.LoadDotenv(args[0])
			if err != nil {
				return exitErr(err)
			}
			mgr, s, err := t.open()
			if err != nil {
				return exitErr(err)
			}
			var keys []string
			if overwrite {
				keys, err = configstore.Set(cmd.Context(), s, values)
			} else {
				keys, err = configstore.Seed(cmd.Context(), s, values)
			}
			if err != nil {
				return exitErr(err)
			}
			if len(keys) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), dim("no changes (existing keys kept; use --overwrite to replace them)"))
				return nil
			}
			t.audit(mgr, s, "import", keys)
			fmt.Fprintf(cmd.OutOrStdout(), "%s %d key(s) into %s: %s\n", styleOK.Render("✓ imported"), len(keys), s.Location(), strings.Join(keys, ", "))
			return nil
		},
	}
	cmd.Flags().BoolVar(&overwrite, "overwrite", false, "replace values that already exist in the store")
	return cmd
}

func configExportCmd(t *configTarget) *cobra.Command {
	var format, out string
	cmd := &cobra.Command{
		Use:   "export",
		Short: "Print every value (dotenv, json or yaml)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, s, err := t.open()
			if err != nil {
				return exitErr(err)
			}
			values, err := s.Read(cmd.Context())
			if err != nil {
				return exitErr(err)
			}
			var b []byte
			switch format {
			case "dotenv":
				var sb strings.Builder
				for _, k := range configstore.SortedKeys(values) {
					fmt.Fprintf(&sb, "%s=%s\n", k, dotenvQuote(values[k]))
				}
				b = []byte(sb.String())
			case "json":
				if b, err = json.MarshalIndent(values, "", "  "); err != nil {
					return exitErr(err)
				}
				b = append(b, '\n')
			case "yaml":
				if b, err = yaml.Marshal(values); err != nil {
					return exitErr(err)
				}
			default:
				return exitErr(fmt.Errorf("unknown format %q (dotenv|json|yaml)", format))
			}
			if out == "" || out == "-" {
				_, err = cmd.OutOrStdout().Write(b)
				return err
			}
			if err := os.WriteFile(out, b, 0o600); err != nil {
				return exitErr(err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s %d key(s) to %s\n", styleOK.Render("✓ exported"), len(values), bold(out))
			return nil
		},
	}
	cmd.Flags().StringVar(&format, "format", "dotenv", "output format: dotenv|json|yaml")
	cmd.Flags().StringVarP(&out, "out", "o", "", "output file (default: stdout)")
	return cmd
}

func configWhereCmd(t *configTarget) *cobra.Command {
	return &cobra.Command{
		Use:   "where",
		Short: "Show which store is used and the variables an app needs to read it",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, s, err := t.open()
			if err != nil {
				return exitErr(err)
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "%s %s\n\n", heading("store:"), bold(s.Location()))
			fmt.Fprintln(out, heading("App bootstrap variables (emitted by `keel sync`):"))
			for _, kv := range s.BootstrapEnv() {
				fmt.Fprintf(out, "  %s=%s\n", kv[0], kv[1])
			}
			if s.Backend() == configstore.BackendVault {
				fmt.Fprintln(out, dim("  + VAULT_ADDR / VAULT_TOKEN from the vault instance"))
			} else {
				fmt.Fprintln(out, dim("  + AWS_ENDPOINT_URL / AWS_REGION / credentials from the secrets instance"))
			}
			return nil
		},
	}
}

// looksSecret flags keys whose values should not be shown by default.
func looksSecret(key string) bool {
	k := strings.ToUpper(key)
	for _, marker := range []string{"SECRET", "PASSWORD", "PASSWD", "TOKEN", "KEY", "CREDENTIAL", "PRIVATE"} {
		if strings.Contains(k, marker) {
			return true
		}
	}
	return false
}

func maskValue(v string) string {
	if len(v) <= 4 {
		return "••••"
	}
	return v[:2] + "••••" + v[len(v)-2:]
}

func dotenvQuote(v string) string {
	if v == "" || strings.ContainsAny(v, " \t\"'$#\n") {
		v = strings.ReplaceAll(v, `"`, `\"`)
		return `"` + strings.ReplaceAll(v, "\n", `\n`) + `"`
	}
	return v
}

// configStoreKinds are the kinds that can back `config.store`.
var configStoreKinds = []domain.Kind{domain.KindVault, domain.KindSecrets}
