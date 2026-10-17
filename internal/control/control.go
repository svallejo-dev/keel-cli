// Package control is the orchestration layer: it turns high-level commands
// (create/up/down/destroy/health) into provider calls and keeps the registry and
// audit log consistent. The CLI and TUI both drive the platform through a Manager.
package control

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/svallejo-dev/keel-cli/internal/catalog"
	"github.com/svallejo-dev/keel-cli/internal/config"
	"github.com/svallejo-dev/keel-cli/internal/configstore"
	"github.com/svallejo-dev/keel-cli/internal/domain"
	"github.com/svallejo-dev/keel-cli/internal/health"
	"github.com/svallejo-dev/keel-cli/internal/ops"
	"github.com/svallejo-dev/keel-cli/internal/provider"
	"github.com/svallejo-dev/keel-cli/internal/registry"
)

// Manager coordinates providers and the registry.
type Manager struct {
	cfg       config.Config
	store     *registry.Store
	providers map[domain.ProviderName]provider.Provider
	ops       *ops.Registry
	// forceProvider, when set (mock engine), routes every blueprint to one driver.
	forceProvider domain.ProviderName
}

// NewManager builds a manager. providers maps a ProviderName to its driver.
// opsReg exposes component operations (may be nil if unavailable). forceProvider
// is set (e.g. to ProviderMock) to route all blueprints to one driver regardless
// of their declared provider; leave empty to honor blueprints.
func NewManager(cfg config.Config, store *registry.Store, providers map[domain.ProviderName]provider.Provider, opsReg *ops.Registry, forceProvider domain.ProviderName) *Manager {
	return &Manager{cfg: cfg, store: store, providers: providers, ops: opsReg, forceProvider: forceProvider}
}

// Ops returns the component-operations registry (may be nil).
func (m *Manager) Ops() *ops.Registry { return m.ops }

// Update merges params/notes into an instance, persists, audits and optionally
// restarts it to apply changes.
func (m *Manager) Update(ctx context.Context, name string, params map[string]string, notes string, restart bool) (*domain.Instance, error) {
	inst, err := m.store.Get(name)
	if err != nil {
		return nil, err
	}
	if len(params) > 0 && inst.Params == nil {
		inst.Params = map[string]string{}
	}
	for k, v := range params {
		inst.Params[k] = v
	}
	if notes != "" {
		inst.Notes = notes
	}
	inst.UpdatedAt = time.Now()
	if err := m.store.Put(inst); err != nil {
		return nil, err
	}
	m.store.AuditOK("update", name, string(inst.Kind), string(inst.Provider), params)
	if restart {
		if err := m.Restart(ctx, name); err != nil {
			return inst, err
		}
	}
	return inst, nil
}

// ConfigStore opens the application-config store held by the named instance (a
// vault or secrets instance). path is the Vault KV path or the secret id.
func (m *Manager) ConfigStore(name, path string) (configstore.Store, error) {
	inst, err := m.store.Get(name)
	if err != nil {
		return nil, err
	}
	return configstore.Open(m.cfg, inst, path)
}

// Store exposes the underlying registry (for audit reads, listing, etc.).
func (m *Manager) Store() *registry.Store { return m.store }

// Config exposes the resolved platform config.
func (m *Manager) Config() config.Config { return m.cfg }

func (m *Manager) providerName(bp domain.Blueprint, override domain.ProviderName) domain.ProviderName {
	if m.forceProvider != "" {
		return m.forceProvider // mock engine routes everything to mock
	}
	if override != "" {
		return override
	}
	return bp.Provider
}

func (m *Manager) driver(name domain.ProviderName) (provider.Provider, error) {
	p, ok := m.providers[name]
	if !ok {
		return nil, fmt.Errorf("no provider registered for %q", name)
	}
	return p, nil
}

// Create provisions a new named instance from a blueprint kind. providerOverride,
// when non-empty, forces a specific driver (e.g. ProviderDocker) instead of the
// blueprint default; it is ignored under the mock engine.
func (m *Manager) Create(ctx context.Context, kind domain.Kind, name string, params map[string]string, roles []domain.Role, providerOverride domain.ProviderName) (*domain.Instance, error) {
	bp, ok := catalog.Get(kind)
	if !ok {
		return nil, fmt.Errorf("unknown blueprint %q (see `keel catalog`)", kind)
	}
	if bp.Deferred {
		return nil, fmt.Errorf("blueprint %q is not yet enabled", kind)
	}
	if m.store.Exists(name) {
		return nil, fmt.Errorf("%q: %w", name, registry.ErrExists)
	}

	pname := m.providerName(bp, providerOverride)
	drv, err := m.driver(pname)
	if err != nil {
		return nil, err
	}

	if err := catalog.ValidateParams(kind, params); err != nil {
		return nil, err
	}

	spec := domain.Spec{Name: name, Kind: kind, Roles: roles, Params: params}
	inst, err := drv.Provision(ctx, spec, bp, m.store.Ports())
	if err != nil {
		m.store.AuditErr("create", name, err)
		return nil, fmt.Errorf("provision %s/%s: %w", kind, name, err)
	}
	if err := m.store.Add(inst); err != nil {
		// Roll back the just-provisioned resource to avoid orphans.
		_ = drv.Destroy(ctx, inst, true)
		m.store.AuditErr("create", name, err)
		return nil, err
	}

	// Post-provision seeding that needs the ops layer (which providers cannot
	// reach): pre-create Kafka topics declared with the `topics` param.
	m.seedTopics(ctx, inst, params["topics"])

	m.store.AuditOK("create", name, string(kind), string(pname), params)
	return inst, nil
}

// seedTopics creates the topics requested at provision time ("name[:partitions]",
// comma-separated). Best-effort: a failure is recorded on the instance rather than
// undoing a healthy cluster. Skipped for mock instances (no real broker).
func (m *Manager) seedTopics(ctx context.Context, inst *domain.Instance, spec string) {
	spec = strings.TrimSpace(spec)
	if spec == "" || inst.Kind != domain.KindKafka || inst.Provider == domain.ProviderMock || m.ops == nil {
		return
	}
	// A freshly started broker needs a moment before it accepts clients.
	if !m.waitBroker(ctx, inst, 45*time.Second) {
		inst.Resources["topics_failed"] = "broker not ready"
		_ = m.store.Put(inst)
		return
	}

	var created, failed []string
	for _, raw := range strings.Split(spec, ",") {
		name, partStr, _ := strings.Cut(strings.TrimSpace(raw), ":")
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		partitions, err := strconv.Atoi(strings.TrimSpace(partStr))
		if err != nil || partitions < 1 {
			partitions = 1
		}
		if err := m.ops.Kafka.CreateTopic(ctx, inst, name, partitions, 1, nil); err != nil {
			failed = append(failed, name)
			continue
		}
		created = append(created, name)
	}
	if len(created) > 0 {
		inst.Resources["topics"] = strings.Join(created, ",")
	}
	if len(failed) > 0 {
		inst.Resources["topics_failed"] = strings.Join(failed, ",")
	}
	if len(created)+len(failed) > 0 {
		_ = m.store.Put(inst)
	}
}

// waitBroker polls the Kafka broker until it answers or the timeout elapses.
func (m *Manager) waitBroker(ctx context.Context, inst *domain.Instance, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if _, err := m.ops.Kafka.ListTopics(ctx, inst); err == nil {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(2 * time.Second):
		}
	}
}

// Up (re)starts a single instance.
func (m *Manager) Up(ctx context.Context, name string) error {
	return m.lifecycle(ctx, "up", name, func(drv provider.Provider, inst *domain.Instance) error {
		return drv.Start(ctx, inst)
	})
}

// Down stops a single instance (data preserved).
func (m *Manager) Down(ctx context.Context, name string) error {
	return m.lifecycle(ctx, "down", name, func(drv provider.Provider, inst *domain.Instance) error {
		return drv.Stop(ctx, inst)
	})
}

// Restart stops then starts a single instance.
func (m *Manager) Restart(ctx context.Context, name string) error {
	return m.lifecycle(ctx, "restart", name, func(drv provider.Provider, inst *domain.Instance) error {
		if err := drv.Stop(ctx, inst); err != nil {
			return err
		}
		return drv.Start(ctx, inst)
	})
}

func (m *Manager) lifecycle(ctx context.Context, action, name string, fn func(provider.Provider, *domain.Instance) error) error {
	inst, err := m.store.Get(name)
	if err != nil {
		return err
	}
	drv, err := m.driver(inst.Provider)
	if err != nil {
		return err
	}
	if err := fn(drv, inst); err != nil {
		m.store.AuditErr(action, name, err)
		return err
	}
	if err := m.store.Put(inst); err != nil {
		return err
	}
	m.store.AuditOK(action, name, string(inst.Kind), string(inst.Provider), nil)
	return nil
}

// Ensure brings an existing instance back to its declared running state when its
// driver supports it (see provider.Ensurer) — e.g. a container that died with
// the host, or a Vault that restarted sealed. Drivers without that ability are
// left untouched. It reports whether a repair was attempted.
func (m *Manager) Ensure(ctx context.Context, name string) (bool, error) {
	inst, err := m.store.Get(name)
	if err != nil {
		return false, err
	}
	drv, err := m.driver(inst.Provider)
	if err != nil {
		return false, err
	}
	ens, ok := drv.(provider.Ensurer)
	if !ok {
		return false, nil
	}
	if err := ens.Ensure(ctx, inst); err != nil {
		m.store.AuditErr("ensure", name, err)
		return true, err
	}
	return true, m.store.Put(inst)
}

// Destroy removes an instance (and its data when purge is true).
func (m *Manager) Destroy(ctx context.Context, name string, purge bool) error {
	inst, err := m.store.Get(name)
	if err != nil {
		return err
	}
	drv, err := m.driver(inst.Provider)
	if err != nil {
		return err
	}
	if err := drv.Destroy(ctx, inst, purge); err != nil {
		m.store.AuditErr("destroy", name, err)
		return err
	}
	if err := m.store.Remove(name); err != nil {
		return err
	}
	m.store.AuditOK("destroy", name, string(inst.Kind), string(inst.Provider), nil)
	return nil
}

// Logs streams logs for an instance.
func (m *Manager) Logs(ctx context.Context, name string, follow bool, w io.Writer) error {
	inst, err := m.store.Get(name)
	if err != nil {
		return err
	}
	drv, err := m.driver(inst.Provider)
	if err != nil {
		return err
	}
	return drv.Logs(ctx, inst, follow, w)
}

// List returns all instances.
func (m *Manager) List() []*domain.Instance { return m.store.List() }

// Get returns a single instance.
func (m *Manager) Get(name string) (*domain.Instance, error) { return m.store.Get(name) }

// Health probes a single instance.
func (m *Manager) Health(ctx context.Context, name string) (health.Result, error) {
	inst, err := m.store.Get(name)
	if err != nil {
		return health.Result{}, err
	}
	return health.Check(ctx, m.cfg, inst), nil
}

// InstanceHealth pairs an instance with its probe result.
type InstanceHealth struct {
	Instance *domain.Instance
	Result   health.Result
}

// HealthAll probes every instance.
func (m *Manager) HealthAll(ctx context.Context) []InstanceHealth {
	insts := m.store.List()
	out := make([]InstanceHealth, 0, len(insts))
	for _, inst := range insts {
		out = append(out, InstanceHealth{Instance: inst, Result: health.Check(ctx, m.cfg, inst)})
	}
	return out
}

// UpAll starts every instance, returning the first error encountered (after
// attempting all of them).
func (m *Manager) UpAll(ctx context.Context) error {
	return m.forEach(ctx, m.Up)
}

// DownAll stops every instance.
func (m *Manager) DownAll(ctx context.Context) error {
	return m.forEach(ctx, m.Down)
}

func (m *Manager) forEach(ctx context.Context, fn func(context.Context, string) error) error {
	var firstErr error
	for _, inst := range m.store.List() {
		if err := fn(ctx, inst.Name); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
