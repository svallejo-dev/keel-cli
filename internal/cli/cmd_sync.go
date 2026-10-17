package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/svallejo-dev/keel-cli/internal/catalog"
	"github.com/svallejo-dev/keel-cli/internal/config"
	"github.com/svallejo-dev/keel-cli/internal/configstore"
	"github.com/svallejo-dev/keel-cli/internal/control"
	"github.com/svallejo-dev/keel-cli/internal/domain"
	"github.com/svallejo-dev/keel-cli/internal/floci"
	"github.com/svallejo-dev/keel-cli/internal/inject"
	"github.com/svallejo-dev/keel-cli/internal/manifest"
)

// stackStats summarizes what a manifest run did.
type stackStats struct{ created, skipped, started, failed int }

// provisionManifest creates the manifest's missing instances (idempotent) and,
// when start is true, starts the ones that exist but are stopped. Shared by
// `apply` (stacks) and `sync` (project manifests).
func provisionManifest(ctx context.Context, mgr *control.Manager, m *manifest.Manifest, out io.Writer, start bool) stackStats {
	var s stackStats
	for _, in := range m.Instances {
		if mgr.Store().Exists(in.Name) {
			if start {
				if inst, err := mgr.Get(in.Name); err == nil && inst.Status == domain.StatusStopped {
					if err := mgr.Up(ctx, in.Name); err != nil {
						fmt.Fprintf(out, "%s %s: %s\n", styleBad.Render("✗"), in.Name, err.Error())
						s.failed++
						continue
					}
					fmt.Fprintf(out, "%s %s %s\n", styleOK.Render("▲"), in.Name, dim("(started)"))
					s.started++
					continue
				}
				// The registry says running, but the container may have died with
				// the host or come back sealed (Vault): let the driver repair it.
				if _, err := mgr.Ensure(ctx, in.Name); err != nil {
					fmt.Fprintf(out, "%s %s: %s\n", styleBad.Render("✗"), in.Name, err.Error())
					s.failed++
					continue
				}
			}
			fmt.Fprintf(out, "%s %s %s\n", dim("•"), in.Name, dim("(exists)"))
			s.skipped++
			continue
		}
		if _, err := mgr.Create(ctx, domain.Kind(in.Kind), in.Name, in.Params, in.DomainRoles(), domain.ProviderName(in.Provider)); err != nil {
			fmt.Fprintf(out, "%s %s: %s\n", styleBad.Render("✗"), in.Name, err.Error())
			s.failed++
			continue
		}
		fmt.Fprintf(out, "%s %s %s\n", styleOK.Render("✓"), in.Name, dim("("+in.Kind+")"))
		s.created++
	}
	return s
}

// newSyncCmd reconciles the current project: engine up, declared infra present
// and running, and the connection file refreshed — in one command.
func newSyncCmd() *cobra.Command {
	var file, mode, format, out string
	var resolve bool
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Reconcile this project's infra (keel.yaml) and refresh its connection file",
		Long: `Sync makes reality match what your project declares.

Standing in a repo with a keel.yaml, it: (1) starts the engine if it is down,
(2) creates any declared instance that is missing and starts those that are
stopped (and repairs running ones whose container died or whose Vault is
sealed), (3) rewrites the connection file (.env by default) with the current
endpoints and credentials, and (4) when keel.yaml has a config section, seeds the
config store (Vault or Secrets Manager) and points the app at it — or, with
resolve, writes the stored values into the connection file too.

It is idempotent — run it after a reboot, after a git pull, or whenever you edit
keel.yaml. Values like ports and queue URLs are assigned at runtime, so
re-running keeps your connection file from going stale.

Create the manifest with ` + "`keel init`" + `.

Examples:
  keel sync
  keel sync --mode docker          # for an app running in a container
  keel sync --resolve              # also write the config store's values
  keel sync --file stacks/qa.yaml`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			path := file
			if path == "" {
				cwd, err := os.Getwd()
				if err != nil {
					return exitErr(err)
				}
				if path = manifest.FindProject(cwd); path == "" {
					return exitErr(fmt.Errorf("no %s here — create one with `keel init`", manifest.ProjectFiles[0]))
				}
			}
			m, err := manifest.Load(path)
			if err != nil {
				return exitErr(err)
			}

			outW := cmd.OutOrStdout()
			label := m.Name
			if label == "" {
				label = path
			}
			fmt.Fprintf(outW, "%s %s %s\n", heading("syncing project:"), bold(label), dim("("+path+")"))

			// 1) Engine: make sure the substrate is actually up — floci only when
			// an instance is AWS-backed (a Vault/GCP-only project never needs it).
			cfg := loadConfig()
			if cfg.ResolveEngine() == config.EngineReal && needsFloci(m) {
				if err := floci.EnsureUp(cmd.Context(), cfg, func(msg string) {
					fmt.Fprintln(outW, dim("• "+msg))
				}); err != nil {
					return exitErr(err)
				}
			}

			mgr, err := buildManagerProgress()
			if err != nil {
				return exitErr(err)
			}

			// 2) Infra: create what's missing, start what's stopped.
			st := provisionManifest(cmd.Context(), mgr, m, outW, true)
			if st.failed > 0 {
				return exitErr(fmt.Errorf("%d instance(s) failed", st.failed))
			}

			// 3) Connections: refresh the project's config file.
			set := m.InjectOrDefault()
			if mode != "" {
				set.Mode = mode
			}
			if format != "" {
				set.Format = format
			}
			if out != "" {
				set.Out = out
			}

			insts := make([]*domain.Instance, 0, len(m.Instances))
			for _, in := range m.Instances {
				inst, err := mgr.Get(in.Name)
				if err != nil {
					return exitErr(err)
				}
				insts = append(insts, inst)
			}
			bundle := inject.Build(insts, mgr.Config(), inject.Mode(set.Mode))

			// 4) Application config: seed the store and point the app at it (or
			// resolve the values into the file when the project asks for it).
			if m.Config != nil {
				if err := syncConfig(cmd.Context(), mgr, m, bundle, resolve, outW); err != nil {
					return exitErr(err)
				}
			}

			rendered, err := bundle.Render(inject.Format(set.Format))
			if err != nil {
				return exitErr(err)
			}
			if err := os.WriteFile(set.Out, []byte(rendered), 0o600); err != nil {
				return exitErr(fmt.Errorf("write %s: %w", set.Out, err))
			}
			mgr.Store().AuditOK("sync", set.Out, "", string(mgr.Config().Engine),
				map[string]string{"format": set.Format, "mode": set.Mode, "manifest": path})

			fmt.Fprintf(outW, "\n%s created=%d started=%d unchanged=%d\n", dim("infra:"), st.created, st.started, st.skipped)
			fmt.Fprintf(outW, "%s %s %s\n", styleOK.Render("✓ connections →"), bold(set.Out),
				dim(fmt.Sprintf("(%d vars · %s · %s mode)", len(bundle.Vars), set.Format, set.Mode)))
			fmt.Fprintln(outW, dim("your app can start now — the config file is up to date"))
			return nil
		},
	}
	cmd.Flags().StringVarP(&file, "file", "f", "", "manifest path (default: ./keel.yaml)")
	cmd.Flags().StringVar(&mode, "mode", "", "override address mode: host|docker")
	cmd.Flags().StringVar(&format, "format", "", "override output format: dotenv|env|json|yaml")
	cmd.Flags().StringVarP(&out, "out", "o", "", "override output file")
	cmd.Flags().BoolVar(&resolve, "resolve", false, "write the config store's values into the connection file (overrides config.resolve)")
	return cmd
}

// needsFloci reports whether any declared instance is provisioned through floci.
func needsFloci(m *manifest.Manifest) bool {
	for _, in := range m.Instances {
		prov := domain.ProviderName(in.Provider)
		if prov == "" {
			if bp, ok := catalog.Get(domain.Kind(in.Kind)); ok {
				prov = bp.Provider
			}
		}
		if prov == domain.ProviderAWS {
			return true
		}
	}
	return false
}

// syncConfig seeds the project's config store from its seed file and adds to the
// bundle either the bootstrap variables the app needs to read the store, or the
// resolved values themselves.
func syncConfig(ctx context.Context, mgr *control.Manager, m *manifest.Manifest, bundle *inject.Bundle, forceResolve bool, out io.Writer) error {
	cfg := m.Config
	inst, err := mgr.Get(cfg.Store)
	if err != nil {
		return err
	}
	if inst.Provider == domain.ProviderMock {
		fmt.Fprintln(out, warn("config: "+cfg.Store+" is simulated (mock engine) — skipping the config store"))
		return nil
	}
	store, err := mgr.ConfigStore(cfg.Store, m.ConfigPath())
	if err != nil {
		return err
	}

	if cfg.Seed != "" {
		values, err := configstore.LoadDotenv(cfg.Seed)
		switch {
		case errors.Is(err, os.ErrNotExist):
			fmt.Fprintln(out, warn("config: seed file "+cfg.Seed+" not found — nothing seeded"))
		case err != nil:
			return fmt.Errorf("config seed: %w", err)
		default:
			added, err := configstore.Seed(ctx, store, values)
			if err != nil {
				return fmt.Errorf("config seed: %w", err)
			}
			if len(added) > 0 {
				fmt.Fprintf(out, "%s %s %s\n", styleOK.Render("✓ config seeded"), bold(store.Location()),
					dim(fmt.Sprintf("(%d new: %s)", len(added), strings.Join(added, ", "))))
				mgr.Store().AuditOK("config.seed", cfg.Store, string(inst.Kind), string(inst.Provider),
					map[string]string{"location": store.Location(), "keys": strings.Join(added, ",")})
			}
		}
	}

	if forceResolve || cfg.Resolve {
		values, err := store.Read(ctx)
		if err != nil {
			return fmt.Errorf("config read: %w", err)
		}
		var shadowed []string
		for _, k := range configstore.SortedKeys(values) {
			if !bundle.Add(k, values[k]) {
				shadowed = append(shadowed, k)
			}
		}
		fmt.Fprintf(out, "%s %s %s\n", styleOK.Render("✓ config resolved"), bold(store.Location()),
			dim(fmt.Sprintf("(%d values written to the connection file)", len(values)-len(shadowed))))
		if len(shadowed) > 0 {
			fmt.Fprintln(out, warn("config: keys also emitted by keel kept keel's value: "+strings.Join(shadowed, ", ")))
		}
		return nil
	}

	for _, kv := range store.BootstrapEnv() {
		bundle.Add(kv[0], kv[1])
	}
	fmt.Fprintf(out, "%s %s %s\n", styleOK.Render("✓ config store"), bold(store.Location()),
		dim("(the app reads it at startup via CONFIG_STORE)"))
	return nil
}
