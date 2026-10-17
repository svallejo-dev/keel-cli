// Package mock provides an in-memory Provider that simulates provisioning without
// Docker or floci. It lets the whole CLI/TUI be exercised end-to-end before the
// real substrate is installed, and doubles as the test double for the control layer.
package mock

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"time"

	"github.com/svallejo-dev/keel-cli/internal/domain"
	"github.com/svallejo-dev/keel-cli/internal/provider"
)

// FlociGatewayPort is the AWS-API endpoint the real floci emulator listens on;
// mock mirrors it so emitted connection config matches the real thing.
const FlociGatewayPort = 4566

// Provider is the mock driver.
type Provider struct {
	// Region reported for AWS-emulated services.
	Region string
	// now is injectable for deterministic tests; defaults to time.Now.
	now func() time.Time
}

// New returns a mock provider.
func New() *Provider {
	return &Provider{Region: "us-east-1", now: time.Now}
}

func (p *Provider) Name() domain.ProviderName { return domain.ProviderMock }

func (p *Provider) clock() time.Time {
	if p.now != nil {
		return p.now()
	}
	return time.Now()
}

// Provision simulates creating the resource and returns a running instance.
func (p *Provider) Provision(_ context.Context, spec domain.Spec, bp domain.Blueprint, alloc provider.PortAllocator) (*domain.Instance, error) {
	roles := spec.Roles
	if len(roles) == 0 {
		roles = bp.Roles
	}
	if len(roles) == 0 {
		roles = []domain.Role{domain.RoleDefault}
	}

	now := p.clock()
	inst := &domain.Instance{
		Name:         spec.Name,
		Kind:         bp.Kind,
		Provider:     domain.ProviderMock,
		CloudService: bp.CloudService,
		Status:       domain.StatusRunning,
		Endpoints:    map[domain.Role]domain.Endpoint{},
		Resources:    map[string]string{},
		Params:       spec.Params,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	if isAWSEmulated(bp.Kind) {
		// AWS-emulated services share the floci gateway; there is no per-instance
		// engine port. Record an ARN-like resource id instead.
		inst.Endpoints[domain.RoleDefault] = domain.Endpoint{
			Host: "localhost", Port: FlociGatewayPort,
			DockerHost: "floci", DockerPort: FlociGatewayPort,
		}
		inst.Resources["arn"] = fmt.Sprintf("arn:aws:%s:%s:000000000000:%s", awsNamespace(bp.Kind), p.Region, spec.Name)
		inst.Resources["endpoint_url"] = fmt.Sprintf("http://localhost:%d", FlociGatewayPort)
		if bp.Kind == domain.KindDynamoDB {
			inst.Resources["table"] = spec.Name
			pk := spec.Params["pk"]
			if pk == "" {
				pk = "id"
			}
			inst.Resources["partition_key"] = pk
			if sk := spec.Params["sk"]; sk != "" {
				inst.Resources["sort_key"] = sk
			}
		}
		return inst, nil
	}

	// Engine-backed services (postgres/redis/kafka/schema-registry): one host port
	// per role.
	for _, role := range roles {
		port, err := alloc.Allocate()
		if err != nil {
			return nil, fmt.Errorf("mock: allocate port for %s/%s: %w", spec.Name, role, err)
		}
		inst.Endpoints[role] = domain.Endpoint{
			Host:       "localhost",
			Port:       port,
			DockerHost: dockerHost(spec.Name, role),
			DockerPort: bp.DefaultPort,
		}
	}
	inst.Cred = mockCredential(bp.Kind, spec)
	inst.Resources["container_id"] = "mock-" + randHex(6)
	switch bp.Kind {
	case domain.KindVault:
		mount := spec.Params["mount"]
		if mount == "" {
			mount = "secret"
		}
		inst.Resources["mount"] = mount
	case domain.KindGCPIdentity:
		project := spec.Params["project"]
		if project == "" {
			project = "demo-keel"
		}
		inst.Resources["project"] = project
	}
	return inst, nil
}

func (p *Provider) Start(_ context.Context, inst *domain.Instance) error {
	inst.Status = domain.StatusRunning
	inst.UpdatedAt = p.clock()
	return nil
}

func (p *Provider) Stop(_ context.Context, inst *domain.Instance) error {
	inst.Status = domain.StatusStopped
	inst.UpdatedAt = p.clock()
	return nil
}

func (p *Provider) Destroy(_ context.Context, _ *domain.Instance, _ bool) error { return nil }

func (p *Provider) Logs(_ context.Context, inst *domain.Instance, _ bool, w io.Writer) error {
	_, err := fmt.Fprintf(w, "[mock] %s (%s) is simulated; no real logs available\n", inst.Name, inst.Kind)
	return err
}

// --- helpers ---

func isAWSEmulated(k domain.Kind) bool {
	switch k {
	case domain.KindS3, domain.KindSNS, domain.KindSQS, domain.KindSecrets, domain.KindDynamoDB:
		return true
	default:
		return false
	}
}

func awsNamespace(k domain.Kind) string {
	switch k {
	case domain.KindS3:
		return "s3"
	case domain.KindSNS:
		return "sns"
	case domain.KindSQS:
		return "sqs"
	case domain.KindSecrets:
		return "secretsmanager"
	case domain.KindDynamoDB:
		return "dynamodb"
	default:
		return string(k)
	}
}

func dockerHost(name string, role domain.Role) string {
	if role == domain.RoleDefault {
		return name
	}
	return fmt.Sprintf("%s-%s", name, role)
}

func mockCredential(k domain.Kind, spec domain.Spec) domain.Credential {
	switch k {
	case domain.KindPostgres:
		db := spec.Params["dbname"]
		if db == "" {
			db = spec.Name
		}
		return domain.Credential{
			Username: "infra",
			Password: "infra_" + randHex(4),
			Extra:    map[string]string{"dbname": db},
		}
	case domain.KindRedis:
		return domain.Credential{Password: ""} // ElastiCache local default: no auth
	case domain.KindVault:
		return domain.Credential{Token: "mock-root-token"}
	case domain.KindGCPIdentity:
		return domain.Credential{Extra: map[string]string{"api_key": "fake-api-key"}}
	case domain.KindKafka, domain.KindSchemaRegistry:
		return domain.Credential{}
	default:
		return domain.Credential{}
	}
}

func randHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "000000"
	}
	return hex.EncodeToString(b)
}
