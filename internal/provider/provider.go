// Package provider defines the driver abstraction that materializes catalog
// blueprints into running instances. Each driver (aws-floci, docker, mock)
// implements Provider; the control layer depends only on this interface, so a
// resource can fall back from floci to direct Docker without touching callers.
package provider

import (
	"context"
	"io"

	"github.com/svallejo-dev/keel-cli/internal/domain"
)

// PortAllocator hands out free host ports and lets them be returned. The registry
// implements this; drivers receive it so port bookkeeping stays centralized and
// collision-free.
type PortAllocator interface {
	Allocate() (int, error)
	Release(port int)
}

// Provider is a single infrastructure driver.
type Provider interface {
	// Name returns the driver identifier stored on every instance it creates.
	Name() domain.ProviderName

	// Provision creates the underlying resource(s) for spec using bp as the
	// template, allocating host ports through alloc, and returns a fully
	// populated Instance (endpoints, credentials, resource IDs) in the running
	// state. It must be safe to call once per instance name.
	Provision(ctx context.Context, spec domain.Spec, bp domain.Blueprint, alloc PortAllocator) (*domain.Instance, error)

	// Start (re)starts a previously stopped instance in place.
	Start(ctx context.Context, inst *domain.Instance) error

	// Stop halts an instance but preserves its data/resources.
	Stop(ctx context.Context, inst *domain.Instance) error

	// Destroy removes an instance. When purge is true, backing volumes/data are
	// deleted too.
	Destroy(ctx context.Context, inst *domain.Instance, purge bool) error

	// Logs streams instance logs to w. When follow is true it tails until ctx is
	// cancelled.
	Logs(ctx context.Context, inst *domain.Instance, follow bool, w io.Writer) error
}

// Ensurer is implemented by drivers that can bring an instance back to its
// declared state in place — e.g. restart a container that died with the host and
// re-run post-start steps (unsealing Vault). It must be idempotent: calling it on
// a healthy instance is a cheap no-op. `keel sync` uses it so a reconcile repairs
// drift instead of trusting the registry blindly.
type Ensurer interface {
	Ensure(ctx context.Context, inst *domain.Instance) error
}
