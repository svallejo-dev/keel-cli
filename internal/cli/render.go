package cli

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/svallejo-dev/keel-cli/internal/domain"
)

// endpointsString renders an instance's role→address map compactly.
func endpointsString(inst *domain.Instance) string {
	if len(inst.Endpoints) == 0 {
		return dim("-")
	}
	roles := make([]domain.Role, 0, len(inst.Endpoints))
	for r := range inst.Endpoints {
		roles = append(roles, r)
	}
	sort.Slice(roles, func(i, j int) bool { return roles[i] < roles[j] })

	parts := make([]string, 0, len(roles))
	for _, r := range roles {
		ep := inst.Endpoints[r]
		addr := fmt.Sprintf("%s:%d", ep.Host, ep.Port)
		if r == domain.RoleDefault {
			parts = append(parts, addr)
		} else {
			parts = append(parts, fmt.Sprintf("%s=%s", r, addr))
		}
	}
	return strings.Join(parts, " ")
}

// printInstanceSummary prints a human-friendly block for a single instance.
func printInstanceSummary(w io.Writer, inst *domain.Instance) {
	fmt.Fprintf(w, "%s  %s\n", bold(inst.Name), dim("("+string(inst.Kind)+")"))
	tw := newTabWriter(w)
	fmt.Fprintf(tw, "  status\t%s\n", statusText(inst.Status))
	fmt.Fprintf(tw, "  cloud\t%s\n", inst.CloudService)
	fmt.Fprintf(tw, "  provider\t%s\n", inst.Provider)
	fmt.Fprintf(tw, "  endpoints\t%s\n", endpointsString(inst))
	if inst.Cred.Username != "" {
		fmt.Fprintf(tw, "  user\t%s\n", inst.Cred.Username)
	}
	if inst.Cred.Password != "" {
		fmt.Fprintf(tw, "  password\t%s\n", inst.Cred.Password)
	}
	if inst.Cred.Token != "" {
		fmt.Fprintf(tw, "  token\t%s\n", inst.Cred.Token)
	}
	if ui := inst.Resources["ui_url"]; ui != "" {
		fmt.Fprintf(tw, "  console\t%s\n", ui)
	}
	if db := inst.Cred.Extra["dbname"]; db != "" {
		fmt.Fprintf(tw, "  dbname\t%s\n", db)
	}
	if arn := inst.Resources["arn"]; arn != "" {
		fmt.Fprintf(tw, "  arn\t%s\n", arn)
	}
	tw.Flush()
}
