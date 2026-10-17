package tui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/svallejo-dev/keel-cli/internal/catalog"
	"github.com/svallejo-dev/keel-cli/internal/config"
	"github.com/svallejo-dev/keel-cli/internal/control"
	"github.com/svallejo-dev/keel-cli/internal/domain"
	"github.com/svallejo-dev/keel-cli/internal/ops"
	"github.com/svallejo-dev/keel-cli/internal/provider"
	"github.com/svallejo-dev/keel-cli/internal/provider/mock"
	"github.com/svallejo-dev/keel-cli/internal/registry"
)

func testManager(t *testing.T) *control.Manager {
	t.Helper()
	cfg := config.Default()
	cfg.Home = t.TempDir()
	cfg.Engine = config.EngineMock
	store, err := registry.Open(cfg.Home)
	if err != nil {
		t.Fatal(err)
	}
	opsReg, _ := ops.New(cfg)
	providers := map[domain.ProviderName]provider.Provider{domain.ProviderMock: mock.New()}
	mgr := control.NewManager(cfg, store, providers, opsReg, domain.ProviderMock)
	ctx := context.Background()
	for _, kv := range []struct {
		k domain.Kind
		n string
	}{
		{domain.KindRedis, "cache"}, {domain.KindPostgres, "db"}, {domain.KindKafka, "bus"},
		{domain.KindS3, "bucket"}, {domain.KindSQS, "queue"}, {domain.KindSecrets, "creds"},
	} {
		if _, err := mgr.Create(ctx, kv.k, kv.n, nil, nil, ""); err != nil {
			t.Fatalf("create %s: %v", kv.n, err)
		}
	}
	return mgr
}

// TestScreensRenderWithoutPanic drives every screen through resize + a key + view.
func TestScreensRenderWithoutPanic(t *testing.T) {
	mgr := testManager(t)
	app := newApp(context.Background(), mgr, newHome())
	app.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	app.insts = mgr.HealthAll(context.Background())

	inst := mgr.List()[0]
	screens := []screen{
		newHome(),
		newProject(),
		newCustomProject(),
		newDashboard(),
		newDetail(inst),
		newMenu(inst),
		newCreate(),
		newInject(inst),
		newForm(inst, byID(lifecycleActions(), "edit")),
		newConfirm(inst, byID(lifecycleActions(), "delete"), nil, 1),
		newLogs(inst),
		newOutput("text", result{text: "hello"}),
		newOutput("table", result{table: &tableData{cols: []string{"A", "B"}, rows: [][]string{{"1", "2"}}}}),
	}
	for _, s := range screens {
		s.Update(app, tea.WindowSizeMsg{Width: 100, Height: 30})
		s.Update(app, tea.KeyMsg{Type: tea.KeyDown})
		if s.View(app) == "" && s.Title() == "" {
			t.Errorf("screen %T rendered empty", s)
		}
		_ = s.Footer(app)
	}
}

// drive runs an update and drains the resulting commands (feeding their messages
// back), the way the tea runtime would, with a depth guard against blink loops.
func drive(app *App, msg tea.Msg) {
	_, cmd := app.Update(msg)
	feed(app, cmd, 0)
}

func feed(app *App, cmd tea.Cmd, depth int) {
	if cmd == nil || depth > 8 {
		return
	}
	switch m := cmd().(type) {
	case nil:
		return
	case tea.BatchMsg:
		for _, c := range m {
			feed(app, c, depth+1)
		}
	default:
		_, next := app.Update(m)
		feed(app, next, depth+1)
	}
}

// TestHomeLauncher covers the interactive front door: the home renders its menu
// and routes into the new-project wizard down to the name form.
func TestHomeLauncher(t *testing.T) {
	mgr := testManager(t)
	app := newApp(context.Background(), mgr, newHome())
	app.Update(tea.WindowSizeMsg{Width: 92, Height: 30})

	if v := app.View(); !strings.Contains(v, "Infraestructura") || !strings.Contains(v, "Nuevo proyecto") {
		t.Fatalf("home missing menu items:\n%s", v)
	}

	// Home → "Nuevo proyecto" → preset 3 → name form.
	drive(app, tea.KeyMsg{Type: tea.KeyDown})
	drive(app, tea.KeyMsg{Type: tea.KeyEnter})
	if got := app.top().Title(); got != "Nuevo proyecto" {
		t.Fatalf("expected preset chooser 'Nuevo proyecto', got %q (%T)", got, app.top())
	}
	drive(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("3")})
	if _, ok := app.top().(*form); !ok {
		t.Fatalf("expected name form, got %T", app.top())
	}
	if v := app.View(); !strings.Contains(v, "Nombre del proyecto") {
		t.Fatalf("form missing name field:\n%s", v)
	}

	// esc returns to the preset chooser.
	drive(app, tea.KeyMsg{Type: tea.KeyEsc})
	if got := app.top().Title(); got != "Nuevo proyecto" {
		t.Fatalf("esc should return to preset chooser, got %q", got)
	}
}

// TestCustomOptions covers the à-la-carte mapping, incl. "skills implies Claude".
func TestCustomOptions(t *testing.T) {
	cases := []struct {
		name     string
		in       []string
		wantAI   string
		wantSkil string
	}{
		{"none", nil, "", ""},
		{"copilot only", []string{"ai:copilot"}, "copilot", ""},
		{"skills implies claude", []string{"skills"}, "claude", "all"},
		{"claude+cursor+skills", []string{"ai:claude", "ai:cursor", "skills"}, "claude,cursor", "all"},
		{"skills with copilot adds claude first", []string{"ai:copilot", "skills"}, "claude,copilot", "all"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ai, sk := customOptions(c.in)
			if strings.Join(ai, ",") != c.wantAI {
				t.Errorf("ai = %v, want %q", ai, c.wantAI)
			}
			if strings.Join(sk, ",") != c.wantSkil {
				t.Errorf("skills = %v, want %q", sk, c.wantSkil)
			}
		})
	}
}

// TestCreateFormCoversDeclaredParams is the structural-parity guard: the create
// wizard must expose every param the catalog declares for each blueprint. Adding
// an engine or a param can no longer leave the TUI behind (the bug that started
// this: DynamoDB's pk/sk were unreachable from the TUI).
func TestCreateFormCoversDeclaredParams(t *testing.T) {
	for _, bp := range catalog.Active() {
		act := createAction(bp)
		got := map[string]bool{}
		for _, f := range act.fields {
			got[f.key] = true
		}
		if !got["name"] {
			t.Errorf("%s: create form is missing the instance name field", bp.Kind)
		}
		for _, p := range catalog.ParamsFor(bp.Kind, bp.Provider) {
			if !got[p.Key] {
				t.Errorf("%s: declared param %q is not offered by the TUI create form", bp.Kind, p.Key)
			}
		}
	}
}

// TestCreateFormFitsScreen ensures the busiest form still renders within the
// available body height (it scrolls around the focused field).
func TestCreateFormFitsScreen(t *testing.T) {
	mgr := testManager(t)
	app := newApp(context.Background(), mgr, newHome())
	app.Update(tea.WindowSizeMsg{Width: 90, Height: 24})

	bp, ok := catalog.Get(domain.KindDynamoDB)
	if !ok {
		t.Fatal("dynamodb blueprint missing")
	}
	f := newForm(&domain.Instance{}, createAction(bp))
	f.Update(app, tea.WindowSizeMsg{Width: 90, Height: 24})

	view := f.View(app)
	if lines := strings.Count(view, "\n"); lines > app.bodyHeight() {
		t.Errorf("form rendered %d lines, exceeds body height %d", lines, app.bodyHeight())
	}
	// Focus starts at the first field, so it must be visible.
	if !strings.Contains(view, "Instance name") {
		t.Errorf("focused field not visible:\n%s", view)
	}
}

// TestActionsCoverAllOps ensures every kind with capabilities has actions.
func TestActionsCoverAllOps(t *testing.T) {
	for _, k := range []domain.Kind{
		domain.KindKafka, domain.KindPostgres, domain.KindRedis,
		domain.KindS3, domain.KindSNS, domain.KindSQS, domain.KindSecrets,
	} {
		if ops.HasOps(k) && len(opsActions(k)) == 0 {
			t.Errorf("kind %s has capabilities but no TUI actions", k)
		}
	}
}
