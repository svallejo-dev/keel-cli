package configstore

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/svallejo-dev/keel-cli/internal/config"
	"github.com/svallejo-dev/keel-cli/internal/domain"
)

// memStore is an in-memory Store for the read-modify-write helpers.
type memStore struct {
	values map[string]string
	writes int
}

func (m *memStore) Backend() string  { return "mem" }
func (m *memStore) Location() string { return "mem://" }
func (m *memStore) Read(context.Context) (map[string]string, error) {
	out := map[string]string{}
	for k, v := range m.values {
		out[k] = v
	}
	return out, nil
}
func (m *memStore) Write(_ context.Context, v map[string]string) error {
	m.values = v
	m.writes++
	return nil
}
func (m *memStore) BootstrapEnv() [][2]string { return nil }

func TestSeedNeverOverwrites(t *testing.T) {
	s := &memStore{values: map[string]string{"A": "changed-by-dev"}}
	added, err := Seed(context.Background(), s, map[string]string{"A": "seed", "B": "2"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(added, []string{"B"}) {
		t.Errorf("added = %v, want [B]", added)
	}
	if s.values["A"] != "changed-by-dev" || s.values["B"] != "2" {
		t.Errorf("values = %v", s.values)
	}
	// Re-seeding is a no-op that does not even write.
	if _, err := Seed(context.Background(), s, map[string]string{"A": "seed", "B": "2"}); err != nil || s.writes != 1 {
		t.Errorf("second seed wrote again (writes=%d, err=%v)", s.writes, err)
	}
}

func TestSetAndUnsetReportChanges(t *testing.T) {
	s := &memStore{values: map[string]string{"A": "1"}}
	changed, _ := Set(context.Background(), s, map[string]string{"A": "1", "B": "2"})
	if !reflect.DeepEqual(changed, []string{"B"}) {
		t.Errorf("changed = %v, want [B]", changed)
	}
	removed, _ := Unset(context.Background(), s, []string{"A", "nope"})
	if !reflect.DeepEqual(removed, []string{"A"}) {
		t.Errorf("removed = %v, want [A]", removed)
	}
	if _, ok := s.values["A"]; ok {
		t.Error("A still present after unset")
	}
}

func TestParseDotenv(t *testing.T) {
	in := `# comment
FEATURE_X=on
export MAX_RETRIES=3
QUOTED="hello world"
SINGLE='a #not comment'
TRAILING=value # comment
EMPTY=
`
	got, err := ParseDotenv(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"FEATURE_X":   "on",
		"MAX_RETRIES": "3",
		"QUOTED":      "hello world",
		"SINGLE":      "a #not comment",
		"TRAILING":    "value",
		"EMPTY":       "",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v\nwant %v", got, want)
	}
	if _, err := ParseDotenv(strings.NewReader("not a pair\n")); err == nil {
		t.Error("expected an error for a line without '='")
	}
}

func TestDecodeJSON(t *testing.T) {
	got, err := DecodeJSON(`{"A":"x","N":3,"O":{"k":true},"Z":null}`)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"A": "x", "N": "3", "O": `{"k":true}`, "Z": ""}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	if _, err := DecodeJSON(`"just a string"`); err == nil {
		t.Error("expected an error for a non-object secret")
	}
}

func TestOpenRules(t *testing.T) {
	cfg := config.Default()
	vault := &domain.Instance{
		Name: "v", Kind: domain.KindVault, Provider: domain.ProviderDocker,
		Endpoints: map[domain.Role]domain.Endpoint{domain.RoleDefault: {Host: "127.0.0.1", Port: 1}},
		Resources: map[string]string{"mount": "kv"},
	}
	if _, err := Open(cfg, vault, ""); err == nil {
		t.Error("vault without a path must fail")
	}
	s, err := Open(cfg, vault, "/orders/")
	if err != nil {
		t.Fatal(err)
	}
	if s.Location() != "vault://kv/orders" {
		t.Errorf("Location = %q", s.Location())
	}
	if env := s.BootstrapEnv(); env[0] != [2]string{"CONFIG_STORE", "vault"} || env[2] != [2]string{"CONFIG_VAULT_PATH", "orders"} {
		t.Errorf("BootstrapEnv = %v", env)
	}

	secrets := &domain.Instance{Name: "app-config", Kind: domain.KindSecrets, Provider: domain.ProviderAWS}
	s, err = Open(cfg, secrets, "")
	if err != nil {
		t.Fatal(err)
	}
	if s.Location() != "secretsmanager://app-config" {
		t.Errorf("secret id should default to the instance name, got %q", s.Location())
	}

	if _, err := Open(cfg, &domain.Instance{Name: "r", Kind: domain.KindRedis}, "x"); err == nil {
		t.Error("redis cannot be a config store")
	}
	mock := *vault
	mock.Provider = domain.ProviderMock
	if _, err := Open(cfg, &mock, "x"); err == nil {
		t.Error("mock instances cannot be opened")
	}
}
