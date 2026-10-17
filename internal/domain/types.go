// Package domain holds the pure, dependency-free types that describe the
// infrastructure catalog and running instances. Per CONNECTION_INJECTION_PATTERN.md
// this layer must not import any infrastructure SDK (AWS, Docker, DB drivers) — it
// only speaks in primitive types so every other layer can depend on it freely.
package domain

import "time"

// Kind identifies a catalog blueprint (an engine we can spin up).
type Kind string

const (
	KindPostgres       Kind = "postgres"
	KindRedis          Kind = "redis"
	KindKafka          Kind = "kafka"
	KindSchemaRegistry Kind = "schema-registry"
	KindS3             Kind = "s3"
	KindSNS            Kind = "sns"
	KindSQS            Kind = "sqs"
	KindSecrets        Kind = "secrets"
	KindDynamoDB       Kind = "dynamodb"
	KindVault          Kind = "vault"
	KindGCPIdentity    Kind = "gcp-identity"

	// Deferred / legacy engines (M4, catalog-only for now).
	KindOracle    Kind = "oracle"
	KindCassandra Kind = "cassandra"
	KindRabbitMQ  Kind = "rabbitmq"
)

// ProviderName identifies which driver materializes an instance.
type ProviderName string

const (
	// ProviderAWS provisions AWS-shaped resources through the floci emulator.
	ProviderAWS ProviderName = "aws-floci"
	// ProviderDocker runs non-AWS engines as plain Docker containers.
	ProviderDocker ProviderName = "docker"
	// ProviderGCP runs Google Cloud emulators (e.g. the Identity Platform /
	// Firebase Auth emulator) as Docker containers.
	ProviderGCP ProviderName = "gcp-emulator"
	// ProviderMock simulates everything in-memory (no Docker required). Used for
	// local development, tests, and demos before Docker/floci are available.
	ProviderMock ProviderName = "mock"
)

// Role distinguishes multiple connection endpoints of the same engine, honoring
// the pattern's "un cliente por rol, no por servicio" rule (e.g. write vs read).
type Role string

const (
	RoleDefault Role = ""      // engines with a single endpoint
	RoleWrite   Role = "write" // primary / read-write endpoint
	RoleRead    Role = "read"  // read replica endpoint
	RoleUI      Role = "ui"    // human-facing web console (not a client endpoint)
)

func (r Role) String() string {
	if r == RoleDefault {
		return "default"
	}
	return string(r)
}

// Status is the lifecycle state of an instance.
type Status string

const (
	StatusPending Status = "pending"
	StatusRunning Status = "running"
	StatusStopped Status = "stopped"
	StatusError   Status = "error"
	StatusUnknown Status = "unknown"
)

// Category groups blueprints for display in the catalog/TUI.
type Category string

const (
	CategoryDatabase  Category = "database"
	CategoryCache     Category = "cache"
	CategoryMessaging Category = "messaging"
	CategoryStorage   Category = "storage"
	CategorySecurity  Category = "security"
	CategoryIdentity  Category = "identity"
)

// Cloud names the reference cloud a blueprint mirrors. Engines that are not a
// managed service of any cloud (e.g. Vault) use CloudNone.
type Cloud string

const (
	CloudNone Cloud = ""
	CloudAWS  Cloud = "aws"
	CloudGCP  Cloud = "gcp"
)

// String renders the cloud for display ("-" when cloud-agnostic).
func (c Cloud) String() string {
	if c == CloudNone {
		return "-"
	}
	return string(c)
}

// Param declares one engine-specific option a blueprint accepts. The catalog is
// the single source of truth: the CLI validates against these, the TUI builds its
// create form from them and the docs render them — so every surface stays in sync
// by construction instead of by hand.
type Param struct {
	Key       string
	Label     string
	Help      string
	Default   string
	Enum      []string // allowed values; empty means free-form
	Required  bool
	Multiline bool
	Providers []ProviderName // drivers this applies to; empty means all
}

// AppliesTo reports whether the param is meaningful for a driver.
func (p Param) AppliesTo(provider ProviderName) bool {
	if len(p.Providers) == 0 {
		return true
	}
	for _, pr := range p.Providers {
		if pr == provider {
			return true
		}
	}
	return false
}

// Blueprint is a catalog entry: the template for an engine we can instantiate.
type Blueprint struct {
	Kind         Kind
	DisplayName  string
	Cloud        Cloud  // reference cloud this engine mirrors (aws, gcp, or none)
	CloudService string // parity target (e.g. "Amazon RDS / Aurora PostgreSQL")
	Category     Category
	Provider     ProviderName
	Image        string // backing container image (informational / docker driver)
	DefaultPort  int    // conventional engine port
	Roles        []Role // roles this engine exposes; nil => RoleDefault only
	Params       []Param
	Description  string
	Deferred     bool // true for legacy engines not yet fully wired
}

// HasRoles reports whether the blueprint exposes distinct read/write roles.
func (b Blueprint) HasRoles() bool { return len(b.Roles) > 0 }

// Spec is a request to create a named instance from a blueprint.
type Spec struct {
	Name   string            // unique instance name chosen by the user
	Kind   Kind              // which blueprint
	Roles  []Role            // requested roles (defaults to the blueprint's roles)
	Params map[string]string // engine-specific overrides (version, dbname, topics, ...)
}

// Endpoint is a single reachable address for an instance/role. It carries both a
// host-mode address (localhost + mapped port, for apps running on the host) and a
// docker-mode address (in-network hostname + port, for containerized apps).
type Endpoint struct {
	Host       string `json:"host"`
	Port       int    `json:"port"`
	DockerHost string `json:"docker_host,omitempty"`
	DockerPort int    `json:"docker_port,omitempty"`
}

// Credential holds the access material for an instance (local dev only).
type Credential struct {
	Username string            `json:"username,omitempty"`
	Password string            `json:"password,omitempty"`
	Token    string            `json:"token,omitempty"`
	Extra    map[string]string `json:"extra,omitempty"`
}

// Instance is a provisioned engine tracked in the registry (single source of truth).
type Instance struct {
	Name         string       `json:"name"`
	Kind         Kind         `json:"kind"`
	Provider     ProviderName `json:"provider"`
	CloudService string       `json:"cloud_service"`
	// LegacyAWSService reads state written before multi-cloud support; the
	// registry migrates it into CloudService on load.
	LegacyAWSService string            `json:"aws_service,omitempty"`
	Status           Status            `json:"status"`
	Endpoints        map[Role]Endpoint `json:"endpoints"`
	Cred             Credential        `json:"credential"`
	Resources        map[string]string `json:"resources,omitempty"` // driver bookkeeping (containerID, rdsID, arn...)
	Params           map[string]string `json:"params,omitempty"`
	Notes            string            `json:"notes,omitempty"`
	CreatedAt        time.Time         `json:"created_at"`
	UpdatedAt        time.Time         `json:"updated_at"`
}

// PrimaryEndpoint returns the write endpoint if present, else the default/first.
func (i *Instance) PrimaryEndpoint() (Endpoint, bool) {
	if ep, ok := i.Endpoints[RoleWrite]; ok {
		return ep, true
	}
	if ep, ok := i.Endpoints[RoleDefault]; ok {
		return ep, true
	}
	for _, ep := range i.Endpoints {
		return ep, true
	}
	return Endpoint{}, false
}
