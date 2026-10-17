package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/svallejo-dev/keel-cli/internal/catalog"
	"github.com/svallejo-dev/keel-cli/internal/domain"
)

func newCatalogCmd() *cobra.Command {
	var all bool
	cmd := &cobra.Command{
		Use:     "catalog [kind]",
		Aliases: []string{"blueprints"},
		Short:   "List blueprints, or show one engine's options (keel catalog dynamodb)",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 {
				return showBlueprint(cmd, domain.Kind(args[0]))
			}
			list := catalog.Active()
			if all {
				list = catalog.All()
			}
			tw := newTabWriter(cmd.OutOrStdout())
			fmt.Fprintln(tw, heading("KIND\tCLOUD\tSERVICE\tCATEGORY\tPROVIDER\tPORT\tROLES"))
			for _, bp := range list {
				roles := "default"
				if bp.HasRoles() {
					roles = joinRoles(bp.Roles)
				}
				name := string(bp.Kind)
				if bp.Deferred {
					name += dim(" (deferred)")
				}
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%d\t%s\n",
					name, bp.Cloud, bp.CloudService, bp.Category, bp.Provider, bp.DefaultPort, roles)
			}
			tw.Flush()
			fmt.Fprintln(cmd.OutOrStdout())
			fmt.Fprintln(cmd.OutOrStdout(), dim("Create one with:  keel create <kind> --name <instance>"))
			if !all {
				fmt.Fprintln(cmd.OutOrStdout(), dim("Show legacy/deferred engines with:  keel catalog --all"))
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "include deferred/legacy engines")
	return cmd
}

// showBlueprint prints one engine's detail sheet: parity target, defaults and the
// --param options it accepts (the same list the TUI form is generated from).
func showBlueprint(cmd *cobra.Command, kind domain.Kind) error {
	bp, ok := catalog.Get(kind)
	if !ok {
		return exitErr(fmt.Errorf("unknown blueprint %q (run `keel catalog`)", kind))
	}
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "%s  %s\n", bold(string(bp.Kind)), dim(bp.DisplayName))
	fmt.Fprintf(out, "%s\n\n", bp.Description)

	tw := newTabWriter(out)
	fmt.Fprintf(tw, "%s\t%s\n", dim("cloud"), bp.Cloud)
	fmt.Fprintf(tw, "%s\t%s\n", dim("parity"), bp.CloudService)
	fmt.Fprintf(tw, "%s\t%s\n", dim("category"), bp.Category)
	fmt.Fprintf(tw, "%s\t%s\n", dim("provider"), bp.Provider)
	fmt.Fprintf(tw, "%s\t%d\n", dim("port"), bp.DefaultPort)
	roles := "default"
	if bp.HasRoles() {
		roles = joinRoles(bp.Roles)
	}
	fmt.Fprintf(tw, "%s\t%s\n", dim("roles"), roles)
	tw.Flush()

	if len(bp.Params) == 0 {
		fmt.Fprintln(out, "\n"+dim("this engine takes no --param options"))
		return nil
	}

	fmt.Fprintf(out, "\n%s\n", heading("OPTIONS  (--param key=value)"))
	ptw := newTabWriter(out)
	fmt.Fprintln(ptw, heading("KEY\tDEFAULT\tVALUES\tDRIVER\tDESCRIPTION"))
	for _, p := range bp.Params {
		def, values, driver := p.Default, "", "any"
		if def == "" {
			def = "-"
		}
		if p.Required {
			def = styleBad.Render("required")
		}
		if len(p.Enum) > 0 {
			values = strings.Join(p.Enum, "|")
		} else {
			values = "-"
		}
		if len(p.Providers) > 0 {
			names := make([]string, len(p.Providers))
			for i, pr := range p.Providers {
				names[i] = string(pr)
			}
			driver = strings.Join(names, ",")
		}
		help := p.Help
		if help == "" {
			help = p.Label
		}
		fmt.Fprintf(ptw, "%s\t%s\t%s\t%s\t%s\n", p.Key, def, values, dim(driver), help)
	}
	ptw.Flush()
	fmt.Fprintf(out, "\n%s\n", dim("keel create "+string(bp.Kind)+" --name my-"+string(bp.Kind)+" --param key=value"))
	return nil
}

func joinRoles(roles []domain.Role) string {
	parts := make([]string, len(roles))
	for i, r := range roles {
		parts[i] = r.String()
	}
	return strings.Join(parts, ",")
}

// exitErr returns the error so cobra prints it once (SilenceErrors=false) and the
// process exits non-zero. Kept as a thin wrapper so call sites read intentionally.
func exitErr(err error) error {
	return err
}
