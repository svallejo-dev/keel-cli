package ops

import (
	"context"
	"errors"
	"fmt"

	"github.com/svallejo-dev/keel-cli/internal/domain"
	"github.com/svallejo-dev/keel-cli/internal/vaultx"
)

// VaultOps operates on the KV v2 secrets of a vault instance.
type VaultOps struct{}

func (o *VaultOps) client(inst *domain.Instance) (*vaultx.Client, string, error) {
	if err := requireReal(inst); err != nil {
		return nil, "", err
	}
	ep, ok := inst.PrimaryEndpoint()
	if !ok {
		return nil, "", fmt.Errorf("vault %q has no endpoint", inst.Name)
	}
	mount := inst.Resources["mount"]
	if mount == "" {
		mount = "secret"
	}
	return vaultx.New(fmt.Sprintf("http://%s:%d", ep.Host, ep.Port), inst.Cred.Token), mount, nil
}

// Status reports whether the server is initialized and unsealed.
func (o *VaultOps) Status(ctx context.Context, inst *domain.Instance) (vaultx.Status, error) {
	c, _, err := o.client(inst)
	if err != nil {
		return vaultx.Status{}, err
	}
	return c.Health(ctx)
}

// List returns the secret paths under prefix ("" = mount root).
func (o *VaultOps) List(ctx context.Context, inst *domain.Instance, prefix string) ([]string, error) {
	c, mount, err := o.client(inst)
	if err != nil {
		return nil, err
	}
	return c.KVList(ctx, mount, prefix)
}

// Get reads the secret at path.
func (o *VaultOps) Get(ctx context.Context, inst *domain.Instance, path string) (map[string]string, error) {
	c, mount, err := o.client(inst)
	if err != nil {
		return nil, err
	}
	return c.KVGet(ctx, mount, path)
}

// Put writes data at path. With merge, existing keys not in data are kept
// (like `vault kv patch`); otherwise the secret is replaced (`vault kv put`).
func (o *VaultOps) Put(ctx context.Context, inst *domain.Instance, path string, data map[string]string, merge bool) error {
	c, mount, err := o.client(inst)
	if err != nil {
		return err
	}
	if merge {
		cur, err := c.KVGet(ctx, mount, path)
		if err != nil && !errors.Is(err, vaultx.ErrNotFound) {
			return err
		}
		merged := make(map[string]string, len(cur)+len(data))
		for k, v := range cur {
			merged[k] = v
		}
		for k, v := range data {
			merged[k] = v
		}
		data = merged
	}
	return c.KVPut(ctx, mount, path, data)
}

// Delete permanently removes the secret at path (all versions).
func (o *VaultOps) Delete(ctx context.Context, inst *domain.Instance, path string) error {
	c, mount, err := o.client(inst)
	if err != nil {
		return err
	}
	return c.KVDelete(ctx, mount, path)
}
