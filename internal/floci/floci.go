// Package floci brings the floci AWS emulator up from nothing. It creates the
// shared docker network, pulls the image if missing, starts the container and
// waits until the AWS endpoint answers — so "everything from zero" (no network,
// no image, no container) just works before the first `create`.
//
// It shells out to the docker CLI (same approach as the dockerx provider) rather
// than embedding compose, so it works from any directory once keel is installed.
package floci

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"github.com/svallejo-dev/keel-cli/internal/config"
)

const (
	// ContainerName is the docker container keel manages for the emulator.
	ContainerName = "floci"
	// Image is the floci distribution keel starts.
	Image = "floci/floci:latest"
	// DataVolume persists floci state across restarts.
	DataVolume = "keel-floci-data"

	apiPort        = 4566
	rdsPortRange   = "7000-7060" // floci proxies RDS instances here
	cachePortRange = "6379-6400" // …and ElastiCache here

	probeTimeout = 2 * time.Second
	// startTimeout covers a cold start including the image pull.
	startTimeout = 10 * time.Minute
	// readyTimeout is how long we wait for the API to answer once started.
	readyTimeout = 90 * time.Second
)

// State describes the emulator's container state.
type State string

const (
	StateAbsent  State = "absent"  // no container
	StateStopped State = "stopped" // container exists but is not running
	StateRunning State = "running" // container is running
)

// Status is a snapshot of the emulator's health.
type Status struct {
	DockerAvailable bool
	Container       State
	Reachable       bool // the AWS API answered
	Endpoint        string
}

// Probe reports the current state without changing anything.
func Probe(ctx context.Context, cfg config.Config) Status {
	s := Status{Endpoint: cfg.FlociEndpoint, Container: StateAbsent}
	s.DockerAvailable = config.DockerAvailable()
	if !s.DockerAvailable {
		return s
	}
	s.Container = containerState(ctx)
	s.Reachable = Reachable(ctx, cfg.FlociEndpoint)
	return s
}

// Reachable reports whether the AWS endpoint answers. Any HTTP response means the
// emulator is serving (even 400/404).
func Reachable(ctx context.Context, endpoint string) bool {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return false
	}
	resp, err := (&http.Client{Timeout: probeTimeout}).Do(req)
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return true
}

// EnsureUp makes the emulator reachable, doing whatever is needed: creating the
// network, pulling the image, starting or creating the container, then waiting
// for the API. It is a no-op (one fast probe) when floci is already up, so it is
// cheap to call before every provision. log may be nil.
func EnsureUp(ctx context.Context, cfg config.Config, log func(string)) error {
	say := func(format string, args ...any) {
		if log != nil {
			log(fmt.Sprintf(format, args...))
		}
	}

	if Reachable(ctx, cfg.FlociEndpoint) {
		return nil
	}
	if !config.DockerAvailable() {
		return fmt.Errorf("Docker no está disponible: instala/arranca Docker Desktop y reintenta")
	}

	ctx, cancel := context.WithTimeout(ctx, startTimeout)
	defer cancel()

	if err := ensureNetwork(ctx, cfg.DockerNetwork); err != nil {
		return fmt.Errorf("crear red %s: %w", cfg.DockerNetwork, err)
	}

	switch containerState(ctx) {
	case StateRunning:
		say("floci ya está corriendo — esperando a que responda…")
	case StateStopped:
		say("arrancando el contenedor floci…")
		if _, err := dockerRun(ctx, "start", ContainerName); err != nil {
			return fmt.Errorf("arrancar %s: %w", ContainerName, err)
		}
	case StateAbsent:
		if !imageExists(ctx) {
			say("descargando la imagen %s (la primera vez puede tardar unos minutos)…", Image)
		}
		say("creando el contenedor floci…")
		if _, err := dockerRun(ctx, runArgs(cfg)...); err != nil {
			return fmt.Errorf("crear %s: %w", ContainerName, err)
		}
	}

	say("esperando a que la API de floci responda en %s…", cfg.FlociEndpoint)
	if err := waitReachable(ctx, cfg.FlociEndpoint); err != nil {
		return fmt.Errorf("floci arrancó pero no responde en %s: %w (revisa: docker logs %s)",
			cfg.FlociEndpoint, err, ContainerName)
	}
	say("floci listo ✓")
	return nil
}

// Down stops the emulator container (data volume is kept).
func Down(ctx context.Context, log func(string)) error {
	if !config.DockerAvailable() {
		return fmt.Errorf("Docker no está disponible")
	}
	if containerState(ctx) == StateAbsent {
		if log != nil {
			log("no hay contenedor floci")
		}
		return nil
	}
	if _, err := dockerRun(ctx, "stop", ContainerName); err != nil {
		return fmt.Errorf("detener %s: %w", ContainerName, err)
	}
	if log != nil {
		log("floci detenido")
	}
	return nil
}

// runArgs builds the `docker run` invocation mirroring docker/floci-compose.yml.
// Ports bind to 127.0.0.1 explicitly: on Windows "localhost" resolves to IPv6
// first, which would not reach an IPv4-only publish.
func runArgs(cfg config.Config) []string {
	return []string{
		"run", "-d",
		"--name", ContainerName,
		"--restart", "unless-stopped",
		"--network", cfg.DockerNetwork,
		"--label", "managed-by=keel",
		"-p", fmt.Sprintf("127.0.0.1:%d:%d", apiPort, apiPort),
		"-p", fmt.Sprintf("127.0.0.1:%s:%s", rdsPortRange, rdsPortRange),
		"-p", fmt.Sprintf("127.0.0.1:%s:%s", cachePortRange, cachePortRange),
		"-e", "AWS_DEFAULT_REGION=" + cfg.Region,
		// No FLOCI_SERVICES limit: keel uses s3/sns/sqs/secretsmanager/kms plus
		// rds/elasticache/kafka/dynamodb, so every service stays enabled.
		"-v", "/var/run/docker.sock:/var/run/docker.sock",
		"-v", DataVolume + ":/var/lib/floci",
		Image,
	}
}

func ensureNetwork(ctx context.Context, name string) error {
	if _, err := dockerRun(ctx, "network", "inspect", name); err == nil {
		return nil
	}
	_, err := dockerRun(ctx, "network", "create", name)
	return err
}

// containerState reports whether the floci container is absent, stopped or running.
func containerState(ctx context.Context) State {
	out, err := dockerRun(ctx, "inspect", "-f", "{{.State.Running}}", ContainerName)
	if err != nil {
		return StateAbsent
	}
	if strings.TrimSpace(out) == "true" {
		return StateRunning
	}
	return StateStopped
}

func imageExists(ctx context.Context) bool {
	_, err := dockerRun(ctx, "image", "inspect", Image)
	return err == nil
}

func waitReachable(ctx context.Context, endpoint string) error {
	deadline := time.Now().Add(readyTimeout)
	for {
		if Reachable(ctx, endpoint) {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timeout tras %s", readyTimeout)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(1500 * time.Millisecond):
		}
	}
}

func dockerRun(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("%s", msg)
	}
	return strings.TrimSpace(out.String()), nil
}
