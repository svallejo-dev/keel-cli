// Package catalog is the source of truth for the infrastructure blueprints the
// platform can spin up. The initial set is derived from real architecture
// assessments and mirrors AWS managed services;
// GCP services are added as the projects move there, and cloud-agnostic engines
// (Vault) carry no cloud at all.
package catalog

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/svallejo-dev/keel-cli/internal/domain"
)

// Shared param sets. Declared once so every engine that supports them stays
// consistent (and the docker-only ones never show up for floci-backed engines).
var (
	dockerImageParams = []domain.Param{
		{Key: "image", Label: "Container image", Help: "override the backing image", Providers: []domain.ProviderName{domain.ProviderDocker}},
		{Key: "version", Label: "Image tag", Help: "tag for the default image (e.g. 16)", Providers: []domain.ProviderName{domain.ProviderDocker}},
	}
	attrTypeEnum = []string{"S", "N", "B"}
	boolEnum     = []string{"true", "false"}
)

// blueprints is the registered catalog, keyed by Kind.
var blueprints = map[domain.Kind]domain.Blueprint{
	domain.KindPostgres: {
		Kind:         domain.KindPostgres,
		DisplayName:  "PostgreSQL",
		Cloud:        domain.CloudAWS,
		CloudService: "Amazon RDS / Aurora PostgreSQL",
		Category:     domain.CategoryDatabase,
		Provider:     domain.ProviderAWS,
		Image:        "postgres:16",
		DefaultPort:  5432,
		Roles:        []domain.Role{domain.RoleWrite, domain.RoleRead},
		Params: append([]domain.Param{
			{Key: "dbname", Label: "Database name", Help: "initial database (default: the instance name)"},
			{Key: "engine_version", Label: "Engine version", Help: "postgres version, e.g. 16"},
			{Key: "instance_class", Label: "Instance class", Default: "db.t3.micro", Providers: []domain.ProviderName{domain.ProviderAWS}},
			{Key: "storage", Label: "Allocated storage (GB)", Default: "20", Providers: []domain.ProviderName{domain.ProviderAWS}},
		}, dockerImageParams...),
		Description: "Relational database via floci RDS (real postgres). Write & read roles; each instance gets an isolated RDS instance. Override with --provider docker.",
	},
	domain.KindRedis: {
		Kind:         domain.KindRedis,
		DisplayName:  "Redis",
		Cloud:        domain.CloudAWS,
		CloudService: "Amazon ElastiCache for Redis",
		Category:     domain.CategoryCache,
		Provider:     domain.ProviderAWS,
		Image:        "redis:7",
		DefaultPort:  6379,
		Params: append([]domain.Param{
			{Key: "node_type", Label: "Cache node type", Default: "cache.t3.micro", Providers: []domain.ProviderName{domain.ProviderAWS}},
			{Key: "engine_version", Label: "Engine version", Help: "redis version, e.g. 7.1", Providers: []domain.ProviderName{domain.ProviderAWS}},
		}, dockerImageParams...),
		Description: "In-memory cache via floci ElastiCache (real redis/valkey). Override with --provider docker.",
	},
	domain.KindKafka: {
		Kind:         domain.KindKafka,
		DisplayName:  "Kafka",
		Cloud:        domain.CloudAWS,
		CloudService: "Amazon MSK",
		Category:     domain.CategoryMessaging,
		Provider:     domain.ProviderAWS,
		Image:        "redpandadata/redpanda:latest",
		DefaultPort:  9092,
		Params: append([]domain.Param{
			{Key: "topics", Label: "Topics to pre-create", Help: "name[:partitions], comma-separated — e.g. pagos:3,envios:6"},
		}, dockerImageParams...),
		Description: "Distributed log via floci MSK. Best consumed in docker mode (broker not host-published); use --provider docker for full host access.",
	},
	domain.KindSchemaRegistry: {
		Kind:         domain.KindSchemaRegistry,
		DisplayName:  "Schema Registry",
		CloudService: "(no AWS-native equivalent — parity deviation)",
		Category:     domain.CategoryMessaging,
		Provider:     domain.ProviderDocker,
		Image:        "apicurio/apicurio-registry-mem:latest",
		DefaultPort:  8080,
		Params:       dockerImageParams,
		Description:  "Schema/contract registry, sidecar to a Kafka instance. Runs via direct Docker.",
	},
	domain.KindS3: {
		Kind:         domain.KindS3,
		DisplayName:  "S3",
		Cloud:        domain.CloudAWS,
		CloudService: "Amazon S3",
		Category:     domain.CategoryStorage,
		Provider:     domain.ProviderAWS,
		DefaultPort:  4566,
		Params: []domain.Param{
			{Key: "versioning", Label: "Object versioning", Enum: boolEnum, Default: "false", Help: "keep previous versions of objects"},
		},
		Description: "Object storage bucket (emulated by floci at the AWS endpoint).",
	},
	domain.KindSNS: {
		Kind:         domain.KindSNS,
		DisplayName:  "SNS",
		Cloud:        domain.CloudAWS,
		CloudService: "Amazon SNS",
		Category:     domain.CategoryMessaging,
		Provider:     domain.ProviderAWS,
		DefaultPort:  4566,
		Params: []domain.Param{
			{Key: "fifo", Label: "FIFO topic", Enum: boolEnum, Default: "false", Help: "ordered, exactly-once delivery (name gets a .fifo suffix)"},
		},
		Description: "Pub/Sub fan-out topic (emulated by floci).",
	},
	domain.KindSQS: {
		Kind:         domain.KindSQS,
		DisplayName:  "SQS",
		Cloud:        domain.CloudAWS,
		CloudService: "Amazon SQS (+ DLQ)",
		Category:     domain.CategoryMessaging,
		Provider:     domain.ProviderAWS,
		DefaultPort:  4566,
		Params: []domain.Param{
			{Key: "visibility_timeout", Label: "Visibility timeout (s)", Default: "30", Help: "how long a received message stays hidden"},
			{Key: "retention", Label: "Message retention (s)", Default: "345600", Help: "how long unread messages are kept (4 days)"},
			{Key: "dlq", Label: "Create dead-letter queue", Enum: boolEnum, Default: "true"},
			{Key: "max_receive_count", Label: "Max receives before DLQ", Default: "5", Help: "only used when dlq=true"},
			{Key: "fifo", Label: "FIFO queue", Enum: boolEnum, Default: "false", Help: "ordered delivery (name gets a .fifo suffix)"},
		},
		Description: "Message queue with dead-letter queue (emulated by floci).",
	},
	domain.KindSecrets: {
		Kind:         domain.KindSecrets,
		DisplayName:  "Secrets Manager",
		Cloud:        domain.CloudAWS,
		CloudService: "AWS Secrets Manager (+ KMS)",
		Category:     domain.CategorySecurity,
		Provider:     domain.ProviderAWS,
		DefaultPort:  4566,
		Params: []domain.Param{
			{Key: "value", Label: "Secret value", Help: "JSON or plain text (default: {})", Multiline: true},
			{Key: "description", Label: "Description", Help: "what this secret holds"},
		},
		Description: "Secret store for credentials and signing keys (emulated by floci).",
	},
	domain.KindDynamoDB: {
		Kind:         domain.KindDynamoDB,
		DisplayName:  "DynamoDB",
		Cloud:        domain.CloudAWS,
		CloudService: "Amazon DynamoDB",
		Category:     domain.CategoryDatabase,
		Provider:     domain.ProviderAWS,
		DefaultPort:  4566,
		Params: []domain.Param{
			{Key: "pk", Label: "Partition key", Default: "id", Help: "key attribute name"},
			{Key: "pktype", Label: "Partition key type", Enum: attrTypeEnum, Default: "S"},
			{Key: "sk", Label: "Sort key", Help: "optional"},
			{Key: "sktype", Label: "Sort key type", Enum: attrTypeEnum, Default: "S"},
			{Key: "gsi_name", Label: "GSI name", Help: "optional secondary index, e.g. byUser"},
			{Key: "gsi_pk", Label: "GSI partition key", Help: "required when gsi_name is set"},
			{Key: "gsi_pktype", Label: "GSI partition key type", Enum: attrTypeEnum, Default: "S"},
			{Key: "gsi_sk", Label: "GSI sort key", Help: "optional"},
			{Key: "gsi_sktype", Label: "GSI sort key type", Enum: attrTypeEnum, Default: "S"},
			{Key: "gsi_projection", Label: "GSI projection", Enum: []string{"ALL", "KEYS_ONLY"}, Default: "ALL"},
			{Key: "ttl", Label: "TTL attribute", Help: "attribute holding the expiry epoch (enables TTL)"},
			{Key: "read_capacity", Label: "Read capacity units", Default: "5"},
			{Key: "write_capacity", Label: "Write capacity units", Default: "5"},
		},
		Description: "Serverless NoSQL key-value/document table (emulated by floci). Supports a partition/sort key plus one global secondary index.",
	},
	domain.KindVault: {
		Kind:         domain.KindVault,
		DisplayName:  "HashiCorp Vault",
		CloudService: "HashiCorp Vault (KV v2) — cloud-agnostic",
		Category:     domain.CategorySecurity,
		Provider:     domain.ProviderDocker,
		Image:        "hashicorp/vault:2.1",
		DefaultPort:  8200,
		Params: append([]domain.Param{
			{Key: "mount", Label: "KV v2 mount", Default: "secret", Help: "path where the KV v2 engine is enabled"},
		}, dockerImageParams...),
		Description: "Secret and configuration store. Runs a persistent Vault server (file storage) that keel initializes and unseals for you; data survives restarts.",
	},
	domain.KindGCPIdentity: {
		Kind:         domain.KindGCPIdentity,
		DisplayName:  "Identity Platform",
		Cloud:        domain.CloudGCP,
		CloudService: "Google Cloud Identity Platform",
		Category:     domain.CategoryIdentity,
		Provider:     domain.ProviderGCP,
		// Built locally on first use from an embedded Dockerfile (pinned
		// firebase-tools); there is no official image for the auth emulator.
		Image:       "keel/gcp-identity-emulator:15.33.0",
		DefaultPort: 9099,
		Params: []domain.Param{
			{Key: "project", Label: "GCP project id", Help: "default: $KEEL_GCP_PROJECT or demo-keel (demo-* ids never reach real Google resources)"},
			{Key: "ui", Label: "Emulator UI", Enum: boolEnum, Default: "true", Help: "web console to browse users and tokens"},
		},
		Description: "Identity provider (users, sign-in, ID tokens, tenants) via the official Firebase Auth emulator, which speaks the Identity Platform API. Users persist across restarts.",
	},

	// --- Deferred / legacy engines (catalog-only until M4) ---
	domain.KindOracle: {
		Kind:         domain.KindOracle,
		DisplayName:  "Oracle",
		Cloud:        domain.CloudAWS,
		CloudService: "Amazon RDS for Oracle",
		Category:     domain.CategoryDatabase,
		Provider:     domain.ProviderDocker,
		Image:        "gvenzl/oracle-free:slim",
		DefaultPort:  1521,
		Description:  "Legacy relational engine (migration testing only).",
		Deferred:     true,
	},
	domain.KindCassandra: {
		Kind:         domain.KindCassandra,
		DisplayName:  "Cassandra",
		Cloud:        domain.CloudAWS,
		CloudService: "Amazon Keyspaces",
		Category:     domain.CategoryDatabase,
		Provider:     domain.ProviderDocker,
		Image:        "cassandra:5",
		DefaultPort:  9042,
		Description:  "Legacy wide-column store (migration testing only).",
		Deferred:     true,
	},
	domain.KindRabbitMQ: {
		Kind:         domain.KindRabbitMQ,
		DisplayName:  "RabbitMQ",
		Cloud:        domain.CloudAWS,
		CloudService: "Amazon MQ for RabbitMQ",
		Category:     domain.CategoryMessaging,
		Provider:     domain.ProviderDocker,
		Image:        "rabbitmq:3-management",
		DefaultPort:  5672,
		Description:  "Legacy message broker (migration testing only).",
		Deferred:     true,
	},
}

// Get returns the blueprint for a kind.
func Get(k domain.Kind) (domain.Blueprint, bool) {
	bp, ok := blueprints[k]
	return bp, ok
}

// All returns every blueprint sorted by kind for stable display.
func All() []domain.Blueprint {
	out := make([]domain.Blueprint, 0, len(blueprints))
	for _, bp := range blueprints {
		out = append(out, bp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Kind < out[j].Kind })
	return out
}

// Active returns non-deferred blueprints (the ones fully wired today).
func Active() []domain.Blueprint {
	var out []domain.Blueprint
	for _, bp := range All() {
		if !bp.Deferred {
			out = append(out, bp)
		}
	}
	return out
}

// ParamsFor returns the params a kind accepts, keeping only those that apply to
// the given driver. Pass an empty provider to get the blueprint's default driver.
func ParamsFor(k domain.Kind, provider domain.ProviderName) []domain.Param {
	bp, ok := blueprints[k]
	if !ok {
		return nil
	}
	if provider == "" {
		provider = bp.Provider
	}
	out := make([]domain.Param, 0, len(bp.Params))
	for _, p := range bp.Params {
		if p.AppliesTo(provider) {
			out = append(out, p)
		}
	}
	return out
}

// ValidateParams rejects unknown keys and values outside a param's Enum, so a
// typo fails loudly instead of being silently ignored.
func ValidateParams(k domain.Kind, params map[string]string) error {
	if len(params) == 0 {
		return nil
	}
	bp, ok := blueprints[k]
	if !ok {
		return fmt.Errorf("unknown blueprint %q", k)
	}
	known := make(map[string]domain.Param, len(bp.Params))
	for _, p := range bp.Params {
		known[p.Key] = p
	}
	for key, val := range params {
		p, ok := known[key]
		if !ok {
			return fmt.Errorf("unknown param %q for %s (accepted: %s)", key, k, strings.Join(paramKeys(bp), ", "))
		}
		if len(p.Enum) > 0 && val != "" && !slices.Contains(p.Enum, val) {
			return fmt.Errorf("invalid value %q for param %q (accepted: %s)", val, key, strings.Join(p.Enum, ", "))
		}
	}
	return nil
}

func paramKeys(bp domain.Blueprint) []string {
	ks := make([]string, 0, len(bp.Params))
	for _, p := range bp.Params {
		ks = append(ks, p.Key)
	}
	return ks
}

// Kinds returns all known kinds as strings (for CLI validation/help).
func Kinds() []string {
	ks := make([]string, 0, len(blueprints))
	for _, bp := range All() {
		ks = append(ks, string(bp.Kind))
	}
	return ks
}
