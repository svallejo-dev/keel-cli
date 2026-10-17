package tui

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/svallejo-dev/keel-cli/internal/control"
	"github.com/svallejo-dev/keel-cli/internal/domain"
	"github.com/svallejo-dev/keel-cli/internal/scaffold"
)

// projectPreset bundles a set of scaffold options behind a friendly label.
type projectPreset struct {
	icon    string
	title   string
	desc    string
	aiTools []string
	skills  []string
}

var projectPresets = []projectPreset{
	{
		icon:  "▢",
		title: "Repo simple",
		desc:  "Servicio Go REST (POD, Gin, graceful shutdown). Sin configuración de IA.",
	},
	{
		icon:    "◧",
		title:   "Repo + config de IA",
		desc:    "…más espacios para Claude, Copilot y Cursor (agents/MCP) + AGENTS.md.",
		aiTools: []string{"claude", "copilot", "cursor"},
	},
	{
		icon:    "◨",
		title:   "Repo + IA + skills de Go (Claude)",
		desc:    "…más Claude con skills de Go embebidas (idiomas, diseño, DDD, git).",
		aiTools: []string{"claude"},
		skills:  []string{"all"},
	},
	{
		icon:    "◼",
		title:   "Todo",
		desc:    "Todas las herramientas de IA + skills de Go.",
		aiTools: []string{"claude", "copilot", "cursor"},
		skills:  []string{"all"},
	},
}

// newProject is the preset picker for scaffolding a new Go project.
func newProject() *chooser {
	items := make([]chooserItem, 0, len(projectPresets)+1)
	for _, p := range projectPresets {
		p := p
		items = append(items, chooserItem{
			icon:  p.icon,
			title: p.title,
			desc:  p.desc,
			do: func(a *App) tea.Cmd {
				return push(newForm(&domain.Instance{}, scaffoldAction(p)))
			},
		})
	}
	// À la carte option.
	items = append(items, chooserItem{
		icon:  "⚙",
		title: "Personalizado",
		desc:  "Elige a la carta qué herramientas de IA y skills incluir.",
		do:    func(a *App) tea.Cmd { return push(newCustomProject()) },
	})

	return &chooser{
		title:  "Nuevo proyecto",
		banner: "  " + headingStyle.Render("Nuevo proyecto Go") + "\n  " + dimStyle.Render("Elige qué incluir; luego te pido el nombre."),
		footer: "↑/↓ move · 1–5 quick · enter elegir · esc atrás",
		items:  items,
	}
}

// newCustomProject lets the user pick AI tools and skills à la carte, then
// proceeds to the name form with the assembled options.
func newCustomProject() *multiselect {
	return &multiselect{
		title:  "Personalizado",
		banner: "  " + headingStyle.Render("Configura tu repo") + "\n  " + dimStyle.Render("Marca lo que quieras incluir; luego te pido el nombre."),
		footer: "↑/↓ move · space alternar · enter continuar · esc atrás",
		items: []toggle{
			{key: "ai:claude", group: "Config de IA", label: "Claude Code", desc: "agents, skills, MCP"},
			{key: "ai:copilot", group: "Config de IA", label: "GitHub Copilot", desc: "instructions, prompts, MCP"},
			{key: "ai:cursor", group: "Config de IA", label: "Cursor", desc: "rules, MCP"},
			{key: "skills", group: "Extras", label: "Skills de Go", desc: "idiomas, diseño, DDD, git — requiere Claude"},
		},
		onDone: func(a *App, selected []string) tea.Cmd {
			ai, skills := customOptions(selected)
			p := projectPreset{title: "Personalizado", aiTools: ai, skills: skills}
			return push(newForm(&domain.Instance{}, scaffoldAction(p)))
		},
	}
}

// customOptions maps the multiselect keys to scaffold options. Choosing skills
// implies Claude (skills are Claude Code artifacts), so Claude is auto-enabled.
func customOptions(selected []string) (aiTools, skills []string) {
	for _, s := range selected {
		switch s {
		case "ai:claude":
			aiTools = append(aiTools, "claude")
		case "ai:copilot":
			aiTools = append(aiTools, "copilot")
		case "ai:cursor":
			aiTools = append(aiTools, "cursor")
		case "skills":
			skills = []string{"all"}
		}
	}
	if len(skills) > 0 && !contains(aiTools, "claude") {
		aiTools = append([]string{"claude"}, aiTools...)
	}
	return aiTools, skills
}

func contains(ss []string, v string) bool {
	for _, s := range ss {
		if s == v {
			return true
		}
	}
	return false
}

// scaffoldAction turns a preset into a form-backed action that generates the
// project. It reuses the same form + output plumbing as every other TUI action.
func scaffoldAction(p projectPreset) action {
	return action{
		id:    "scaffold",
		label: "Nuevo proyecto — " + p.title,
		fields: []field{
			{key: "name", label: "Nombre del proyecto", required: true, placeholder: "p. ej. payments-api"},
			{key: "module", label: "Módulo Go", placeholder: "opcional (default: el nombre)"},
			{key: "config_store", label: "Config store", placeholder: "vault | aws | none (default: vault)"},
		},
		run: func(ctx context.Context, _ *control.Manager, _ *domain.Instance, in map[string]string) (result, error) {
			svc, err := scaffold.NewGoService(scaffold.Options{
				Name:        in["name"],
				Module:      in["module"],
				Tidy:        true,
				AITools:     p.aiTools,
				Skills:      p.skills,
				ConfigStore: in["config_store"],
			})
			if err != nil {
				return result{}, err
			}
			res, err := svc.Generate(ctx)
			if err != nil {
				return result{}, err
			}
			return result{text: scaffoldSummary(svc, res)}, nil
		},
	}
}

// scaffoldSummary renders a human-friendly report of what was generated.
func scaffoldSummary(svc *scaffold.GoService, res *scaffold.Result) string {
	var b strings.Builder
	fmt.Fprintf(&b, "✓ Proyecto creado en %s\n\n", svc.Dir())

	const skillPrefix = ".claude/skills/"
	for _, f := range res.Files {
		if strings.HasPrefix(f, skillPrefix) {
			continue
		}
		fmt.Fprintf(&b, "  %s\n", f)
	}
	if len(svc.Skills) > 0 {
		fmt.Fprintf(&b, "  %s  (%d skills: %s)\n", skillPrefix, len(svc.Skills), strings.Join(svc.Skills, ", "))
	}
	if len(svc.AITools) > 0 {
		fmt.Fprintf(&b, "\nConfig de IA: %s\n", strings.Join(svc.AITools, ", "))
	}

	b.WriteString("\n")
	switch {
	case res.Tidied:
		b.WriteString("Se ejecutó `go mod tidy` (dependencias resueltas).\n")
	case res.TidyErr != nil:
		fmt.Fprintf(&b, "`go mod tidy` no completó: %s\n(córrelo tú: cd %s && go mod tidy)\n", res.TidyErr.Error(), svc.Dir())
	}

	fmt.Fprintf(&b, "\nSiguientes pasos:\n  cd %s\n  make run    # o: go run ./cmd/%s\n  curl localhost:8080/ping\n", svc.Dir(), svc.Name)
	return b.String()
}
