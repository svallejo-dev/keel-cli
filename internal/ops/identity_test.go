package ops

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/svallejo-dev/keel-cli/internal/domain"
)

// identityInstance points an instance at a test server.
func identityInstance(t *testing.T, srv *httptest.Server) *domain.Instance {
	t.Helper()
	host, portStr, _ := strings.Cut(strings.TrimPrefix(srv.URL, "http://"), ":")
	port, _ := strconv.Atoi(portStr)
	return &domain.Instance{
		Name: "auth", Kind: domain.KindGCPIdentity, Provider: domain.ProviderGCP,
		Endpoints: map[domain.Role]domain.Endpoint{domain.RoleDefault: {Host: host, Port: port}},
		Resources: map[string]string{"project": "demo-keel"},
	}
}

func TestIdentityDeleteByEmailResolvesUID(t *testing.T) {
	var deleted string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer owner" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/identitytoolkit.googleapis.com/v1/projects/demo-keel/accounts:lookup":
			_, _ = w.Write([]byte(`{"users":[{"localId":"uid-1","email":"ana@test.com"}]}`))
		case "/identitytoolkit.googleapis.com/v1/projects/demo-keel/accounts:delete":
			var in map[string]string
			_ = json.NewDecoder(r.Body).Decode(&in)
			deleted = in["localId"]
			_, _ = w.Write([]byte(`{}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	o := &IdentityOps{}
	if err := o.DeleteUser(context.Background(), identityInstance(t, srv), "ana@test.com"); err != nil {
		t.Fatal(err)
	}
	if deleted != "uid-1" {
		t.Errorf("deleted %q, want uid-1", deleted)
	}
}

func TestIdentitySurfacesGoogleErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"code":400,"message":"INVALID_PASSWORD"}}`))
	}))
	defer srv.Close()

	_, err := (&IdentityOps{}).SignIn(context.Background(), identityInstance(t, srv), "ana@test.com", "bad")
	if err == nil || !strings.Contains(err.Error(), "INVALID_PASSWORD") {
		t.Fatalf("err = %v, want INVALID_PASSWORD", err)
	}
}

func TestIdentityClaimsMustBeJSON(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	err := (&IdentityOps{}).SetClaims(context.Background(), identityInstance(t, srv), "uid-1", "role=admin")
	if err == nil || !strings.Contains(err.Error(), "JSON object") {
		t.Fatalf("err = %v, want a JSON validation error", err)
	}
}
