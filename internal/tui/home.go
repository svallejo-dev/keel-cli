package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/svallejo-dev/keel-cli/internal/floci"
)

// keelLogo is the launcher wordmark.
const keelLogo = ` _  __ _____ _____ _
| |/ /| ____| ____| |
| ' / |  _| |  _| | |
| . \ | |___| |___| |___
|_|\_\|_____|_____|_____|`

// newHome builds the launcher: a branded menu that routes into every major flow.
func newHome() *chooser {
	banner := headingStyle.Render(keelLogo) + "\n\n" +
		"  " + dimStyle.Render("Infraestructura como Servicio local · con forma de AWS, en tu máquina")

	return &chooser{
		title:  "Inicio",
		banner: banner,
		footer: "↑/↓ move · 1–6 quick · enter choose · q quit",
		items: []chooserItem{
			{
				icon:  "◆",
				title: "Infraestructura",
				desc:  "Crear y gestionar componentes: Postgres, Redis, Kafka, S3, SNS/SQS, Secrets…",
				do:    func(a *App) tea.Cmd { return push(newDashboard()) },
			},
			{
				icon:  "◈",
				title: "Nuevo proyecto",
				desc:  "Generar un repositorio Go (REST) desde plantilla, con o sin config de IA",
				do:    func(a *App) tea.Cmd { return push(newProject()) },
			},
			{
				icon:  "▶",
				title: "Arrancar motor (floci)",
				desc:  "Crea red, imagen y contenedor si hace falta, y espera a que responda",
				do:    engineUpCmd,
			},
			{
				icon:  "✦",
				title: "Doctor",
				desc:  "Diagnóstico del entorno: Docker, motor, floci, estado",
				do:    func(a *App) tea.Cmd { return push(newOutput("Doctor", doctorResult(a))) },
			},
			{
				icon:  "❔",
				title: "Acerca de Keel",
				desc:  "Qué es Keel y cómo se usa",
				do:    func(a *App) tea.Cmd { return push(newOutput("Acerca de Keel", aboutResult())) },
			},
			{
				icon:  "⏻",
				title: "Salir",
				desc:  "Cerrar Keel",
				do:    func(a *App) tea.Cmd { return tea.Quit },
			},
		},
	}
}

// doctorResult builds an environment diagnostic without shelling out to the CLI.
func doctorResult(a *App) result {
	cfg := a.mgr.Config()
	st := floci.Probe(a.ctx, cfg)

	docker, reach := "no detectado", "inalcanzable"
	if st.DockerAvailable {
		docker = "disponible"
	}
	if st.Reachable {
		reach = "alcanzable ✓"
	}

	text := fmt.Sprintf(`Entorno de Keel

  motor (resuelto)  : %s
  Docker            : %s
  contenedor floci  : %s
  endpoint floci    : %s
  estado            : %s
  región            : %s
  red docker        : %s
  home (estado)     : %s

Sin Docker, Keel corre en modo mock (instancias simuladas) y la CLI/TUI siguen
siendo totalmente usables. Con Docker + floci arriba, el motor se resuelve a real.

Si el estado es "inalcanzable", usa la opción «Arrancar motor (floci)» del menú
de inicio (o ejecuta: keel engine up). Keel lo levanta desde cero: red, imagen y
contenedor.`,
		a.engine, docker, st.Container, cfg.FlociEndpoint, reach, cfg.Region, cfg.DockerNetwork, cfg.Home)
	return result{text: text}
}

// engineUpCmd brings floci up asynchronously, streaming its progress into the
// output screen (and the error screen on failure) via the shared action plumbing.
func engineUpCmd(a *App) tea.Cmd {
	const label = "Arrancar motor (floci)"
	start := func() tea.Msg { return runStart{} }
	do := func() tea.Msg {
		cfg := a.mgr.Config()
		var log strings.Builder
		err := floci.EnsureUp(a.ctx, cfg, func(msg string) { log.WriteString("• " + msg + "\n") })
		if err != nil {
			return runResult{label: label, err: err}
		}
		if log.Len() == 0 {
			log.WriteString("• floci ya estaba arriba\n")
		}
		log.WriteString("\n✓ motor listo en " + cfg.FlociEndpoint + "\n")
		return runResult{label: label, res: result{text: log.String(), refresh: true}}
	}
	return tea.Batch(start, do)
}

// aboutResult is the About screen content.
func aboutResult() result {
	return result{text: `Keel — Infraestructura como Servicio local

Un "Fury/Vercel para infra" en tu máquina: levanta infraestructura con forma de
AWS (Postgres≡RDS, Redis≡ElastiCache, Kafka≡MSK, S3, SNS/SQS, Secrets Manager)
vía Docker + floci, para que cualquier repo, en cualquier lenguaje, cree los
componentes que necesita y se conecte a ellos.

Todo lo que ves aquí también está en la CLI:
  keel catalog · create · up/down · list · health · inject · new go · …

Desde este panel:
  ◆ Infraestructura — crea y gestiona componentes
  ◈ Nuevo proyecto  — genera un repo Go (con o sin IA/skills)
  ✦ Doctor          — revisa el entorno`}
}
