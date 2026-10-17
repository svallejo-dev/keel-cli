package vaultx

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// fakeVault implements just enough of the Vault API for the client: health,
// init, unseal and a single KV v2 mount named "secret".
type fakeVault struct {
	mu          sync.Mutex
	initialized bool
	sealed      bool
	data        map[string]map[string]any
}

func newFakeVault() *httptest.Server {
	f := &fakeVault{sealed: true, data: map[string]map[string]any{}}
	return httptest.NewServer(http.HandlerFunc(f.serve))
}

func (f *fakeVault) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	authed := r.Header.Get("X-Vault-Token") == "root"
	path := r.URL.Path
	switch {
	case path == "/v1/sys/health":
		code := http.StatusOK
		if !f.initialized {
			code = http.StatusNotImplemented
		} else if f.sealed {
			code = http.StatusServiceUnavailable
		}
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(map[string]any{"initialized": f.initialized, "sealed": f.sealed, "version": "2.1.0"})
	case path == "/v1/sys/init":
		f.initialized = true
		_ = json.NewEncoder(w).Encode(map[string]any{"keys_base64": []string{"k1"}, "root_token": "root"})
	case path == "/v1/sys/unseal":
		var in struct{ Key string }
		_ = json.NewDecoder(r.Body).Decode(&in)
		if in.Key == "k1" {
			f.sealed = false
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"sealed": f.sealed})
	case !authed:
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"errors":["permission denied"]}`))
	case path == "/v1/sys/mounts/secret":
		w.WriteHeader(http.StatusNoContent)
	case strings.HasPrefix(path, "/v1/secret/data/"):
		key := strings.TrimPrefix(path, "/v1/secret/data/")
		switch r.Method {
		case http.MethodGet:
			d, ok := f.data[key]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"errors":[]}`))
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"data": d}})
		default:
			var in struct {
				Data map[string]any `json:"data"`
			}
			_ = json.NewDecoder(r.Body).Decode(&in)
			f.data[key] = in.Data
			_, _ = w.Write([]byte(`{"data":{"version":1}}`))
		}
	case strings.HasPrefix(path, "/v1/secret/metadata/"):
		key := strings.TrimPrefix(path, "/v1/secret/metadata/")
		if r.Method == http.MethodDelete {
			delete(f.data, key)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		var keys []string
		for k := range f.data {
			keys = append(keys, k)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"keys": keys}})
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func TestBootstrapAndKVRoundTrip(t *testing.T) {
	srv := newFakeVault()
	defer srv.Close()
	ctx := context.Background()
	c := New(srv.URL, "")

	st, err := c.Health(ctx)
	if err != nil || st.Initialized {
		t.Fatalf("fresh health = %+v, %v; want uninitialized without error", st, err)
	}
	key, token, err := c.Init(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if st, _ := c.Health(ctx); !st.Sealed {
		t.Fatal("expected sealed after init")
	}
	if err := c.Unseal(ctx, key); err != nil {
		t.Fatal(err)
	}

	c.Token = token
	if err := c.EnableKV2(ctx, "secret"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.KVGet(ctx, "secret", "orders"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing secret: err = %v, want ErrNotFound", err)
	}
	if err := c.KVPut(ctx, "secret", "/orders/", map[string]string{"A": "1"}); err != nil {
		t.Fatal(err)
	}
	got, err := c.KVGet(ctx, "secret", "orders")
	if err != nil || got["A"] != "1" {
		t.Fatalf("KVGet = %v, %v", got, err)
	}
	keys, err := c.KVList(ctx, "secret", "")
	if err != nil || len(keys) != 1 || keys[0] != "orders" {
		t.Fatalf("KVList = %v, %v", keys, err)
	}
	if err := c.KVDelete(ctx, "secret", "orders"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.KVGet(ctx, "secret", "orders"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("after delete: err = %v, want ErrNotFound", err)
	}
}

func TestVaultErrorsAreSurfaced(t *testing.T) {
	srv := newFakeVault()
	defer srv.Close()
	err := New(srv.URL, "wrong").KVPut(context.Background(), "secret", "x", map[string]string{"a": "b"})
	if err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("err = %v, want Vault's message", err)
	}
}

func TestStringifyKeepsJSON(t *testing.T) {
	if got := stringify(map[string]any{"a": 1.0}); got != `{"a":1}` {
		t.Errorf("stringify object = %q", got)
	}
	if got := stringify(true); got != "true" {
		t.Errorf("stringify bool = %q", got)
	}
}
