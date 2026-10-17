// Package dockerx is the Provider that runs real engine containers directly via
// the docker CLI. It backs the data engines whose AWS managed equivalents are the
// same engine under the hood (postgres≡RDS, redis≡ElastiCache, redpanda≡MSK) plus
// non-AWS sidecars (schema registry). floci is reserved for AWS-API services.
package dockerx

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/svallejo-dev/keel-cli/internal/config"
	"github.com/svallejo-dev/keel-cli/internal/dockercli"
	"github.com/svallejo-dev/keel-cli/internal/domain"
	"github.com/svallejo-dev/keel-cli/internal/provider"
	"github.com/svallejo-dev/keel-cli/internal/vaultx"
)

// Provider runs engines as Docker containers.
type Provider struct {
	cfg config.Config
}

// New returns a docker-backed provider.
func New(cfg config.Config) *Provider { return &Provider{cfg: cfg} }

// kafkaExternalPort is the in-container port of Redpanda's EXTERNAL listener,
// published to the host (the INTERNAL one stays on the blueprint's default port).
const kafkaExternalPort = 19092

func (p *Provider) Name() domain.ProviderName { return domain.ProviderDocker }

const namePrefix = "keel-"

// Provision creates and starts the container(s) for the instance.
func (p *Provider) Provision(ctx context.Context, spec domain.Spec, bp domain.Blueprint, alloc provider.PortAllocator) (*domain.Instance, error) {
	if err := p.ensureNetwork(ctx); err != nil {
		return nil, err
	}
	port, err := alloc.Allocate()
	if err != nil {
		return nil, err
	}

	now := time.Now()
	inst := &domain.Instance{
		Name:         spec.Name,
		Kind:         bp.Kind,
		Provider:     domain.ProviderDocker,
		CloudService: bp.CloudService,
		Status:       domain.StatusRunning,
		Endpoints:    map[domain.Role]domain.Endpoint{},
		Resources:    map[string]string{},
		Params:       spec.Params,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	container := namePrefix + sanitize(spec.Name)
	image := p.image(bp, spec)
	// 127.0.0.1, not "localhost": on Windows "localhost" resolves to IPv6 ::1 first
	// while the published port is IPv4-only, which yields "connection refused".
	ep := domain.Endpoint{Host: "127.0.0.1", Port: port, DockerHost: container, DockerPort: bp.DefaultPort}

	var runArgs []string
	switch bp.Kind {
	case domain.KindPostgres:
		user, pass, db := "infra", "infra_"+randHex(5), dbName(spec)
		inst.Cred = domain.Credential{Username: user, Password: pass, Extra: map[string]string{"dbname": db}}
		runArgs = p.baseRun(container, port, bp.DefaultPort,
			"-e", "POSTGRES_USER="+user,
			"-e", "POSTGRES_PASSWORD="+pass,
			"-e", "POSTGRES_DB="+db,
			image)
		// Local single node serves both write and read roles (no streaming replica).
		inst.Endpoints[domain.RoleWrite] = ep
		inst.Endpoints[domain.RoleRead] = ep

	case domain.KindRedis:
		runArgs = p.baseRun(container, port, bp.DefaultPort, image)
		inst.Endpoints[domain.RoleDefault] = ep

	case domain.KindKafka:
		// Two listeners, because a broker can only advertise one address per
		// listener and our clients live in both worlds:
		//   INTERNAL <container>:9092      → containers on the shared network
		//                                    (this is how the ops layer runs rpk)
		//   EXTERNAL 127.0.0.1:<hostPort>  → processes on the host
		// Publishing a single host-advertised address breaks in-container clients,
		// which is exactly what happened before this.
		runArgs = p.baseRun(container, port, kafkaExternalPort, image,
			"redpanda", "start",
			"--overprovisioned", "--smp", "1", "--memory", "512M", "--reserve-memory", "0M",
			"--node-id", "0", "--check=false",
			fmt.Sprintf("--kafka-addr=INTERNAL://0.0.0.0:%d,EXTERNAL://0.0.0.0:%d", bp.DefaultPort, kafkaExternalPort),
			fmt.Sprintf("--advertise-kafka-addr=INTERNAL://%s:%d,EXTERNAL://127.0.0.1:%d", container, bp.DefaultPort, port),
		)
		inst.Endpoints[domain.RoleDefault] = ep

	case domain.KindSchemaRegistry:
		runArgs = p.baseRun(container, port, bp.DefaultPort, image)
		inst.Endpoints[domain.RoleDefault] = ep

	case domain.KindVault:
		// A real server (file storage on the image's /vault/file volume), not -dev:
		// dev mode keeps everything in memory, so a restart would wipe the
		// project's configuration. keel initializes and unseals it instead.
		runArgs = p.baseRun(container, port, bp.DefaultPort,
			"-e", "SKIP_SETCAP=true",
			"-e", "VAULT_LOCAL_CONFIG="+vaultServerConfig,
			image, "server")
		inst.Endpoints[domain.RoleDefault] = ep

	default:
		// Generic engine: run image, map default port.
		runArgs = p.baseRun(container, port, bp.DefaultPort, image)
		inst.Endpoints[domain.RoleDefault] = ep
	}

	if _, err := p.docker(ctx, runArgs...); err != nil {
		alloc.Release(port)
		return nil, fmt.Errorf("docker run %s: %w", container, err)
	}
	inst.Resources["container"] = container
	inst.Resources["image"] = image

	if bp.Kind == domain.KindVault {
		if err := bootstrapVault(ctx, inst, spec); err != nil {
			_, _ = p.docker(ctx, "rm", "-f", "-v", container)
			alloc.Release(port)
			return nil, err
		}
	}
	return inst, nil
}

func (p *Provider) Start(ctx context.Context, inst *domain.Instance) error {
	if _, err := p.docker(ctx, "start", container(inst)); err != nil {
		return err
	}
	if err := afterStart(ctx, inst); err != nil {
		return err
	}
	inst.Status = domain.StatusRunning
	inst.UpdatedAt = time.Now()
	return nil
}

// Ensure starts the container if it is not running (e.g. after a host reboot)
// and re-runs post-start steps. `docker start` on a running container is a no-op,
// so this is safe to call on every reconcile.
func (p *Provider) Ensure(ctx context.Context, inst *domain.Instance) error {
	return p.Start(ctx, inst)
}

func (p *Provider) Stop(ctx context.Context, inst *domain.Instance) error {
	if _, err := p.docker(ctx, "stop", container(inst)); err != nil {
		return err
	}
	inst.Status = domain.StatusStopped
	inst.UpdatedAt = time.Now()
	return nil
}

func (p *Provider) Destroy(ctx context.Context, inst *domain.Instance, purge bool) error {
	args := []string{"rm", "-f"}
	if purge {
		args = append(args, "-v") // also remove anonymous volumes
	}
	args = append(args, container(inst))
	_, err := p.docker(ctx, args...)
	return err
}

func (p *Provider) Logs(ctx context.Context, inst *domain.Instance, follow bool, w io.Writer) error {
	args := []string{"logs"}
	if follow {
		args = append(args, "-f")
	} else {
		args = append(args, "--tail", "200")
	}
	args = append(args, container(inst))
	return dockercli.Stream(ctx, w, args...)
}

// --- vault ---

// vaultServerConfig is the server config handed to the official image through
// VAULT_LOCAL_CONFIG. mlock is disabled because containers rarely grant IPC_LOCK.
const vaultServerConfig = `{"storage":{"file":{"path":"/vault/file"}},` +
	`"listener":[{"tcp":{"address":"0.0.0.0:8200","tls_disable":true}}],` +
	`"disable_mlock":true,"ui":true,"api_addr":"http://127.0.0.1:8200"}`

// vaultStartTimeout covers a cold container start (the image is already pulled).
const vaultStartTimeout = 60 * time.Second

// bootstrapVault initializes a brand-new server, unseals it and enables the KV v2
// mount. The single unseal key and the root token are stored on the instance:
// acceptable for a local dev server, never for a real one.
func bootstrapVault(ctx context.Context, inst *domain.Instance, spec domain.Spec) error {
	ep := inst.Endpoints[domain.RoleDefault]
	c := vaultx.New(fmt.Sprintf("http://%s:%d", ep.Host, ep.Port), "")
	if _, err := c.WaitReady(ctx, vaultStartTimeout); err != nil {
		return err
	}
	key, token, err := c.Init(ctx)
	if err != nil {
		return err
	}
	if err := c.Unseal(ctx, key); err != nil {
		return err
	}
	mount := strings.Trim(spec.Params["mount"], "/")
	if mount == "" {
		mount = "secret"
	}
	c.Token = token
	if err := c.EnableKV2(ctx, mount); err != nil {
		return fmt.Errorf("enable kv v2 at %s: %w", mount, err)
	}
	inst.Cred = domain.Credential{Token: token, Extra: map[string]string{"unseal_key": key}}
	inst.Resources["mount"] = mount
	return nil
}

// afterStart runs per-kind steps once a container is (re)started. Vault comes
// back sealed after every restart, so it must be unsealed before it is usable.
func afterStart(ctx context.Context, inst *domain.Instance) error {
	if inst.Kind != domain.KindVault {
		return nil
	}
	key := inst.Cred.Extra["unseal_key"]
	if key == "" {
		return fmt.Errorf("vault %s: no unseal key recorded", inst.Name)
	}
	ep := inst.Endpoints[domain.RoleDefault]
	c := vaultx.New(fmt.Sprintf("http://%s:%d", ep.Host, ep.Port), "")
	st, err := c.WaitReady(ctx, vaultStartTimeout)
	if err != nil {
		return err
	}
	if !st.Sealed {
		return nil
	}
	return c.Unseal(ctx, key)
}

// --- helpers ---

func (p *Provider) baseRun(name string, hostPort, containerPort int, imageAndArgs ...string) []string {
	args := []string{
		"run", "-d",
		"--name", name,
		"--network", p.cfg.DockerNetwork,
		"--label", "managed-by=keel",
		"-p", fmt.Sprintf("127.0.0.1:%d:%d", hostPort, containerPort),
	}
	return append(args, imageAndArgs...)
}

// image resolves the container image: an explicit `image` wins, else a tag from
// `version` (or `engine_version`, the AWS-side spelling, so the same manifest
// works with either driver).
func (p *Provider) image(bp domain.Blueprint, spec domain.Spec) string {
	if v := strings.TrimSpace(spec.Params["image"]); v != "" {
		return v
	}
	tag := strings.TrimSpace(spec.Params["version"])
	if tag == "" {
		tag = strings.TrimSpace(spec.Params["engine_version"])
	}
	if tag != "" {
		if base, _, ok := strings.Cut(bp.Image, ":"); ok {
			return base + ":" + tag
		}
		return bp.Image + ":" + tag
	}
	return bp.Image
}

func (p *Provider) ensureNetwork(ctx context.Context) error {
	return dockercli.EnsureNetwork(ctx, p.cfg.DockerNetwork)
}

func (p *Provider) docker(ctx context.Context, args ...string) (string, error) {
	return dockercli.Run(ctx, args...)
}

func container(inst *domain.Instance) string {
	if c := inst.Resources["container"]; c != "" {
		return c
	}
	return namePrefix + sanitize(inst.Name)
}

func dbName(spec domain.Spec) string {
	if v := spec.Params["dbname"]; v != "" {
		return v
	}
	return sanitize(spec.Name)
}

func sanitize(name string) string {
	var sb strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_', r == '-', r == '.':
			sb.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			sb.WriteRune(r + 32)
		default:
			sb.WriteRune('-')
		}
	}
	return sb.String()
}

func randHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "000000"
	}
	return hex.EncodeToString(b)
}
