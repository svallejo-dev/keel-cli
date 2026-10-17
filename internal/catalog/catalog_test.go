package catalog

import (
	"strings"
	"testing"

	"github.com/svallejo-dev/keel-cli/internal/domain"
)

func TestValidateParams(t *testing.T) {
	cases := []struct {
		name    string
		kind    domain.Kind
		params  map[string]string
		wantErr string // substring; empty means it must succeed
	}{
		{"nil is fine", domain.KindDynamoDB, nil, ""},
		{"known keys", domain.KindDynamoDB, map[string]string{"pk": "eventId", "sktype": "N"}, ""},
		{"unknown key", domain.KindDynamoDB, map[string]string{"pkk": "x"}, `unknown param "pkk"`},
		{"value outside enum", domain.KindDynamoDB, map[string]string{"pktype": "Z"}, `invalid value "Z"`},
		{"empty value skips enum check", domain.KindDynamoDB, map[string]string{"pktype": ""}, ""},
		{"sqs attributes", domain.KindSQS, map[string]string{"visibility_timeout": "45", "dlq": "false"}, ""},
		{"sqs bad bool", domain.KindSQS, map[string]string{"dlq": "nope"}, `invalid value "nope"`},
		{"engine with no params", domain.KindSNS, map[string]string{"nope": "x"}, `unknown param "nope"`},
		{"vault mount", domain.KindVault, map[string]string{"mount": "kv"}, ""},
		{"identity project and ui", domain.KindGCPIdentity, map[string]string{"project": "demo-x", "ui": "false"}, ""},
		{"identity bad ui", domain.KindGCPIdentity, map[string]string{"ui": "maybe"}, `invalid value "maybe"`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ValidateParams(c.kind, c.params)
			if c.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", c.wantErr)
			}
			if !strings.Contains(err.Error(), c.wantErr) {
				t.Errorf("error = %q, want it to contain %q", err, c.wantErr)
			}
		})
	}
}

// TestParamsForFiltersByDriver ensures docker-only options never surface for the
// floci-backed driver (and vice versa).
func TestParamsForFiltersByDriver(t *testing.T) {
	has := func(ps []domain.Param, key string) bool {
		for _, p := range ps {
			if p.Key == key {
				return true
			}
		}
		return false
	}

	awsParams := ParamsFor(domain.KindPostgres, domain.ProviderAWS)
	if has(awsParams, "image") {
		t.Error("docker-only param \"image\" leaked into the aws driver")
	}
	if !has(awsParams, "instance_class") {
		t.Error("aws-only param \"instance_class\" missing for the aws driver")
	}

	dockerParams := ParamsFor(domain.KindPostgres, domain.ProviderDocker)
	if !has(dockerParams, "image") {
		t.Error("docker param \"image\" missing for the docker driver")
	}
	if has(dockerParams, "instance_class") {
		t.Error("aws-only param \"instance_class\" leaked into the docker driver")
	}

	// dbname has no Providers restriction: it must appear for both.
	if !has(awsParams, "dbname") || !has(dockerParams, "dbname") {
		t.Error("unrestricted param \"dbname\" should apply to every driver")
	}
}

// TestParamsAreWellFormed guards the catalog itself: keys unique per blueprint,
// and any Default must be one of the declared Enum values.
func TestParamsAreWellFormed(t *testing.T) {
	for _, bp := range All() {
		seen := map[string]bool{}
		for _, p := range bp.Params {
			if p.Key == "" || p.Label == "" {
				t.Errorf("%s: param with empty key or label", bp.Kind)
			}
			if seen[p.Key] {
				t.Errorf("%s: duplicate param %q", bp.Kind, p.Key)
			}
			seen[p.Key] = true

			if p.Default != "" && len(p.Enum) > 0 {
				ok := false
				for _, e := range p.Enum {
					if e == p.Default {
						ok = true
					}
				}
				if !ok {
					t.Errorf("%s/%s: default %q is not in enum %v", bp.Kind, p.Key, p.Default, p.Enum)
				}
			}
		}
	}
}
