// Package gcp is the Provider for Google Cloud services. GCP has no single
// emulator like floci, so each service runs its official emulator in its own
// container on the shared network. Today it covers Identity Platform through the
// Firebase Auth emulator, which serves the same identitytoolkit API that the
// Identity Platform / Firebase Admin SDKs call in production.
package gcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/svallejo-dev/keel-cli/internal/config"
	"github.com/svallejo-dev/keel-cli/internal/dockercli"
	"github.com/svallejo-dev/keel-cli/internal/domain"
	"github.com/svallejo-dev/keel-cli/internal/provider"
)

const namePrefix = "keel-"

// readyTimeout covers a cold emulator start (node boot + data import).
const readyTimeout = 90 * time.Second

// stopTimeout gives the emulator time to export its data on SIGTERM; docker's
// default 10s can cut an export short and lose users.
const stopTimeout = "30"

// FakeAPIKey is accepted by the emulator for any client call (?key=...).
const FakeAPIKey = "fake-api-key"

// identityDockerfile builds the Identity Platform emulator image. The entrypoint
// writes firebase.json from env at start-up so the container can listen on the
// same port numbers keel publishes on the host: the Emulator UI hands those
// addresses to the browser, so host and container ports must match.
//
// The firebase-tools version comes from the image tag (see identityDockerfileFor),
// so the catalog's image name is the single place to bump it.
//
// Data persists in the /data volume: imported at start when present and exported
// on SIGTERM. Export goes to /data/export, not /data — the emulator replaces the
// target directory and cannot remove a mount point (EBUSY).
const identityDockerfile = `FROM node:22-alpine
RUN npm install -g firebase-tools@{{VERSION}} && firebase setup:emulators:ui
WORKDIR /srv
RUN printf '%s\n' '#!/bin/sh' 'set -e' \
 'cat > /srv/firebase.json <<JSON' \
 '{"emulators":{"auth":{"host":"0.0.0.0","port":${AUTH_PORT:-9099}},"ui":{"enabled":${UI_ENABLED:-true},"host":"0.0.0.0","port":${UI_PORT:-4000}},"hub":{"host":"0.0.0.0","port":4400},"logging":{"host":"0.0.0.0","port":4500}}}' \
 'JSON' \
 'IMPORT=""' \
 'if [ -f /data/export/firebase-export-metadata.json ]; then IMPORT="--import=/data/export"; fi' \
 'exec firebase emulators:start --only auth --project "${GCP_PROJECT:-demo-keel}" $IMPORT --export-on-exit=/data/export' \
 > /usr/local/bin/keel-identity-emulator && chmod +x /usr/local/bin/keel-identity-emulator
VOLUME /data
ENTRYPOINT ["/usr/local/bin/keel-identity-emulator"]
`

// Provider runs GCP emulators as containers.
type Provider struct {
	cfg config.Config
	// progress, when set, receives human-readable messages for slow steps (the
	// one-time image build). Nil keeps provisioning silent (used by the TUI).
	progress func(string)
}

// New returns a GCP emulator provider.
func New(cfg config.Config) *Provider { return &Provider{cfg: cfg} }

// SetProgress installs a callback for progress messages (nil = silent).
func (p *Provider) SetProgress(fn func(string)) { p.progress = fn }

func (p *Provider) Name() domain.ProviderName { return domain.ProviderGCP }

// Provision builds the emulator image if needed, starts the container and waits
// until the emulator answers.
func (p *Provider) Provision(ctx context.Context, spec domain.Spec, bp domain.Blueprint, alloc provider.PortAllocator) (*domain.Instance, error) {
	if bp.Kind != domain.KindGCPIdentity {
		return nil, fmt.Errorf("gcp provider does not handle kind %q", bp.Kind)
	}
	if err := dockercli.EnsureNetwork(ctx, p.cfg.DockerNetwork); err != nil {
		return nil, err
	}
	if err := p.ensureImage(ctx, bp.Image); err != nil {
		return nil, err
	}

	project := strings.TrimSpace(spec.Params["project"])
	if project == "" {
		project = p.cfg.GCPProject
	}
	ui := spec.Params["ui"] != "false"

	var ports []int
	release := func() {
		for _, pt := range ports {
			alloc.Release(pt)
		}
	}
	authPort, err := alloc.Allocate()
	if err != nil {
		return nil, err
	}
	ports = append(ports, authPort)
	uiPort := 0
	if ui {
		if uiPort, err = alloc.Allocate(); err != nil {
			release()
			return nil, err
		}
		ports = append(ports, uiPort)
	}

	container := namePrefix + sanitize(spec.Name)
	args := []string{
		"run", "-d",
		"--name", container,
		"--network", p.cfg.DockerNetwork,
		"--label", "managed-by=keel",
		"-e", fmt.Sprintf("AUTH_PORT=%d", authPort),
		"-e", "GCP_PROJECT=" + project,
		"-e", fmt.Sprintf("UI_ENABLED=%t", ui),
		"-p", fmt.Sprintf("127.0.0.1:%d:%d", authPort, authPort),
	}
	if ui {
		args = append(args,
			"-e", fmt.Sprintf("UI_PORT=%d", uiPort),
			"-p", fmt.Sprintf("127.0.0.1:%d:%d", uiPort, uiPort))
	}
	args = append(args, bp.Image)

	if _, err := dockercli.Run(ctx, args...); err != nil {
		release()
		return nil, fmt.Errorf("docker run %s: %w", container, err)
	}

	now := time.Now()
	inst := &domain.Instance{
		Name:         spec.Name,
		Kind:         bp.Kind,
		Provider:     domain.ProviderGCP,
		CloudService: bp.CloudService,
		Status:       domain.StatusRunning,
		// The emulator listens on the published port inside the container too,
		// so the docker-network address uses the same port number.
		Endpoints: map[domain.Role]domain.Endpoint{
			domain.RoleDefault: {Host: "127.0.0.1", Port: authPort, DockerHost: container, DockerPort: authPort},
		},
		Cred:      domain.Credential{Extra: map[string]string{"api_key": FakeAPIKey}},
		Resources: map[string]string{"container": container, "image": bp.Image, "project": project},
		Params:    spec.Params,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if ui {
		inst.Endpoints[domain.RoleUI] = domain.Endpoint{Host: "127.0.0.1", Port: uiPort}
		inst.Resources["ui_url"] = fmt.Sprintf("http://127.0.0.1:%d/auth", uiPort)
	}

	if err := waitIdentity(ctx, authPort); err != nil {
		_, _ = dockercli.Run(ctx, "rm", "-f", "-v", container)
		release()
		return nil, err
	}
	return inst, nil
}

func (p *Provider) Start(ctx context.Context, inst *domain.Instance) error {
	if _, err := dockercli.Run(ctx, "start", container(inst)); err != nil {
		return err
	}
	if ep, ok := inst.Endpoints[domain.RoleDefault]; ok {
		if err := waitIdentity(ctx, ep.Port); err != nil {
			return err
		}
	}
	inst.Status = domain.StatusRunning
	inst.UpdatedAt = time.Now()
	return nil
}

// Ensure restarts the emulator if it is not running; a no-op otherwise.
func (p *Provider) Ensure(ctx context.Context, inst *domain.Instance) error {
	return p.Start(ctx, inst)
}

func (p *Provider) Stop(ctx context.Context, inst *domain.Instance) error {
	if _, err := dockercli.Run(ctx, "stop", "-t", stopTimeout, container(inst)); err != nil {
		return err
	}
	inst.Status = domain.StatusStopped
	inst.UpdatedAt = time.Now()
	return nil
}

func (p *Provider) Destroy(ctx context.Context, inst *domain.Instance, purge bool) error {
	args := []string{"rm", "-f"}
	if purge {
		args = append(args, "-v") // drop the /data volume holding exported users
	}
	_, err := dockercli.Run(ctx, append(args, container(inst))...)
	return err
}

func (p *Provider) Logs(ctx context.Context, inst *domain.Instance, follow bool, w io.Writer) error {
	args := []string{"logs"}
	if follow {
		args = append(args, "-f")
	} else {
		args = append(args, "--tail", "200")
	}
	return dockercli.Stream(ctx, w, append(args, container(inst))...)
}

// ensureImage builds the emulator image once; later instances reuse it.
func (p *Provider) ensureImage(ctx context.Context, image string) error {
	if dockercli.ImageExists(ctx, image) {
		return nil
	}
	if p.progress != nil {
		p.progress(fmt.Sprintf("building %s (first time only, takes a minute or two)…", image))
	}
	return dockercli.Build(ctx, image, identityDockerfileFor(image))
}

// identityDockerfileFor pins firebase-tools to the image's tag ("latest" when
// the tag is missing).
func identityDockerfileFor(image string) string {
	version := "latest"
	if _, tag, ok := strings.Cut(image, ":"); ok && tag != "" {
		version = tag
	}
	return strings.ReplaceAll(identityDockerfile, "{{VERSION}}", version)
}

// waitIdentity polls the emulator root, which reports {"authEmulator":{"ready":true}}.
func waitIdentity(ctx context.Context, port int) error {
	url := fmt.Sprintf("http://127.0.0.1:%d/", port)
	deadline := time.Now().Add(readyTimeout)
	client := &http.Client{Timeout: 2 * time.Second}
	for {
		if IdentityReady(ctx, client, url) {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("identity emulator not ready at %s after %s (see `keel logs`)", url, readyTimeout)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

// IdentityReady reports whether the auth emulator at url says it is ready.
func IdentityReady(ctx context.Context, client *http.Client, url string) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false
	}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	var body struct {
		AuthEmulator struct {
			Ready bool `json:"ready"`
		} `json:"authEmulator"`
	}
	return json.NewDecoder(resp.Body).Decode(&body) == nil && body.AuthEmulator.Ready
}

func container(inst *domain.Instance) string {
	if c := inst.Resources["container"]; c != "" {
		return c
	}
	return namePrefix + sanitize(inst.Name)
}

func sanitize(name string) string {
	var sb strings.Builder
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_', r == '-', r == '.':
			sb.WriteRune(r)
		default:
			sb.WriteRune('-')
		}
	}
	return sb.String()
}
