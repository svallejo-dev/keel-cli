// Package ops is the component-operations layer: it exposes typed sub-resource
// and data operations per engine (Kafka topics, Postgres databases/SQL, Redis
// keys, S3 objects, SNS/SQS messages, secret values, Vault KV, identity users). Both the CLI commands and
// the TUI drive these same operations, so the two interfaces stay at parity by
// construction.
package ops

import (
	"errors"

	"github.com/svallejo-dev/keel-cli/internal/awsx"
	"github.com/svallejo-dev/keel-cli/internal/config"
	"github.com/svallejo-dev/keel-cli/internal/domain"
)

// ErrMock is returned when an operation is attempted on a simulated instance.
var ErrMock = errors.New("operation not available on a mock instance (start Docker + floci for real infra)")

// Registry groups the per-engine operation helpers.
type Registry struct {
	Postgres *PostgresOps
	Redis    *RedisOps
	Kafka    *KafkaOps
	S3       *S3Ops
	SNS      *SNSOps
	SQS      *SQSOps
	Secrets  *SecretsOps
	Vault    *VaultOps
	Identity *IdentityOps
}

// New builds the operation helpers. AWS clients target floci; engine clients are
// built per call from each instance's endpoint.
func New(cfg config.Config) (*Registry, error) {
	cl, err := awsx.New(cfg)
	if err != nil {
		return nil, err
	}
	return &Registry{
		Postgres: &PostgresOps{},
		Redis:    &RedisOps{},
		Kafka:    &KafkaOps{network: cfg.DockerNetwork},
		S3:       &S3Ops{c: cl.S3},
		SNS:      &SNSOps{c: cl.SNS},
		SQS:      &SQSOps{c: cl.SQS},
		Secrets:  &SecretsOps{c: cl.Secrets},
		Vault:    &VaultOps{},
		Identity: &IdentityOps{},
	}, nil
}

// Capability names a group of operations an engine supports (for menus/help).
type Capability struct {
	Group string // stable id: topics, produce, consume, databases, sql, keys, objects, publish, messages, secret, kv, users, token
	Label string // human label
}

// Capabilities returns the operation groups available for a kind.
func Capabilities(kind domain.Kind) []Capability {
	switch kind {
	case domain.KindKafka:
		return []Capability{{"topics", "Topics"}, {"produce", "Produce"}, {"consume", "Consume"}}
	case domain.KindPostgres:
		return []Capability{{"databases", "Databases"}, {"sql", "SQL query"}}
	case domain.KindRedis:
		return []Capability{{"keys", "Keys"}}
	case domain.KindS3:
		return []Capability{{"objects", "Objects"}}
	case domain.KindSNS:
		return []Capability{{"publish", "Publish"}, {"subscriptions", "Subscriptions"}}
	case domain.KindSQS:
		return []Capability{{"messages", "Messages"}}
	case domain.KindSecrets:
		return []Capability{{"secret", "Secret value"}}
	case domain.KindVault:
		return []Capability{{"kv", "KV secrets"}}
	case domain.KindGCPIdentity:
		return []Capability{{"users", "Users"}, {"token", "Sign in (ID token)"}}
	default:
		return nil
	}
}

// HasOps reports whether a kind exposes any component operations.
func HasOps(kind domain.Kind) bool { return len(Capabilities(kind)) > 0 }

// requireReal guards against operating on a simulated instance.
func requireReal(inst *domain.Instance) error {
	if inst.Provider == domain.ProviderMock {
		return ErrMock
	}
	return nil
}
