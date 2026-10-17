package manifest

import (
	"strings"
	"testing"
)

func TestConfigValidationAndDefaults(t *testing.T) {
	base := func(cfg *Config) *Manifest {
		return &Manifest{
			Version: 1,
			Name:    "orders",
			Instances: []Instance{
				{Name: "orders-vault", Kind: "vault"},
				{Name: "orders-secrets", Kind: "secrets"},
				{Name: "orders-db", Kind: "postgres"},
			},
			Config: cfg,
		}
	}
	cases := []struct {
		name     string
		cfg      *Config
		wantErr  string
		wantPath string
	}{
		{"no config section", nil, "", ""},
		{"vault defaults path to project", &Config{Store: "orders-vault"}, "", "orders"},
		{"vault explicit path", &Config{Store: "orders-vault", Path: "team/orders"}, "", "team/orders"},
		{"secrets defaults to its own secret", &Config{Store: "orders-secrets"}, "", ""},
		{"missing store", &Config{}, "missing store", ""},
		{"undeclared store", &Config{Store: "ghost"}, "must be declared under instances", ""},
		{"wrong kind", &Config{Store: "orders-db"}, "must be a vault or secrets instance", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := base(c.cfg)
			err := m.Validate()
			if c.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), c.wantErr) {
					t.Fatalf("err = %v, want it to contain %q", err, c.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got := m.ConfigPath(); got != c.wantPath {
				t.Errorf("ConfigPath = %q, want %q", got, c.wantPath)
			}
		})
	}
}
