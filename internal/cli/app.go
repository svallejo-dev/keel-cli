// Package cli builds the keel command tree (Cobra) and wires the control
// Manager to a provider engine (real floci/Docker, or the mock simulator).
package cli

import (
	"fmt"
	"io"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"

	awsprov "github.com/svallejo-dev/keel-cli/internal/provider/aws"
	dockerprov "github.com/svallejo-dev/keel-cli/internal/provider/dockerx"
	gcpprov "github.com/svallejo-dev/keel-cli/internal/provider/gcp"
	"github.com/svallejo-dev/keel-cli/internal/provider/mock"

	"github.com/svallejo-dev/keel-cli/internal/config"
	"github.com/svallejo-dev/keel-cli/internal/control"
	"github.com/svallejo-dev/keel-cli/internal/domain"
	"github.com/svallejo-dev/keel-cli/internal/ops"
	"github.com/svallejo-dev/keel-cli/internal/provider"
	"github.com/svallejo-dev/keel-cli/internal/registry"
	"github.com/svallejo-dev/keel-cli/internal/tui"
)

// Version is stamped at build time (-ldflags).
var Version = "0.1.0-dev"

// persistent flag values
var (
	flagEngine string
	flagHome   string
)

// Execute runs the root command.
func Execute() error {
	root := newRootCmd()
	return root.Execute()
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "keel",
		Short: "Local Infrastructure-as-a-Service — AWS-shaped infra on your machine",
		Long: `keel is a local Infra-as-a-Service platform (a local Fury/Vercel for infra).

It spins up cloud-parity infrastructure — on AWS: Postgres (RDS/Aurora), Redis
(ElastiCache), Kafka (MSK), S3, SNS/SQS, Secrets Manager, DynamoDB via floci; on
GCP: Identity Platform via its official emulator; plus HashiCorp Vault — so any
repo in any language can create the components it needs and connect to them.

Engine selection (--engine): auto (default) uses the real floci/Docker substrate
when Docker is available, otherwise falls back to a mock simulator so the CLI stays
fully usable.`,
		SilenceUsage:  true,
		SilenceErrors: false, // let cobra print errors (incl. arg validation) once
		Version:       Version,
		// Bare `keel` (no subcommand) opens the interactive home launcher when run
		// in a terminal; otherwise it prints help. Every subcommand still works.
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 || !stdoutIsTerminal() {
				return cmd.Help()
			}
			mgr, err := buildManagerQuiet()
			if err != nil {
				return exitErr(err)
			}
			return tui.RunHome(cmd.Context(), mgr)
		},
	}

	root.PersistentFlags().StringVar(&flagEngine, "engine", "", "provider engine: auto|real|mock (default: auto, or $KEEL_ENGINE)")
	root.PersistentFlags().StringVar(&flagHome, "home", "", "state/audit directory (default: ~/.keel or $KEEL_HOME)")

	root.AddCommand(
		newCatalogCmd(),
		newCreateCmd(),
		newApplyCmd(),
		// project workflow
		newInitCmd(),
		newSyncCmd(),
		newConfigCmd(),
		newUpdateCmd(),
		newUpCmd(),
		newDownCmd(),
		newRestartCmd(),
		newDestroyCmd(),
		newListCmd(),
		newDescribeCmd(),
		newHealthCmd(),
		newDoctorCmd(),
		newEngineCmd(),
		newLogsCmd(),
		newConnectCmd(),
		newInjectCmd(),
		newAuditCmd(),
		newTUICmd(),
		// project scaffolding
		newNewCmd(),
		// component operations
		newKafkaCmd(),
		newPgCmd(),
		newRedisCmd(),
		newS3Cmd(),
		newSnsCmd(),
		newSqsCmd(),
		newSecretCmd(),
		newVaultCmd(),
		newIdpCmd(),
	)
	return root
}

// loadConfig applies persistent flags over env/defaults.
func loadConfig() config.Config {
	cfg := config.Load()
	if flagEngine != "" {
		cfg.Engine = config.Engine(flagEngine)
	}
	if flagHome != "" {
		cfg.Home = flagHome
	}
	return cfg
}

// buildManager wires the control Manager for the resolved engine, printing an
// engine banner. See buildManagerOpts.
func buildManager() (*control.Manager, error) { return buildManagerOpts(false) }

// buildManagerQuiet wires the manager without printing the engine banner.
func buildManagerQuiet() (*control.Manager, error) { return buildManagerOpts(true) }

// buildManagerProgress wires the manager without the engine banner but keeps
// progress messages for slow steps (floci bootstrap, emulator image builds) on
// stderr. For CLI commands that print their own header; never for the TUI.
func buildManagerProgress() (*control.Manager, error) {
	return buildManagerWith(true, true)
}

// buildManagerOpts wires the control Manager for the resolved engine. It always
// registers the mock provider too, so instances created in mock mode remain
// manageable after Docker is installed.
func buildManagerOpts(quiet bool) (*control.Manager, error) {
	return buildManagerWith(quiet, !quiet)
}

// buildManagerWith is buildManagerOpts with the banner and the progress
// messages controlled separately.
func buildManagerWith(quiet, progress bool) (*control.Manager, error) {
	cfg := loadConfig()
	store, err := registry.Open(cfg.Home)
	if err != nil {
		return nil, err
	}

	engine := cfg.ResolveEngine()
	providers := map[domain.ProviderName]provider.Provider{
		domain.ProviderMock: mock.New(),
	}
	var force domain.ProviderName

	switch engine {
	case config.EngineMock:
		force = domain.ProviderMock
		if !quiet {
			if cfg.Engine == config.EngineAuto {
				fmt.Fprintln(os.Stderr, warn("Docker not detected — running in MOCK engine (instances are simulated). Install Docker Desktop and re-run for real infrastructure."))
			} else {
				fmt.Fprintln(os.Stderr, dim("engine: mock (instances are simulated)"))
			}
		}
	case config.EngineReal:
		awsDrv, err := awsprov.New(cfg)
		if err != nil {
			return nil, fmt.Errorf("init aws/floci provider: %w", err)
		}
		// Surface floci bootstrap progress on the CLI; stay silent under the TUI,
		// whose alt-screen would be garbled by stray writes.
		if progress {
			awsDrv.SetProgress(func(msg string) { fmt.Fprintln(os.Stderr, dim("• "+msg)) })
		}
		providers[domain.ProviderAWS] = awsDrv
		providers[domain.ProviderDocker] = dockerprov.New(cfg)
		gcpDrv := gcpprov.New(cfg)
		if progress {
			gcpDrv.SetProgress(func(msg string) { fmt.Fprintln(os.Stderr, dim("• "+msg)) })
		}
		providers[domain.ProviderGCP] = gcpDrv
	}

	// Component-operations registry (best-effort; nil if it cannot be built).
	opsReg, err := ops.New(cfg)
	if err != nil {
		opsReg = nil
	}

	return control.NewManager(cfg, store, providers, opsReg, force), nil
}

// newTabWriter returns a tabwriter for aligned table output.
func newTabWriter(w io.Writer) *tabwriter.Writer {
	return tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
}

// stdoutIsTerminal reports whether stdout is an interactive terminal, so the bare
// command only launches the alt-screen TUI when it can actually render.
func stdoutIsTerminal() bool {
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}
