// Package inject renders resolved connection configuration for consuming apps.
// It is the platform's implementation of CONNECTION_INJECTION_PATTERN.md: it is
// the "delivery mechanism + input surface" that hands a process ready-to-use
// endpoints and secrets uniformly (env vars), with one variable set per role.
package inject

import (
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/svallejo-dev/keel-cli/internal/config"
	"github.com/svallejo-dev/keel-cli/internal/domain"
)

// Mode selects host-reachable vs docker-network-reachable addresses.
type Mode string

const (
	// ModeHost targets apps running on the host (localhost:mappedPort).
	ModeHost Mode = "host"
	// ModeDocker targets containerized apps sharing the docker network.
	ModeDocker Mode = "docker"
)

// Format selects the serialization of the emitted config.
type Format string

const (
	FormatDotenv Format = "dotenv" // KEY=VALUE
	FormatEnv    Format = "env"    // export KEY=VALUE
	FormatJSON   Format = "json"   // {"KEY":"VALUE"}
	FormatYAML   Format = "yaml"   // KEY: VALUE
	// FormatPowerShell emits $env:KEY = 'VALUE', for `keel inject ... | iex` on Windows.
	FormatPowerShell Format = "powershell"
)

// KV is one ordered environment variable.
type KV struct {
	Key   string
	Value string
}

// Bundle is an ordered set of connection variables.
type Bundle struct {
	Vars []KV
	cfg  config.Config // needed to rewrite floci-returned URLs per mode
}

func (b *Bundle) add(key, value string) { b.Vars = append(b.Vars, KV{key, value}) }

// Has reports whether key is already in the bundle.
func (b *Bundle) Has(key string) bool {
	for _, kv := range b.Vars {
		if kv.Key == key {
			return true
		}
	}
	return false
}

// Add appends key=value unless key is already present, and reports whether it
// was added. Connection variables are added first, so they win over application
// config that happens to reuse a name (e.g. AWS_REGION stored in Vault).
func (b *Bundle) Add(key, value string) bool {
	if b.Has(key) {
		return false
	}
	b.add(key, value)
	return true
}

// Build produces a connection Bundle for the given instances.
func Build(insts []*domain.Instance, cfg config.Config, mode Mode) *Bundle {
	b := &Bundle{cfg: cfg}
	awsEmitted, vaultEmitted, gcpEmitted := false, false, false
	for _, inst := range insts {
		if isAWSEmulated(inst.Kind) && !awsEmitted {
			// Shared AWS SDK config block, emitted once, so an app's AWS client
			// transparently talks to floci.
			b.add("AWS_ENDPOINT_URL", awsEndpoint(cfg, mode))
			b.add("AWS_REGION", cfg.Region)
			b.add("AWS_DEFAULT_REGION", cfg.Region)
			b.add("AWS_ACCESS_KEY_ID", cfg.AWSAccessKey)
			b.add("AWS_SECRET_ACCESS_KEY", cfg.AWSSecretKey)
			awsEmitted = true
		}
		if inst.Kind == domain.KindVault && !vaultEmitted {
			// The standard variables every Vault client (CLI, SDKs) reads. With
			// several Vaults, the first one is the default; each also gets its
			// own prefixed set below.
			host, port := addr(inst, domain.RoleDefault, mode)
			b.add("VAULT_ADDR", fmt.Sprintf("http://%s:%d", host, port))
			b.add("VAULT_TOKEN", inst.Cred.Token)
			vaultEmitted = true
		}
		if inst.Kind == domain.KindGCPIdentity && !gcpEmitted {
			// Standard GCP variables: the Admin SDKs (Go, Node, Java, Python)
			// switch to the emulator when FIREBASE_AUTH_EMULATOR_HOST is set and
			// talk to the real Identity Platform when it is absent (QA/prod).
			host, port := addr(inst, domain.RoleDefault, mode)
			project := inst.Resources["project"]
			b.add("GOOGLE_CLOUD_PROJECT", project)
			b.add("GCLOUD_PROJECT", project)
			b.add("FIREBASE_AUTH_EMULATOR_HOST", fmt.Sprintf("%s:%d", host, port))
			gcpEmitted = true
		}
		writeInstance(b, inst, mode)
	}
	return b
}

func writeInstance(b *Bundle, inst *domain.Instance, mode Mode) {
	p := envPrefix(inst.Name)
	switch inst.Kind {
	case domain.KindPostgres:
		writePostgres(b, p, inst, mode)
	case domain.KindRedis:
		writeRedis(b, p, inst, mode)
	case domain.KindKafka:
		host, port := addr(inst, domain.RoleDefault, mode)
		b.add(p+"_BROKERS", fmt.Sprintf("%s:%d", host, port))
		b.add(p+"_BOOTSTRAP_SERVERS", fmt.Sprintf("%s:%d", host, port))
	case domain.KindSchemaRegistry:
		host, port := addr(inst, domain.RoleDefault, mode)
		b.add(p+"_URL", fmt.Sprintf("http://%s:%d", host, port))
	case domain.KindS3:
		b.add(p+"_BUCKET", inst.Name)
	case domain.KindSNS:
		b.add(p+"_TOPIC_ARN", inst.Resources["arn"])
	case domain.KindSQS:
		b.add(p+"_QUEUE_URL", awsResourceURL(inst.Resources["queue_url"], b.cfg, mode))
		if dlq := inst.Resources["dlq_url"]; dlq != "" {
			b.add(p+"_DLQ_URL", awsResourceURL(dlq, b.cfg, mode))
		}
	case domain.KindSecrets:
		b.add(p+"_SECRET_ID", inst.Name)
		if arn := inst.Resources["arn"]; arn != "" {
			b.add(p+"_SECRET_ARN", arn)
		}
	case domain.KindVault:
		host, port := addr(inst, domain.RoleDefault, mode)
		b.add(p+"_ADDR", fmt.Sprintf("http://%s:%d", host, port))
		b.add(p+"_TOKEN", inst.Cred.Token)
		b.add(p+"_KV_MOUNT", inst.Resources["mount"])
	case domain.KindGCPIdentity:
		host, port := addr(inst, domain.RoleDefault, mode)
		b.add(p+"_PROJECT_ID", inst.Resources["project"])
		b.add(p+"_API_KEY", inst.Cred.Extra["api_key"])
		b.add(p+"_EMULATOR_HOST", fmt.Sprintf("%s:%d", host, port))
	case domain.KindDynamoDB:
		table := inst.Resources["table"]
		if table == "" {
			table = inst.Name
		}
		b.add(p+"_TABLE", table)
	default:
		host, port := addr(inst, domain.RoleDefault, mode)
		b.add(p+"_HOST", host)
		b.add(p+"_PORT", fmt.Sprintf("%d", port))
	}
}

func writePostgres(b *Bundle, p string, inst *domain.Instance, mode Mode) {
	db := inst.Cred.Extra["dbname"]
	if db == "" {
		db = inst.Name
	}
	// One variable set per role (write/read), honoring the pattern's rule.
	roles := sortedRoles(inst)
	for _, role := range roles {
		host, port := addr(inst, role, mode)
		suffix := roleSuffix(role)
		b.add(p+"_HOST"+suffix, host)
		b.add(p+"_PORT"+suffix, fmt.Sprintf("%d", port))
		dsn := fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=disable",
			inst.Cred.Username, inst.Cred.Password, host, port, db)
		b.add(p+"_DSN"+suffix, dsn)
	}
	b.add(p+"_USER", inst.Cred.Username)
	b.add(p+"_PASSWORD", inst.Cred.Password)
	b.add(p+"_DBNAME", db)
}

func writeRedis(b *Bundle, p string, inst *domain.Instance, mode Mode) {
	host, port := addr(inst, domain.RoleDefault, mode)
	b.add(p+"_HOST", host)
	b.add(p+"_PORT", fmt.Sprintf("%d", port))
	if inst.Cred.Password != "" {
		b.add(p+"_PASSWORD", inst.Cred.Password)
		b.add(p+"_URL", fmt.Sprintf("redis://:%s@%s:%d/0", inst.Cred.Password, host, port))
	} else {
		b.add(p+"_URL", fmt.Sprintf("redis://%s:%d/0", host, port))
	}
}

// Render serializes the bundle in the requested format.
func (b *Bundle) Render(format Format) (string, error) {
	switch format {
	case FormatDotenv:
		return b.renderLines("")
	case FormatEnv:
		return b.renderLines("export ")
	case FormatPowerShell:
		var sb strings.Builder
		for _, kv := range b.Vars {
			// Single quotes disable $ expansion; a literal ' is escaped by doubling it.
			fmt.Fprintf(&sb, "$env:%s = '%s'\n", kv.Key, strings.ReplaceAll(kv.Value, "'", "''"))
		}
		return sb.String(), nil
	case FormatJSON:
		m := make(map[string]string, len(b.Vars))
		for _, kv := range b.Vars {
			m[kv.Key] = kv.Value
		}
		out, err := json.MarshalIndent(m, "", "  ")
		if err != nil {
			return "", err
		}
		return string(out) + "\n", nil
	case FormatYAML:
		// Preserve order via a yaml.MapSlice-like node sequence.
		root := &yaml.Node{Kind: yaml.MappingNode}
		for _, kv := range b.Vars {
			root.Content = append(root.Content,
				&yaml.Node{Kind: yaml.ScalarNode, Value: kv.Key},
				&yaml.Node{Kind: yaml.ScalarNode, Value: kv.Value},
			)
		}
		out, err := yaml.Marshal(root)
		if err != nil {
			return "", err
		}
		return string(out), nil
	default:
		return "", fmt.Errorf("unknown format %q", format)
	}
}

func (b *Bundle) renderLines(prefix string) (string, error) {
	var sb strings.Builder
	for _, kv := range b.Vars {
		fmt.Fprintf(&sb, "%s%s=%s\n", prefix, kv.Key, quoteIfNeeded(kv.Value))
	}
	return sb.String(), nil
}

// --- helpers ---

func addr(inst *domain.Instance, role domain.Role, mode Mode) (string, int) {
	ep, ok := inst.Endpoints[role]
	if !ok {
		if e, ok2 := inst.PrimaryEndpoint(); ok2 {
			ep = e
		}
	}
	if mode == ModeDocker && ep.DockerHost != "" {
		return ep.DockerHost, ep.DockerPort
	}
	return ep.Host, ep.Port
}

func awsEndpoint(cfg config.Config, mode Mode) string {
	if mode == ModeDocker {
		return "http://floci:4566"
	}
	return cfg.FlociEndpoint
}

// awsResourceURL rewrites the host of a URL returned by floci (e.g. an SQS queue
// URL) so it matches the requested mode: the configured endpoint host in host
// mode (IPv4, avoiding the Windows localhost→::1 trap) and the docker network
// hostname in docker mode. Without this a container would get a localhost URL.
func awsResourceURL(raw string, cfg config.Config, mode Mode) string {
	if raw == "" {
		return raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return raw
	}
	target := awsEndpoint(cfg, mode)
	ep, err := url.Parse(target)
	if err != nil || ep.Host == "" {
		return raw
	}
	u.Scheme, u.Host = ep.Scheme, ep.Host
	return u.String()
}

func isAWSEmulated(k domain.Kind) bool {
	switch k {
	case domain.KindS3, domain.KindSNS, domain.KindSQS, domain.KindSecrets, domain.KindDynamoDB:
		return true
	default:
		return false
	}
}

func sortedRoles(inst *domain.Instance) []domain.Role {
	roles := make([]domain.Role, 0, len(inst.Endpoints))
	for r := range inst.Endpoints {
		roles = append(roles, r)
	}
	sort.Slice(roles, func(i, j int) bool { return roles[i] < roles[j] })
	return roles
}

func roleSuffix(role domain.Role) string {
	switch role {
	case domain.RoleWrite:
		return "_WRITE"
	case domain.RoleRead:
		return "_READ"
	default:
		return ""
	}
}

// envPrefix converts an instance name into an uppercase env-var-safe prefix.
func envPrefix(name string) string {
	var sb strings.Builder
	for _, r := range strings.ToUpper(name) {
		switch {
		case r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			sb.WriteRune(r)
		default:
			sb.WriteRune('_')
		}
	}
	out := sb.String()
	if out == "" || (out[0] >= '0' && out[0] <= '9') {
		out = "I_" + out
	}
	return out
}

func quoteIfNeeded(v string) string {
	if v == "" {
		return `""`
	}
	if strings.ContainsAny(v, " \t\"'$#\n") {
		v = strings.ReplaceAll(v, `"`, `\"`)
		v = strings.ReplaceAll(v, "\n", `\n`) // multi-line values (PEM keys, JSON)
		return `"` + v + `"`
	}
	return v
}
