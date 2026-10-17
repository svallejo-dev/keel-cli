package registry

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/svallejo-dev/keel-cli/internal/domain"
)

func TestPortAllocatorUniqueAndReleasable(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := s.Ports()
	p1, err := a.Allocate()
	if err != nil {
		t.Fatal(err)
	}
	p2, err := a.Allocate()
	if err != nil {
		t.Fatal(err)
	}
	if p1 == p2 {
		t.Fatalf("allocator returned duplicate port %d", p1)
	}
	a.Release(p1)
	// After release p1 should be reusable; the next allocation may reuse it.
	p3, err := a.Allocate()
	if err != nil {
		t.Fatal(err)
	}
	if p3 != p1 && p3 == p2 {
		t.Fatalf("unexpected reuse of live port %d", p2)
	}
}

func TestAddGetRemovePersists(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	inst := &domain.Instance{
		Name: "pg1", Kind: domain.KindPostgres, Provider: domain.ProviderMock,
		Status: domain.StatusRunning, CreatedAt: time.Now(),
		Endpoints: map[domain.Role]domain.Endpoint{domain.RoleDefault: {Host: "localhost", Port: 15010}},
	}
	if err := s.Add(inst); err != nil {
		t.Fatal(err)
	}
	if err := s.Add(inst); err == nil {
		t.Fatal("expected duplicate add to fail")
	}

	// Reopen to confirm persistence.
	s2, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s2.Get("pg1")
	if err != nil {
		t.Fatalf("get after reopen: %v", err)
	}
	if got.Kind != domain.KindPostgres {
		t.Fatalf("unexpected kind %q", got.Kind)
	}
	if err := s2.Remove("pg1"); err != nil {
		t.Fatal(err)
	}
	if _, err := s2.Get("pg1"); err == nil {
		t.Fatal("expected not found after remove")
	}
}

func TestAuditRoundTrip(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s.AuditOK("create", "pg1", "postgres", "mock", nil)
	s.AuditErr("create", "pg2", errString("boom"))
	recs, err := s.ReadAudit(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 2 {
		t.Fatalf("expected 2 audit records, got %d", len(recs))
	}
	if recs[0].Result != "ok" || recs[1].Result != "error" {
		t.Fatalf("unexpected results: %+v", recs)
	}
}

type errString string

func (e errString) Error() string { return string(e) }

func TestLoadMigratesLegacyAWSService(t *testing.T) {
	dir := t.TempDir()
	legacy := `{"version":1,"instances":{"db":{"name":"db","kind":"postgres","aws_service":"Amazon RDS"}}}`
	if err := os.WriteFile(filepath.Join(dir, stateFile), []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	inst, err := s.Get("db")
	if err != nil {
		t.Fatal(err)
	}
	if inst.CloudService != "Amazon RDS" || inst.LegacyAWSService != "" {
		t.Errorf("CloudService=%q Legacy=%q, want migrated", inst.CloudService, inst.LegacyAWSService)
	}
}
