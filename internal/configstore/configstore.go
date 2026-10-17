// Package configstore is where a project's own application configuration lives
// during development: the values its use cases need (feature flags, limits,
// third-party keys…), as opposed to the connection details keel emits for the
// infrastructure. Two backends are supported, chosen per project in keel.yaml:
//
//   - vault: one KV v2 secret (mount/path) holding KEY → value.
//   - aws-secretsmanager: one secret whose value is a JSON object KEY → value.
//
// An app either reads the store itself at startup (the default — same code path
// as QA/prod) using the bootstrap variables from BootstrapEnv, or `keel sync`
// resolves the values into the .env when the project opts into `resolve`.
package configstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	smtypes "github.com/aws/aws-sdk-go-v2/service/secretsmanager/types"

	"github.com/svallejo-dev/keel-cli/internal/awsx"
	"github.com/svallejo-dev/keel-cli/internal/config"
	"github.com/svallejo-dev/keel-cli/internal/domain"
	"github.com/svallejo-dev/keel-cli/internal/vaultx"
)

// Backend identifiers. They are also the values of CONFIG_STORE, the variable
// an app reads to pick its loader — a contract shared with the generated code.
const (
	BackendVault      = "vault"
	BackendAWSSecrets = "aws-secretsmanager"
)

// Store reads and writes one project's configuration as a flat KEY → value map.
type Store interface {
	// Backend returns BackendVault or BackendAWSSecrets.
	Backend() string
	// Location is a human-readable address, e.g. vault://secret/orders.
	Location() string
	// Read returns the stored values; an empty map when nothing is stored yet.
	Read(ctx context.Context) (map[string]string, error)
	// Write replaces the stored values with values.
	Write(ctx context.Context, values map[string]string) error
	// BootstrapEnv lists the variables an app needs to find this store, in
	// emission order. Connection variables (VAULT_ADDR, AWS_*) come from the
	// store instance itself and are not repeated here.
	BootstrapEnv() [][2]string
}

// ErrUnsupported is returned for instances that cannot hold configuration.
var ErrUnsupported = errors.New("a config store must be a vault or secrets instance")

// Open returns the store backed by inst. path is the Vault KV path (required for
// vault) or the Secrets Manager secret id (default: the instance's own secret).
func Open(cfg config.Config, inst *domain.Instance, path string) (Store, error) {
	if inst.Provider == domain.ProviderMock {
		return nil, fmt.Errorf("config store %q is simulated (mock engine) — start Docker for a real store", inst.Name)
	}
	path = strings.Trim(strings.TrimSpace(path), "/")
	switch inst.Kind {
	case domain.KindVault:
		if path == "" {
			return nil, fmt.Errorf("a Vault config store needs a path (e.g. the project name)")
		}
		ep, ok := inst.PrimaryEndpoint()
		if !ok {
			return nil, fmt.Errorf("vault %q has no endpoint", inst.Name)
		}
		mount := inst.Resources["mount"]
		if mount == "" {
			mount = "secret"
		}
		return &vaultStore{
			c:     vaultx.New(fmt.Sprintf("http://%s:%d", ep.Host, ep.Port), inst.Cred.Token),
			mount: mount,
			path:  path,
		}, nil
	case domain.KindSecrets:
		if path == "" {
			path = inst.Name
		}
		cl, err := awsx.New(cfg)
		if err != nil {
			return nil, err
		}
		return &awsStore{c: cl.Secrets, id: path}, nil
	default:
		return nil, fmt.Errorf("%q is %s: %w", inst.Name, inst.Kind, ErrUnsupported)
	}
}

// --- vault ---

type vaultStore struct {
	c     *vaultx.Client
	mount string
	path  string
}

func (s *vaultStore) Backend() string  { return BackendVault }
func (s *vaultStore) Location() string { return "vault://" + s.mount + "/" + s.path }

func (s *vaultStore) Read(ctx context.Context) (map[string]string, error) {
	v, err := s.c.KVGet(ctx, s.mount, s.path)
	if errors.Is(err, vaultx.ErrNotFound) {
		return map[string]string{}, nil
	}
	return v, err
}

func (s *vaultStore) Write(ctx context.Context, values map[string]string) error {
	return s.c.KVPut(ctx, s.mount, s.path, values)
}

func (s *vaultStore) BootstrapEnv() [][2]string {
	return [][2]string{
		{"CONFIG_STORE", BackendVault},
		{"CONFIG_VAULT_MOUNT", s.mount},
		{"CONFIG_VAULT_PATH", s.path},
	}
}

// --- aws secrets manager ---

type awsStore struct {
	c  *secretsmanager.Client
	id string
}

func (s *awsStore) Backend() string  { return BackendAWSSecrets }
func (s *awsStore) Location() string { return "secretsmanager://" + s.id }

func (s *awsStore) Read(ctx context.Context) (map[string]string, error) {
	out, err := s.c.GetSecretValue(ctx, &secretsmanager.GetSecretValueInput{SecretId: aws.String(s.id)})
	var nf *smtypes.ResourceNotFoundException
	if errors.As(err, &nf) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, err
	}
	return DecodeJSON(aws.ToString(out.SecretString))
}

func (s *awsStore) Write(ctx context.Context, values map[string]string) error {
	body, err := json.Marshal(values) // map keys marshal sorted: stable diffs
	if err != nil {
		return err
	}
	_, err = s.c.PutSecretValue(ctx, &secretsmanager.PutSecretValueInput{
		SecretId:     aws.String(s.id),
		SecretString: aws.String(string(body)),
	})
	var nf *smtypes.ResourceNotFoundException
	if errors.As(err, &nf) {
		_, err = s.c.CreateSecret(ctx, &secretsmanager.CreateSecretInput{
			Name:         aws.String(s.id),
			Description:  aws.String("application configuration (managed by keel config)"),
			SecretString: aws.String(string(body)),
		})
	}
	return err
}

func (s *awsStore) BootstrapEnv() [][2]string {
	return [][2]string{
		{"CONFIG_STORE", BackendAWSSecrets},
		{"CONFIG_SECRET_ID", s.id},
	}
}

// DecodeJSON parses a secret value holding a flat JSON object. Non-string values
// are kept as their JSON text so they survive a round-trip through env vars.
func DecodeJSON(raw string) (map[string]string, error) {
	out := map[string]string{}
	if strings.TrimSpace(raw) == "" {
		return out, nil
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return nil, fmt.Errorf("secret value is not a JSON object of KEY/value pairs: %w", err)
	}
	for k, v := range m {
		switch t := v.(type) {
		case string:
			out[k] = t
		case nil:
			out[k] = ""
		default:
			b, _ := json.Marshal(t)
			out[k] = string(b)
		}
	}
	return out, nil
}

// --- read-modify-write helpers shared by the CLI and sync ---

// Set merges values into the store and returns the keys whose value changed.
func Set(ctx context.Context, s Store, values map[string]string) ([]string, error) {
	cur, err := s.Read(ctx)
	if err != nil {
		return nil, err
	}
	var changed []string
	for k, v := range values {
		if old, ok := cur[k]; !ok || old != v {
			cur[k] = v
			changed = append(changed, k)
		}
	}
	sort.Strings(changed)
	if len(changed) == 0 {
		return nil, nil
	}
	return changed, s.Write(ctx, cur)
}

// Unset removes keys and returns the ones that existed.
func Unset(ctx context.Context, s Store, keys []string) ([]string, error) {
	cur, err := s.Read(ctx)
	if err != nil {
		return nil, err
	}
	var removed []string
	for _, k := range keys {
		if _, ok := cur[k]; ok {
			delete(cur, k)
			removed = append(removed, k)
		}
	}
	sort.Strings(removed)
	if len(removed) == 0 {
		return nil, nil
	}
	return removed, s.Write(ctx, cur)
}

// Seed writes only the keys the store does not have yet, so values a developer
// changed in the store are never clobbered by the seed file. It returns the keys
// it added.
func Seed(ctx context.Context, s Store, values map[string]string) ([]string, error) {
	cur, err := s.Read(ctx)
	if err != nil {
		return nil, err
	}
	var added []string
	for k, v := range values {
		if _, ok := cur[k]; !ok {
			cur[k] = v
			added = append(added, k)
		}
	}
	sort.Strings(added)
	if len(added) == 0 {
		return nil, nil
	}
	return added, s.Write(ctx, cur)
}

// SortedKeys returns m's keys in order.
func SortedKeys(m map[string]string) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}
