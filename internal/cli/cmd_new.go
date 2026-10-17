package cli

import (
	"bufio"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/svallejo-dev/keel-cli/internal/scaffold"
)

// newNewCmd is the parent of the project-scaffolding subcommands. It is a
// separate command branch from the infra lifecycle commands: `new` creates
// source projects from a single shared template, it does not provision infra.
func newNewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "new",
		Short: "Scaffold new source projects from a shared template",
		Long: `new generates project skeletons from keel's built-in templates, so every
project starts from the same consistent, opinionated shape.`,
	}
	cmd.AddCommand(newGoScaffoldCmd())
	return cmd
}

func newGoScaffoldCmd() *cobra.Command {
	var (
		module string
		dir    string
		force  bool
		noTidy bool
		ai     string
		skills string
		store  string
	)
	cmd := &cobra.Command{
		Use:   "go [name]",
		Short: "Scaffold a Go REST service (Package Oriented Design, Gin, graceful shutdown)",
		Long: `go scaffolds a production-shaped Go REST service.

Layout follows Package Oriented Design (cmd/ binaries, internal/ application
packages, internal/platform/ foundational packages). It ships with:

  • Gin HTTP framework
  • GET /ping   liveness  and  GET /health  readiness (with dependency checkers)
  • graceful shutdown on SIGINT/SIGTERM (http.Server.Shutdown with a deadline)
  • structured logging (log/slog) + request-id, access-log and recovery middleware
  • layered config (environment > config store > defaults in configs/config.yaml)
  • config-store loader for Vault (default) or Vault + AWS Secrets Manager
    (--config-store aws); --config-store none keeps environment variables only
  • multi-stage Dockerfile (distroless), Makefile, .golangci.yml

Optionally it also scaffolds AI tool configs (agents/skills/MCP folders) for
Claude Code, GitHub Copilot and/or Cursor, plus a tool-agnostic AGENTS.md. You'll
be prompted for which tools unless you pass --ai. When claude is selected, a set
of bundled Go skills is cloned into .claude/skills/ (control with --skills).

If you omit the name, you'll be prompted for it.

Examples:
  keel new go payments-api
  keel new go orders --module github.com/acme/orders --dir ./services/orders
  keel new go api --ai claude,cursor
  keel new go api --ai claude --skills senior-go-idiomatic,git-commit
  keel new go api --ai none
  keel new go api --config-store aws`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// One reader shared by every prompt: a fresh bufio.Reader per prompt
			// would drop input the first reader already buffered.
			in := bufio.NewReader(cmd.InOrStdin())

			name := ""
			if len(args) == 1 {
				name = strings.TrimSpace(args[0])
			}
			if name == "" {
				name = strings.TrimSpace(promptLine(cmd, in, "Project name: "))
			}
			if name == "" {
				return exitErr(fmt.Errorf("a project name is required"))
			}

			// AI tool configs: use --ai if given, else prompt interactively.
			if !cmd.Flags().Changed("ai") {
				ai = promptLine(cmd, in, fmt.Sprintf("AI tool configs to include (agents/skills/MCP) [%s / all / none]: ", strings.Join(scaffold.KnownAITools, ", ")))
			}

			svc, err := scaffold.NewGoService(scaffold.Options{
				Name:        name,
				Module:      module,
				Dir:         dir,
				Force:       force,
				Tidy:        !noTidy,
				AITools:     splitList(ai),
				Skills:      splitList(skills),
				ConfigStore: store,
			})
			if err != nil {
				return exitErr(err)
			}

			out := cmd.OutOrStdout()
			aiNote := "none"
			if len(svc.AITools) > 0 {
				aiNote = strings.Join(svc.AITools, ", ")
			}
			fmt.Fprintln(out, dim(fmt.Sprintf("scaffolding Go service %q (module %s, AI configs: %s, config store: %s) in %s …", svc.Name, svc.Module, aiNote, svc.ConfigStore, svc.Dir())))

			res, err := svc.Generate(cmd.Context())
			if err != nil {
				return exitErr(err)
			}

			fmt.Fprintln(out, styleOK.Render("✓ created")+"  "+bold(svc.Dir()))
			// List generated files, but collapse the many skill files into one line.
			skillPrefix := ".claude/skills/"
			for _, f := range res.Files {
				if strings.HasPrefix(f, skillPrefix) {
					continue
				}
				fmt.Fprintln(out, dim("  "+f))
			}
			if len(svc.Skills) > 0 {
				fmt.Fprintln(out, dim(fmt.Sprintf("  %s  (%d skills: %s)", skillPrefix, len(svc.Skills), strings.Join(svc.Skills, ", "))))
			}
			fmt.Fprintln(out)
			if noTidy {
				fmt.Fprintln(out, dim("skipped `go mod tidy` (--no-tidy)"))
			} else if res.Tidied {
				fmt.Fprintln(out, dim("ran `go mod tidy` (dependencies resolved)"))
			} else if res.TidyErr != nil {
				fmt.Fprintln(out, warn("`go mod tidy` did not complete: "+res.TidyErr.Error()))
				fmt.Fprintln(out, dim("run it yourself once online:  cd "+svc.Dir()+" && go mod tidy"))
			}

			fmt.Fprintln(out)
			fmt.Fprintln(out, heading("Next steps"))
			fmt.Fprintf(out, "  cd %s\n", svc.Dir())
			if !res.Tidied {
				fmt.Fprintln(out, "  go mod tidy")
			}
			fmt.Fprintln(out, "  make run        # or: go run ./cmd/"+svc.Name)
			fmt.Fprintln(out, dim("  curl localhost:8080/ping"))
			return nil
		},
	}
	cmd.Flags().StringVar(&module, "module", "", "Go module path (default: the project name)")
	cmd.Flags().StringVar(&dir, "dir", "", "output directory (default: ./<name>)")
	cmd.Flags().BoolVar(&force, "force", false, "generate even if the target directory is not empty")
	cmd.Flags().BoolVar(&noTidy, "no-tidy", false, "skip running `go mod tidy` after generating")
	cmd.Flags().StringVar(&ai, "ai", "", "AI tool configs to include, comma-separated: claude,copilot,cursor | all | none (prompts if unset)")
	cmd.Flags().StringVar(&skills, "skills", "all", "Claude skills to clone (needs --ai claude): all | none | comma-separated names")
	cmd.Flags().StringVar(&store, "config-store", scaffold.ConfigStoreVault, "config-store loader: vault | aws (Vault + AWS Secrets Manager) | none")
	return cmd
}

// splitList parses a comma-separated flag value into trimmed, non-empty items.
func splitList(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// promptLine writes a label and reads a single line from the shared reader.
func promptLine(cmd *cobra.Command, r *bufio.Reader, label string) string {
	fmt.Fprint(cmd.OutOrStdout(), label)
	line, _ := r.ReadString('\n')
	return strings.TrimRight(line, "\r\n")
}
