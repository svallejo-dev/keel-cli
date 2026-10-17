package cli

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/svallejo-dev/keel-cli/internal/catalog"
	"github.com/svallejo-dev/keel-cli/internal/domain"
	"github.com/svallejo-dev/keel-cli/internal/manifest"
)

// newInitCmd writes a project manifest (keel.yaml) declaring the infra this repo
// needs, so `keel sync` can reconcile it from then on.
func newInitCmd() *cobra.Command {
	var name, kinds, out, mode string
	var force bool

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Create a keel.yaml declaring the infra this project needs",
		Long: `Init writes a project manifest (keel.yaml) in the current directory listing the
infrastructure this repo needs and where its connection config should be written.

Commit it: anyone who clones the repo runs ` + "`keel sync`" + ` and gets the same infra
plus a ready-to-use .env. You are prompted for anything not passed as a flag.

Examples:
  keel init
  keel init --name admin-eventhub --kinds dynamodb,s3,sqs
  keel init --kinds postgres,redis --out .env --mode docker`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			outW := cmd.OutOrStdout()
			in := bufio.NewReader(cmd.InOrStdin())

			target := manifest.ProjectFiles[0]
			if _, err := os.Stat(target); err == nil && !force {
				return exitErr(fmt.Errorf("%s already exists (use --force to overwrite)", target))
			}

			// Project name: flag, else prompt (defaulting to the directory name).
			if name == "" {
				def := "project"
				if cwd, err := os.Getwd(); err == nil {
					def = filepath.Base(cwd)
				}
				name = strings.TrimSpace(promptLine(cmd, in, fmt.Sprintf("Project name [%s]: ", def)))
				if name == "" {
					name = def
				}
			}

			// Components: flag, else prompt showing the catalog.
			if kinds == "" {
				fmt.Fprintln(outW, dim("Available: "+strings.Join(activeKinds(), ", ")))
				kinds = promptLine(cmd, in, "Components (comma-separated): ")
			}
			chosen, err := parseKinds(kinds)
			if err != nil {
				return exitErr(err)
			}
			if len(chosen) == 0 {
				return exitErr(fmt.Errorf("pick at least one component"))
			}

			m := manifest.Manifest{
				Version: 1,
				Name:    sanitizeProjectName(name),
				Inject:  &manifest.Inject{Out: firstNonEmpty(out, ".env"), Format: "dotenv", Mode: firstNonEmpty(mode, "host")},
			}
			for _, k := range chosen {
				m.Instances = append(m.Instances, manifest.Instance{
					Name: instanceName(m.Name, k),
					Kind: string(k),
				})
			}

			// A config store among the components becomes the home of the app's
			// own configuration (Vault preferred when both are picked).
			if store := pickConfigStore(m.Instances); store != "" {
				m.Config = &manifest.Config{Store: store, Seed: configSeedFile}
				if err := writeSeedTemplate(configSeedFile); err != nil {
					return exitErr(err)
				}
			}

			body, err := yaml.Marshal(m)
			if err != nil {
				return exitErr(err)
			}
			header := fmt.Sprintf(`# keel project manifest — the infra this repo needs.
#
# Commit this file. Anyone who clones the repo runs "keel sync" to get the same
# infrastructure and a fresh %s. Local only: in QA/prod the platform injects the
# same variables instead.
`, m.InjectOrDefault().Out)
			if err := os.WriteFile(target, append([]byte(header), body...), 0o644); err != nil {
				return exitErr(fmt.Errorf("write %s: %w", target, err))
			}

			fmt.Fprintln(outW, styleOK.Render("✓ created")+"  "+bold(target))
			for _, i := range m.Instances {
				fmt.Fprintf(outW, "  %s %s\n", i.Name, dim("("+i.Kind+")"))
			}
			if m.Config != nil {
				fmt.Fprintf(outW, "  %s %s\n", dim("config store →"), m.Config.Store+dim(" (seed: "+configSeedFile+")"))
			}
			fmt.Fprintln(outW)
			fmt.Fprintln(outW, heading("Next step"))
			if m.Config != nil {
				fmt.Fprintln(outW, "  edita "+configSeedFile+"   # valores de desarrollo de tu app")
			}
			fmt.Fprintln(outW, "  keel sync        # crea la infra y escribe "+m.InjectOrDefault().Out)
			return nil
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "project name (default: the directory name)")
	cmd.Flags().StringVar(&kinds, "kinds", "", "components, comma-separated (e.g. dynamodb,s3,sqs)")
	cmd.Flags().StringVarP(&out, "out", "o", "", "connection file to write on sync (default: .env)")
	cmd.Flags().StringVar(&mode, "mode", "", "address mode: host|docker (default: host)")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite an existing manifest")
	return cmd
}

// configSeedFile is the dotenv file `keel init` proposes for development values.
const configSeedFile = "config.dev.env"

// pickConfigStore returns the instance that should hold the app config: the
// first vault, else the first secrets instance, else "".
func pickConfigStore(insts []manifest.Instance) string {
	for _, k := range configStoreKinds {
		for _, in := range insts {
			if in.Kind == string(k) {
				return in.Name
			}
		}
	}
	return ""
}

// writeSeedTemplate creates the seed file with guidance unless it exists.
func writeSeedTemplate(path string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	const body = `# Valores de configuración de la APP para desarrollo local.
#
# keel sync los siembra en el config store (Vault o Secrets Manager) solo si la
# clave aún no existe: lo que cambies con ` + "`keel config set`" + ` nunca se pisa.
# Tu app los lee del store al arrancar (CONFIG_STORE), igual que en QA/prod.
#
# Aquí van valores de lógica/casos de uso (flags, límites, URLs de terceros…),
# NO credenciales de infraestructura: esas las emite keel en el .env.
# Si este archivo lleva secretos reales, no lo versiones (añádelo a .gitignore).
#
# FEATURE_NEW_CHECKOUT=false
# MAX_RETRIES=3
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// activeKinds lists the non-deferred catalog kinds for the prompt.
func activeKinds() []string {
	bps := catalog.Active()
	ks := make([]string, 0, len(bps))
	for _, bp := range bps {
		ks = append(ks, string(bp.Kind))
	}
	return ks
}

// parseKinds validates a comma-separated kind list against the catalog.
func parseKinds(spec string) ([]domain.Kind, error) {
	var out []domain.Kind
	seen := map[domain.Kind]bool{}
	for _, raw := range strings.Split(spec, ",") {
		k := domain.Kind(strings.ToLower(strings.TrimSpace(raw)))
		if k == "" || seen[k] {
			continue
		}
		if _, ok := catalog.Get(k); !ok {
			return nil, fmt.Errorf("unknown component %q (available: %s)", k, strings.Join(activeKinds(), ", "))
		}
		seen[k] = true
		out = append(out, k)
	}
	return out, nil
}

// instanceName derives a stable instance name from the project and component.
// The emitted env var names come from this, so it is the naming contract.
func instanceName(project string, k domain.Kind) string {
	return project + "-" + string(k)
}

func sanitizeProjectName(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteRune('-')
		}
	}
	return strings.Trim(b.String(), "-")
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
