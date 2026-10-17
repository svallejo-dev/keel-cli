package cli

import (
	"fmt"

	"github.com/svallejo-dev/keel-cli/internal/control"
	"github.com/svallejo-dev/keel-cli/internal/domain"
)

// opsInstance builds the manager, resolves the named instance, verifies its kind
// (when wantKind is set) and that component operations are available.
func opsInstance(name string, wantKind domain.Kind) (*control.Manager, *domain.Instance, error) {
	mgr, err := buildManager()
	if err != nil {
		return nil, nil, err
	}
	inst, err := mgr.Get(name)
	if err != nil {
		return nil, nil, err
	}
	if wantKind != "" && inst.Kind != wantKind {
		return nil, nil, fmt.Errorf("instance %q is %s, not %s", name, inst.Kind, wantKind)
	}
	if inst.Provider == domain.ProviderMock {
		return nil, nil, fmt.Errorf("component operations are not available on the mock engine (start Docker + floci)")
	}
	if mgr.Ops() == nil {
		return nil, nil, fmt.Errorf("component operations are unavailable (could not init AWS clients)")
	}
	return mgr, inst, nil
}
