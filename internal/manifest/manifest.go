// Package manifest loads declarative stack files: a named set of instances to
// provision together. Manifests are microservice-agnostic templates — they list
// infra components, not application services.
package manifest

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"github.com/svallejo-dev/keel-cli/internal/catalog"
	"github.com/svallejo-dev/keel-cli/internal/domain"
)

// ProjectFiles are the names `keel sync` looks for in the current directory,
// in order. A project manifest is a stack manifest plus an inject section.
var ProjectFiles = []string{"keel.yaml", "keel.yml"}

// Manifest is a declarative set of instances, optionally with the connection
// config it should emit for the consuming repo.
type Manifest struct {
	Version   int        `yaml:"version"`
	Name      string     `yaml:"name"`
	Instances []Instance `yaml:"instances"`
	Inject    *Inject    `yaml:"inject,omitempty"`
	Config    *Config    `yaml:"config,omitempty"`
}

// Config declares where the project's application configuration lives during
// development (Vault or AWS Secrets Manager) and how the app receives it.
type Config struct {
	// Store names the instance holding the config: a vault or secrets instance
	// declared under instances.
	Store string `yaml:"store"`
	// Path is the Vault KV path (default: the project name) or the Secrets
	// Manager secret id (default: the store instance's own secret).
	Path string `yaml:"path,omitempty"`
	// Seed is a dotenv file with development values. sync adds the keys the
	// store does not have yet and never overwrites values changed in the store.
	Seed string `yaml:"seed,omitempty"`
	// Resolve makes sync write the resolved values into the connection file too,
	// for apps that cannot read the store themselves. Default: the app reads the
	// store at startup using CONFIG_STORE and friends.
	Resolve bool `yaml:"resolve,omitempty"`
}

// ConfigStoreKind returns the kind of the instance named by config.store, or ""
// when the manifest has no config section.
func (m *Manifest) ConfigStoreKind() domain.Kind {
	if m.Config == nil {
		return ""
	}
	for _, in := range m.Instances {
		if in.Name == m.Config.Store {
			return domain.Kind(in.Kind)
		}
	}
	return ""
}

// ConfigPath resolves the store path: explicit, else the project name for Vault
// (one KV secret per project) and "" for Secrets Manager (the instance's secret).
func (m *Manifest) ConfigPath() string {
	if m.Config == nil {
		return ""
	}
	if m.Config.Path != "" {
		return m.Config.Path
	}
	if m.ConfigStoreKind() == domain.KindVault {
		return m.Name
	}
	return ""
}

// Inject declares where and how the connection config is written. Zero values
// fall back to Defaults (see InjectOrDefault).
type Inject struct {
	Out    string `yaml:"out"`    // output file (default ".env")
	Format string `yaml:"format"` // dotenv|env|json|yaml|powershell (default "dotenv")
	Mode   string `yaml:"mode"`   // host|docker (default "host")
}

// InjectOrDefault returns the inject settings with defaults filled in, so a
// manifest without an `inject:` block still produces a sensible .env.
func (m *Manifest) InjectOrDefault() Inject {
	in := Inject{}
	if m.Inject != nil {
		in = *m.Inject
	}
	if in.Out == "" {
		in.Out = ".env"
	}
	if in.Format == "" {
		in.Format = "dotenv"
	}
	if in.Mode == "" {
		in.Mode = "host"
	}
	return in
}

// FindProject returns the first project manifest found in dir, or "" if none.
func FindProject(dir string) string {
	for _, name := range ProjectFiles {
		p := filepath.Join(dir, name)
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}

// Instance is one requested instance in a manifest.
type Instance struct {
	Name string `yaml:"name"`
	Kind string `yaml:"kind"`
	// omitempty keeps a generated keel.yaml clean and hand-editable.
	Params   map[string]string `yaml:"params,omitempty"`
	Roles    []string          `yaml:"roles,omitempty"`
	Provider string            `yaml:"provider,omitempty"` // optional driver override
}

// Load reads and validates a manifest file.
func Load(path string) (*Manifest, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}
	var m Manifest
	if err := yaml.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}
	if err := m.Validate(); err != nil {
		return nil, err
	}
	return &m, nil
}

// Validate checks names/kinds are present and known.
func (m *Manifest) Validate() error {
	if len(m.Instances) == 0 {
		return fmt.Errorf("manifest has no instances")
	}
	seen := map[string]bool{}
	for i, in := range m.Instances {
		if in.Name == "" {
			return fmt.Errorf("instance #%d: missing name", i+1)
		}
		if seen[in.Name] {
			return fmt.Errorf("duplicate instance name %q", in.Name)
		}
		seen[in.Name] = true
		if _, ok := catalog.Get(domain.Kind(in.Kind)); !ok {
			return fmt.Errorf("instance %q: unknown kind %q", in.Name, in.Kind)
		}
	}
	return m.validateConfig()
}

func (m *Manifest) validateConfig() error {
	if m.Config == nil {
		return nil
	}
	if m.Config.Store == "" {
		return fmt.Errorf("config: missing store (the vault or secrets instance that holds the config)")
	}
	switch m.ConfigStoreKind() {
	case domain.KindVault:
		if m.ConfigPath() == "" {
			return fmt.Errorf("config: set path (or the manifest name) for the Vault KV secret")
		}
		return nil
	case domain.KindSecrets:
		return nil
	case "":
		return fmt.Errorf("config: store %q must be declared under instances", m.Config.Store)
	default:
		return fmt.Errorf("config: store %q is %s; it must be a vault or secrets instance", m.Config.Store, m.ConfigStoreKind())
	}
}

// Roles converts string roles to domain roles.
func (in Instance) DomainRoles() []domain.Role {
	if len(in.Roles) == 0 {
		return nil
	}
	out := make([]domain.Role, len(in.Roles))
	for i, r := range in.Roles {
		out[i] = domain.Role(r)
	}
	return out
}
