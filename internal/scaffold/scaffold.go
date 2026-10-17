// Package scaffold generates new project skeletons from embedded templates.
//
// It currently provides a Go REST service scaffold organised around Package
// Oriented Design: cmd/ binaries, internal/ application packages and
// internal/platform/ foundational packages. Templates live under templates/ and
// are rendered with text/template against a per-project data model, so every
// generated project shares one consistent shape.
package scaffold

import (
	"bytes"
	"context"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"text/template"
	"time"
)

//go:embed all:templates
var templatesFS embed.FS

//go:embed all:skills
var skillsFS embed.FS

// goTemplateRoot is the embedded directory holding the Go service templates.
const goTemplateRoot = "templates/go"

// skillsRoot is the embedded directory holding the bundled Claude Code skills.
const skillsRoot = "skills"

// appPathToken is the placeholder directory name replaced by the binary name
// when materialising template paths (e.g. cmd/__APP__ -> cmd/<name>).
const appPathToken = "__APP__"

// Pinned versions used by the generated project.
const (
	defaultGoVersion  = "1.26"
	defaultGinVersion = "v1.10.0"
)

// Config store options for the generated service. The service always reads the
// environment; a store adds a startup loader for the values its use cases need.
const (
	ConfigStoreVault = "vault" // HashiCorp Vault KV v2 (stdlib-only client)
	ConfigStoreAWS   = "aws"   // Vault + AWS Secrets Manager (adds the AWS SDK)
	ConfigStoreNone  = "none"  // environment variables only
)

// KnownConfigStores lists the accepted ConfigStore values, default first.
var KnownConfigStores = []string{ConfigStoreVault, ConfigStoreAWS, ConfigStoreNone}

// AWS SDK versions pinned in go.mod when the AWS Secrets Manager loader is on.
const (
	awsSDKVersion            = "v1.42.1"
	awsSDKConfigVersion      = "v1.32.29"
	awsSDKSecretsMgrVersion  = "v1.43.0"
	configStorePlatformDir   = "internal/platform/configstore/"
	configStoreAWSOnlySource = configStorePlatformDir + "awssm.go.tmpl"
)

// KnownAITools are the AI coding tools whose config layout the scaffold can
// generate (agents/skills/MCP folders). Order is the display order.
var KnownAITools = []string{"claude", "copilot", "cursor"}

// KnownSkills are the Claude Code skills bundled into keel. When the claude
// AI config is selected they are cloned into the project's .claude/skills/.
// Order is the display order.
var KnownSkills = []string{
	"senior-go-idiomatic",
	"go-design-rules",
	"ddd-go-idiomatic",
	"git-commit",
	"create-pr",
	"review-pr",
}

// Options are the user-facing inputs for a Go service scaffold.
type Options struct {
	Name    string   // project + binary name (required)
	Module  string   // go module path (default: Name)
	Dir     string   // output directory (default: ./<Name>)
	Force   bool     // generate even if Dir is non-empty
	Tidy    bool     // run `go mod tidy` after generating
	AITools []string // AI tool configs to scaffold (subset of KnownAITools)
	Skills  []string // Claude skills to clone (subset of KnownSkills; needs claude)
	// ConfigStore selects the config-store loader: vault (default when empty),
	// aws or none. See KnownConfigStores.
	ConfigStore string
}

// GoService is a resolved, ready-to-render Go service scaffold. The exported
// fields are the data model consumed by the templates.
type GoService struct {
	Name       string
	Module     string
	EnvPrefix  string
	GoVersion  string
	GinVersion string
	Framework  string
	AITools    []string // resolved, validated AI tools (may be empty)
	Skills     []string // resolved skills to clone (empty unless claude selected)
	// ConfigStore is the resolved loader choice (vault, aws or none).
	ConfigStore string
	// AWS SDK versions for go.mod (used when ConfigStore is aws).
	AWSSDKVersion           string
	AWSSDKConfigVersion     string
	AWSSDKSecretsMgrVersion string

	dir   string
	force bool
	tidy  bool
}

// Result reports what a Generate call produced.
type Result struct {
	Dir     string   // absolute-or-relative output directory
	Files   []string // generated files, relative to Dir
	Tidied  bool     // whether `go mod tidy` ran successfully
	TidyErr error    // non-nil if tidy was requested but failed
}

var nameRe = regexp.MustCompile(`[^a-z0-9]+`)

// NewGoService validates the options and resolves defaults.
func NewGoService(o Options) (*GoService, error) {
	name := sanitizeName(o.Name)
	if name == "" {
		return nil, fmt.Errorf("project name %q is empty after normalisation (use letters, digits or dashes)", o.Name)
	}

	module := strings.TrimSpace(o.Module)
	if module == "" {
		module = name
	}
	dir := strings.TrimSpace(o.Dir)
	if dir == "" {
		dir = name
	}

	tools, unknown := NormalizeAITools(o.AITools)
	if len(unknown) > 0 {
		return nil, fmt.Errorf("unknown AI tool(s): %s (known: %s)", strings.Join(unknown, ", "), strings.Join(KnownAITools, ", "))
	}

	skills, unknownSk := NormalizeSkills(o.Skills)
	if len(unknownSk) > 0 {
		return nil, fmt.Errorf("unknown skill(s): %s (known: %s)", strings.Join(unknownSk, ", "), strings.Join(KnownSkills, ", "))
	}
	// Skills are Claude Code artifacts; they only apply when claude is selected.
	if !slices.Contains(tools, "claude") {
		skills = nil
	}

	store := strings.ToLower(strings.TrimSpace(o.ConfigStore))
	if store == "" {
		store = ConfigStoreVault
	}
	if !slices.Contains(KnownConfigStores, store) {
		return nil, fmt.Errorf("unknown config store %q (known: %s)", o.ConfigStore, strings.Join(KnownConfigStores, ", "))
	}

	return &GoService{
		Name:       name,
		Module:     module,
		EnvPrefix:  envPrefix(name),
		GoVersion:  defaultGoVersion,
		GinVersion: defaultGinVersion,
		Framework:  "gin",
		AITools:    tools,
		Skills:     skills,

		ConfigStore:             store,
		AWSSDKVersion:           awsSDKVersion,
		AWSSDKConfigVersion:     awsSDKConfigVersion,
		AWSSDKSecretsMgrVersion: awsSDKSecretsMgrVersion,

		dir:   dir,
		force: o.Force,
		tidy:  o.Tidy,
	}, nil
}

// NormalizeAITools lowercases, trims, de-duplicates and validates the requested
// tools. "all" expands to every KnownAITools; "none"/"" are dropped. It returns
// the valid tools (in KnownAITools order) and any unrecognised names.
func NormalizeAITools(in []string) (tools, unknown []string) {
	return normalizeSet(in, KnownAITools)
}

// NormalizeSkills is NormalizeAITools for the bundled skills (against KnownSkills).
func NormalizeSkills(in []string) (skills, unknown []string) {
	return normalizeSet(in, KnownSkills)
}

// normalizeSet lowercases, trims, de-duplicates and validates in against known.
// "all" expands to every known value; "none"/"" are dropped. It returns the
// selection (in known order) and any unrecognised names.
func normalizeSet(in, known []string) (sel, unknown []string) {
	seen := map[string]bool{}
	for _, raw := range in {
		t := strings.ToLower(strings.TrimSpace(raw))
		switch t {
		case "", "none":
			continue
		case "all":
			for _, k := range known {
				seen[k] = true
			}
			continue
		}
		if slices.Contains(known, t) {
			seen[t] = true
		} else {
			unknown = append(unknown, t)
		}
	}
	for _, k := range known { // stable, known order
		if seen[k] {
			sel = append(sel, k)
		}
	}
	return sel, unknown
}

// Dir returns the resolved output directory.
func (s *GoService) Dir() string { return s.dir }

// Generate materialises the scaffold on disk and, if requested, runs
// `go mod tidy`.
func (s *GoService) Generate(ctx context.Context) (*Result, error) {
	if err := s.ensureTargetWritable(); err != nil {
		return nil, err
	}

	res := &Result{Dir: s.dir}

	// The Go service itself.
	if err := s.renderSubtree(goTemplateRoot, res); err != nil {
		return nil, err
	}

	// Tool-agnostic AI guidance (AGENTS.md) plus each selected tool's config
	// layout (agents/skills/MCP folders).
	if len(s.AITools) > 0 {
		if err := s.renderSubtree("templates/ai/shared", res); err != nil {
			return nil, err
		}
		for _, tool := range s.AITools {
			if err := s.renderSubtree("templates/ai/"+tool, res); err != nil {
				return nil, err
			}
		}
	}

	// Bundled Claude Code skills, cloned verbatim into .claude/skills/.
	if len(s.Skills) > 0 {
		if err := s.copySkills(res); err != nil {
			return nil, err
		}
	}

	if s.tidy {
		if err := s.goModTidy(ctx); err != nil {
			res.TidyErr = err
		} else {
			res.Tidied = true
		}
	}

	return res, nil
}

// renderSubtree walks one embedded template root, renders every file and writes
// it under the output directory (stripping root and the .tmpl suffix).
func (s *GoService) renderSubtree(root string, res *Result) error {
	return fs.WalkDir(templatesFS, root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}

		rel := strings.TrimPrefix(p, root+"/")
		if !s.includes(rel) {
			return nil
		}
		outRel := s.resolveOutputPath(rel)

		rendered, err := s.render(p)
		if err != nil {
			return fmt.Errorf("render %s: %w", rel, err)
		}

		outPath := filepath.Join(s.dir, filepath.FromSlash(outRel))
		if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(outPath, rendered, 0o644); err != nil {
			return err
		}
		res.Files = append(res.Files, outRel)
		return nil
	})
}

// copySkills clones each selected bundled skill verbatim into
// .claude/skills/<skill>/. Skills are copied byte-for-byte (NOT rendered through
// text/template) because their markdown legitimately contains Go template and
// brace syntax.
func (s *GoService) copySkills(res *Result) error {
	for _, skill := range s.Skills {
		root := skillsRoot + "/" + skill
		err := fs.WalkDir(skillsFS, root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}

			// p = "skills/<skill>/<rel>"; place it under .claude/skills/<skill>/<rel>.
			rel := strings.TrimPrefix(p, skillsRoot+"/")
			outRel := ".claude/skills/" + rel

			data, err := skillsFS.ReadFile(p)
			if err != nil {
				return err
			}
			outPath := filepath.Join(s.dir, filepath.FromSlash(outRel))
			if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
				return err
			}
			if err := os.WriteFile(outPath, data, 0o644); err != nil {
				return err
			}
			res.Files = append(res.Files, outRel)
			return nil
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// includes reports whether an optional template is part of this project: the
// config-store loader only exists when a store is selected, and its AWS backend
// only with the aws option (so Vault-only services carry no AWS SDK).
func (s *GoService) includes(rel string) bool {
	switch {
	case rel == configStoreAWSOnlySource:
		return s.ConfigStore == ConfigStoreAWS
	case strings.HasPrefix(rel, configStorePlatformDir):
		return s.ConfigStore != ConfigStoreNone
	default:
		return true
	}
}

// ensureTargetWritable refuses to write into a non-empty directory unless the
// caller passed Force, so an existing project is never clobbered by accident.
func (s *GoService) ensureTargetWritable() error {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if len(entries) > 0 && !s.force {
		return fmt.Errorf("target directory %q is not empty (pass --force to generate anyway)", s.dir)
	}
	return nil
}

// resolveOutputPath turns an embedded template path into its on-disk relative
// path: it swaps the app-name placeholder and drops the .tmpl suffix.
func (s *GoService) resolveOutputPath(rel string) string {
	rel = strings.ReplaceAll(rel, appPathToken, s.Name)
	rel = strings.TrimSuffix(rel, ".tmpl")
	// Templates are stored with forward slashes; keep them until the final join.
	return path.Clean(rel)
}

func (s *GoService) render(embedPath string) ([]byte, error) {
	raw, err := templatesFS.ReadFile(embedPath)
	if err != nil {
		return nil, err
	}
	tmpl, err := template.New(path.Base(embedPath)).Option("missingkey=error").Parse(string(raw))
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, s); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (s *GoService) goModTidy(ctx context.Context) error {
	if _, err := exec.LookPath("go"); err != nil {
		return fmt.Errorf("go toolchain not found on PATH")
	}
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "go", "mod", "tidy")
	cmd.Dir = s.dir
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("go mod tidy: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// sanitizeName normalises a project name to a safe, lowercase, dash-separated
// identifier (a valid Go module/binary name).
func sanitizeName(in string) string {
	s := strings.ToLower(strings.TrimSpace(in))
	s = nameRe.ReplaceAllString(s, "-")
	return strings.Trim(s, "-")
}

// envPrefix derives an environment-variable prefix from the project name:
// "my-svc" -> "MY_SVC".
func envPrefix(name string) string {
	s := strings.ToUpper(name)
	s = regexp.MustCompile(`[^A-Z0-9]+`).ReplaceAllString(s, "_")
	return strings.Trim(s, "_")
}
