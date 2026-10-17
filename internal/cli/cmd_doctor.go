package cli

import (
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"time"

	"github.com/spf13/cobra"

	"github.com/svallejo-dev/keel-cli/internal/config"
)

func newDoctorCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose the platform environment (Docker, floci, engine, state)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg := loadConfig()
			out := cmd.OutOrStdout()
			fmt.Fprintln(out, heading("Environment diagnostics"))

			// Docker CLI + daemon.
			dockerCLI := lookPath("docker")
			checkline(out, "docker CLI", dockerCLI != "", dockerCLI, "install Docker Desktop")
			daemon := config.DockerAvailable()
			checkline(out, "docker daemon", daemon, "running", "start Docker Desktop")

			// floci endpoint.
			flociUp := httpReachable(cfg.FlociEndpoint)
			checkline(out, "floci endpoint", flociUp, cfg.FlociEndpoint, "run `task floci-up` (or docker compose up -d) to start floci")

			// Resolved engine.
			engine := cfg.ResolveEngine()
			fmt.Fprintf(out, "  %s %s\n", styleInfo.Render("engine:"), engine)
			if engine == config.EngineMock {
				fmt.Fprintln(out, dim("    (mock: instances are simulated — install Docker + start floci for real infra)"))
			}

			// State.
			fmt.Fprintf(out, "  %s %s\n", styleInfo.Render("state dir:"), cfg.Home)
			mgr, err := buildManagerQuiet()
			if err == nil {
				fmt.Fprintf(out, "  %s %d\n", styleInfo.Render("instances:"), len(mgr.List()))
			}
			return nil
		},
	}
	return cmd
}

func checkline(w io.Writer, label string, ok bool, okDetail, hint string) {
	if ok {
		fmt.Fprintf(w, "  %s %-16s %s\n", styleOK.Render("✓"), label, dim(okDetail))
	} else {
		fmt.Fprintf(w, "  %s %-16s %s\n", styleBad.Render("✗"), label, dim("— "+hint))
	}
}

func lookPath(bin string) string {
	p, err := exec.LookPath(bin)
	if err != nil {
		return ""
	}
	return p
}

func httpReachable(url string) bool {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return true
}
