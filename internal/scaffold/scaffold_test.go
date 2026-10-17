package scaffold

import (
	"context"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateGoService(t *testing.T) {
	dir := t.TempDir()

	svc, err := NewGoService(Options{
		Name:   "Payments API",
		Module: "github.com/acme/payments",
		Dir:    dir,
		Force:  true,
		Tidy:   false, // no network in tests
	})
	if err != nil {
		t.Fatalf("NewGoService: %v", err)
	}

	// Name and env prefix normalisation.
	if svc.Name != "payments-api" {
		t.Errorf("Name = %q, want payments-api", svc.Name)
	}
	if svc.EnvPrefix != "PAYMENTS_API" {
		t.Errorf("EnvPrefix = %q, want PAYMENTS_API", svc.EnvPrefix)
	}

	res, err := svc.Generate(context.Background())
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	// Expected files must all be present (binary dir uses the resolved name).
	want := []string{
		"go.mod",
		"cmd/payments-api/main.go",
		"configs/config.yaml",
		"internal/config/config.go",
		"internal/handlers/handlers.go",
		"internal/health/health.go",
		"internal/platform/web/middleware.go",
		"internal/platform/web/logging.go",
		"internal/platform/web/respond.go",
		"Dockerfile",
		"Makefile",
		".gitignore",
		".env.example",
		"README.md",
	}
	got := map[string]bool{}
	for _, f := range res.Files {
		got[filepath.ToSlash(f)] = true
	}
	for _, w := range want {
		if !got[w] {
			t.Errorf("missing generated file %q", w)
		}
	}

	// The __APP__ placeholder must not survive in any path.
	for _, f := range res.Files {
		if strings.Contains(f, appPathToken) {
			t.Errorf("unresolved placeholder in path %q", f)
		}
	}

	// Every generated .go file must parse and carry the rendered module path.
	fset := token.NewFileSet()
	for _, f := range res.Files {
		if !strings.HasSuffix(f, ".go") {
			continue
		}
		full := filepath.Join(dir, f)
		src, err := os.ReadFile(full)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		if _, err := parser.ParseFile(fset, full, src, parser.AllErrors); err != nil {
			t.Errorf("generated %s does not parse: %v", f, err)
		}
	}

	// go.mod must reference the requested module and Gin.
	gomod, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		t.Fatalf("read go.mod: %v", err)
	}
	if !strings.Contains(string(gomod), "module github.com/acme/payments") {
		t.Errorf("go.mod missing module path:\n%s", gomod)
	}
	if !strings.Contains(string(gomod), "github.com/gin-gonic/gin") {
		t.Errorf("go.mod missing gin dependency:\n%s", gomod)
	}
}

func TestGenerateRefusesNonEmptyDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "keep.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	svc, err := NewGoService(Options{Name: "svc", Dir: dir, Force: false})
	if err != nil {
		t.Fatalf("NewGoService: %v", err)
	}
	if _, err := svc.Generate(context.Background()); err == nil {
		t.Fatal("expected Generate to refuse a non-empty directory without --force")
	}
}

func TestGenerateWithAITools(t *testing.T) {
	dir := t.TempDir()
	svc, err := NewGoService(Options{
		Name:    "api",
		Dir:     dir,
		Force:   true,
		AITools: []string{"claude", "cursor"},
	})
	if err != nil {
		t.Fatalf("NewGoService: %v", err)
	}
	if _, err := svc.Generate(context.Background()); err != nil {
		t.Fatalf("Generate: %v", err)
	}

	mustExist := []string{
		"AGENTS.md",                 // shared
		"CLAUDE.md",                 // claude
		".claude/agents/README.md",  // claude
		".claude/settings.json",     // claude
		".mcp.json",                 // claude
		".cursor/rules/project.mdc", // cursor
		".cursor/mcp.json",          // cursor
	}
	for _, f := range mustExist {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(f))); err != nil {
			t.Errorf("expected AI config %q: %v", f, err)
		}
	}
	// A tool that was not selected must not appear.
	if _, err := os.Stat(filepath.Join(dir, ".github", "copilot-instructions.md")); err == nil {
		t.Error("copilot config was generated but not requested")
	}
}

func TestGenerateClonesSkills(t *testing.T) {
	dir := t.TempDir()
	svc, err := NewGoService(Options{
		Name:    "api",
		Dir:     dir,
		Force:   true,
		AITools: []string{"claude"},
		Skills:  []string{"all"},
	})
	if err != nil {
		t.Fatalf("NewGoService: %v", err)
	}
	if len(svc.Skills) != len(KnownSkills) {
		t.Fatalf("Skills = %v, want all (%d)", svc.Skills, len(KnownSkills))
	}
	if _, err := svc.Generate(context.Background()); err != nil {
		t.Fatalf("Generate: %v", err)
	}

	// Every bundled skill's SKILL.md must land under .claude/skills/<skill>/.
	for _, sk := range KnownSkills {
		p := filepath.Join(dir, ".claude", "skills", sk, "SKILL.md")
		info, err := os.Stat(p)
		if err != nil {
			t.Errorf("missing skill %q: %v", sk, err)
			continue
		}
		if info.Size() == 0 {
			t.Errorf("skill %q SKILL.md is empty", sk)
		}
	}

	// Cloned verbatim: compare a reference file byte-for-byte with the embedded source.
	got, err := os.ReadFile(filepath.Join(dir, ".claude", "skills", "senior-go-idiomatic", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	want, err := skillsFS.ReadFile("skills/senior-go-idiomatic/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Error("cloned SKILL.md is not a verbatim copy of the embedded source")
	}
}

func TestSkillsRequireClaude(t *testing.T) {
	dir := t.TempDir()
	// Skills requested but only cursor selected → skills must be dropped.
	svc, err := NewGoService(Options{
		Name:    "api",
		Dir:     dir,
		Force:   true,
		AITools: []string{"cursor"},
		Skills:  []string{"all"},
	})
	if err != nil {
		t.Fatalf("NewGoService: %v", err)
	}
	if len(svc.Skills) != 0 {
		t.Errorf("Skills = %v, want none without claude", svc.Skills)
	}
	if _, err := svc.Generate(context.Background()); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".claude", "skills")); err == nil {
		t.Error(".claude/skills was created without claude selected")
	}
}

func TestGenerateWithoutAITools(t *testing.T) {
	dir := t.TempDir()
	svc, err := NewGoService(Options{Name: "api", Dir: dir, Force: true})
	if err != nil {
		t.Fatalf("NewGoService: %v", err)
	}
	if _, err := svc.Generate(context.Background()); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	// No AI selected → no AGENTS.md and no tool dirs.
	for _, f := range []string{"AGENTS.md", "CLAUDE.md", ".claude", ".cursor", ".github"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err == nil {
			t.Errorf("unexpected AI artifact %q when none requested", f)
		}
	}
}

func TestNormalizeAITools(t *testing.T) {
	cases := []struct {
		in      []string
		want    []string
		unknown []string
	}{
		{[]string{"claude", "cursor"}, []string{"claude", "cursor"}, nil},
		{[]string{"CURSOR", " claude "}, []string{"claude", "cursor"}, nil}, // order = KnownAITools
		{[]string{"all"}, []string{"claude", "copilot", "cursor"}, nil},
		{[]string{"none"}, nil, nil},
		{[]string{"claude", "claude"}, []string{"claude"}, nil}, // dedupe
		{[]string{"vim"}, nil, []string{"vim"}},
	}
	for _, c := range cases {
		tools, unknown := NormalizeAITools(c.in)
		if strings.Join(tools, ",") != strings.Join(c.want, ",") {
			t.Errorf("NormalizeAITools(%v) tools = %v, want %v", c.in, tools, c.want)
		}
		if strings.Join(unknown, ",") != strings.Join(c.unknown, ",") {
			t.Errorf("NormalizeAITools(%v) unknown = %v, want %v", c.in, unknown, c.unknown)
		}
	}
}

func TestSanitizeAndEnvPrefix(t *testing.T) {
	cases := []struct {
		in, name, env string
	}{
		{"orders", "orders", "ORDERS"},
		{"Payments API", "payments-api", "PAYMENTS_API"},
		{"  my_service  ", "my-service", "MY_SERVICE"},
		{"Foo--Bar", "foo-bar", "FOO_BAR"},
	}
	for _, c := range cases {
		if got := sanitizeName(c.in); got != c.name {
			t.Errorf("sanitizeName(%q) = %q, want %q", c.in, got, c.name)
		}
		if got := envPrefix(sanitizeName(c.in)); got != c.env {
			t.Errorf("envPrefix(%q) = %q, want %q", c.in, got, c.env)
		}
	}
}

func TestConfigStoreVariants(t *testing.T) {
	cases := []struct {
		store       string
		wantFiles   []string
		absentFiles []string
		awsInGoMod  bool
	}{
		{"", []string{"internal/platform/configstore/configstore.go", "internal/platform/configstore/vault.go"},
			[]string{"internal/platform/configstore/awssm.go"}, false},
		{ConfigStoreAWS, []string{"internal/platform/configstore/vault.go", "internal/platform/configstore/awssm.go"}, nil, true},
		{ConfigStoreNone, nil, []string{"internal/platform/configstore/configstore.go", "internal/platform/configstore/vault.go"}, false},
	}
	for _, c := range cases {
		t.Run("store="+c.store, func(t *testing.T) {
			dir := t.TempDir()
			svc, err := NewGoService(Options{Name: "svc", Dir: dir, Force: true, ConfigStore: c.store})
			if err != nil {
				t.Fatal(err)
			}
			res, err := svc.Generate(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			got := map[string]bool{}
			for _, f := range res.Files {
				got[filepath.ToSlash(f)] = true
			}
			for _, f := range c.wantFiles {
				if !got[f] {
					t.Errorf("missing %s", f)
				}
			}
			for _, f := range c.absentFiles {
				if got[f] {
					t.Errorf("unexpected %s", f)
				}
			}
			gomod, _ := os.ReadFile(filepath.Join(dir, "go.mod"))
			if has := strings.Contains(string(gomod), "aws-sdk-go-v2/service/secretsmanager"); has != c.awsInGoMod {
				t.Errorf("AWS SDK in go.mod = %v, want %v", has, c.awsInGoMod)
			}
			main, _ := os.ReadFile(filepath.Join(dir, "cmd/svc/main.go"))
			if uses := strings.Contains(string(main), "configstore.Load"); uses != (c.store != ConfigStoreNone) {
				t.Errorf("main.go uses configstore.Load = %v for store %q", uses, c.store)
			}
		})
	}
}

func TestUnknownConfigStore(t *testing.T) {
	if _, err := NewGoService(Options{Name: "svc", ConfigStore: "consul"}); err == nil {
		t.Fatal("expected an error for an unknown config store")
	}
}
