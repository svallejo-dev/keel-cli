package inject

import (
	"strings"
	"testing"

	"github.com/svallejo-dev/keel-cli/internal/config"
	"github.com/svallejo-dev/keel-cli/internal/domain"
)

func pgInstance() *domain.Instance {
	return &domain.Instance{
		Name: "orders-db", Kind: domain.KindPostgres, Provider: domain.ProviderDocker,
		Endpoints: map[domain.Role]domain.Endpoint{
			domain.RoleWrite: {Host: "localhost", Port: 15000, DockerHost: "orders-db", DockerPort: 5432},
			domain.RoleRead:  {Host: "localhost", Port: 15000, DockerHost: "orders-db", DockerPort: 5432},
		},
		Cred: domain.Credential{Username: "infra", Password: "pw", Extra: map[string]string{"dbname": "entdb_orders"}},
	}
}

func s3Instance() *domain.Instance {
	return &domain.Instance{
		Name: "archive", Kind: domain.KindS3, Provider: domain.ProviderAWS,
		Endpoints: map[domain.Role]domain.Endpoint{domain.RoleDefault: {Host: "localhost", Port: 4566}},
	}
}

func TestBuildPostgresRolesAndSecrets(t *testing.T) {
	cfg := config.Default()
	b := Build([]*domain.Instance{pgInstance(), s3Instance()}, cfg, ModeHost)
	out, err := b.Render(FormatDotenv)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	// One variable set per role (pattern rule).
	for _, want := range []string{
		"ORDERS_DB_DSN_WRITE=postgres://infra:pw@localhost:15000/entdb_orders?sslmode=disable",
		"ORDERS_DB_DSN_READ=postgres://infra:pw@localhost:15000/entdb_orders?sslmode=disable",
		"ORDERS_DB_USER=infra",
		"AWS_ENDPOINT_URL=http://127.0.0.1:4566",
		"ARCHIVE_BUCKET=archive",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("dotenv missing %q\n---\n%s", want, out)
		}
	}
}

func TestBuildDockerModeUsesNetworkHost(t *testing.T) {
	cfg := config.Default()
	b := Build([]*domain.Instance{pgInstance()}, cfg, ModeDocker)
	out, _ := b.Render(FormatDotenv)
	if !strings.Contains(out, "ORDERS_DB_HOST_WRITE=orders-db") {
		t.Errorf("docker mode should use network host, got:\n%s", out)
	}
	if strings.Contains(out, "localhost:15000") {
		t.Errorf("docker mode should not emit host port mapping:\n%s", out)
	}
}

func TestAWSBlockEmittedOnce(t *testing.T) {
	cfg := config.Default()
	b := Build([]*domain.Instance{s3Instance(), s3Instance()}, cfg, ModeHost)
	out, _ := b.Render(FormatDotenv)
	if n := strings.Count(out, "AWS_ENDPOINT_URL="); n != 1 {
		t.Errorf("AWS block should appear once, appeared %d times", n)
	}
}

func TestBuildVaultAndIdentity(t *testing.T) {
	vault := &domain.Instance{
		Name: "orders-vault", Kind: domain.KindVault, Provider: domain.ProviderDocker,
		Endpoints: map[domain.Role]domain.Endpoint{domain.RoleDefault: {Host: "127.0.0.1", Port: 15010, DockerHost: "keel-orders-vault", DockerPort: 8200}},
		Cred:      domain.Credential{Token: "hvs.root"},
		Resources: map[string]string{"mount": "secret"},
	}
	idp := &domain.Instance{
		Name: "auth", Kind: domain.KindGCPIdentity, Provider: domain.ProviderGCP,
		Endpoints: map[domain.Role]domain.Endpoint{
			domain.RoleDefault: {Host: "127.0.0.1", Port: 15020, DockerHost: "keel-auth", DockerPort: 15020},
			domain.RoleUI:      {Host: "127.0.0.1", Port: 15021},
		},
		Cred:      domain.Credential{Extra: map[string]string{"api_key": "fake-api-key"}},
		Resources: map[string]string{"project": "demo-keel"},
	}

	host, _ := Build([]*domain.Instance{vault, idp}, config.Default(), ModeHost).Render(FormatDotenv)
	for _, want := range []string{
		"VAULT_ADDR=http://127.0.0.1:15010",
		"VAULT_TOKEN=hvs.root",
		"ORDERS_VAULT_KV_MOUNT=secret",
		"GOOGLE_CLOUD_PROJECT=demo-keel",
		"FIREBASE_AUTH_EMULATOR_HOST=127.0.0.1:15020",
		"AUTH_API_KEY=fake-api-key",
		"AUTH_PROJECT_ID=demo-keel",
	} {
		if !strings.Contains(host, want) {
			t.Errorf("host mode missing %q\n---\n%s", want, host)
		}
	}

	docker, _ := Build([]*domain.Instance{vault, idp}, config.Default(), ModeDocker).Render(FormatDotenv)
	for _, want := range []string{
		"VAULT_ADDR=http://keel-orders-vault:8200",
		"FIREBASE_AUTH_EMULATOR_HOST=keel-auth:15020",
	} {
		if !strings.Contains(docker, want) {
			t.Errorf("docker mode missing %q\n---\n%s", want, docker)
		}
	}
}

func TestAddKeepsFirstValue(t *testing.T) {
	b := Build([]*domain.Instance{s3Instance()}, config.Default(), ModeHost)
	if b.Add("AWS_REGION", "eu-west-1") {
		t.Error("Add overwrote a connection variable")
	}
	if !b.Add("FEATURE_X", "on") {
		t.Error("Add rejected a new key")
	}
	out, _ := b.Render(FormatDotenv)
	if !strings.Contains(out, "AWS_REGION=us-east-1") || !strings.Contains(out, "FEATURE_X=on") {
		t.Errorf("unexpected bundle:\n%s", out)
	}
}

func TestDotenvQuotesMultiline(t *testing.T) {
	b := &Bundle{}
	b.Add("PEM", "line1\nline2")
	out, _ := b.Render(FormatDotenv)
	if out != "PEM=\"line1\\nline2\"\n" {
		t.Errorf("multi-line value rendered as %q", out)
	}
}
