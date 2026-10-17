// Package vaultx is a minimal HTTP client for the parts of the Vault API keel
// needs: bootstrapping a server (init, unseal, enabling KV v2) and reading and
// writing KV v2 secrets. It talks plain JSON over net/http instead of pulling the
// Vault SDK, which keeps the dependency graph small.
package vaultx

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

// ErrNotFound is returned when a KV path holds no secret.
var ErrNotFound = errors.New("vault: secret not found")

// Client calls one Vault server.
type Client struct {
	Addr  string // e.g. http://127.0.0.1:15000
	Token string // X-Vault-Token; empty for unauthenticated sys endpoints
	HTTP  *http.Client
}

// New returns a client for addr authenticating with token.
func New(addr, token string) *Client {
	return &Client{
		Addr:  strings.TrimRight(addr, "/"),
		Token: token,
		HTTP:  &http.Client{Timeout: 10 * time.Second},
	}
}

// Status is the seal/init state reported by sys/health.
type Status struct {
	Initialized bool   `json:"initialized"`
	Sealed      bool   `json:"sealed"`
	Version     string `json:"version"`
}

// Health reports the server state. sys/health encodes state in the HTTP status
// (200 active, 501 uninitialized, 503 sealed) but always returns the JSON body,
// so any of those is a successful probe.
func (c *Client) Health(ctx context.Context) (Status, error) {
	var st Status
	code, err := c.do(ctx, http.MethodGet, "/v1/sys/health", nil, &st)
	if err != nil {
		return Status{}, err
	}
	switch code {
	case http.StatusOK, http.StatusTooManyRequests, http.StatusNotImplemented, http.StatusServiceUnavailable, 472, 473:
		return st, nil
	default:
		return Status{}, fmt.Errorf("vault health: HTTP %d", code)
	}
}

// WaitReady polls until the API answers sys/health or timeout elapses.
func (c *Client) WaitReady(ctx context.Context, timeout time.Duration) (Status, error) {
	deadline := time.Now().Add(timeout)
	for {
		st, err := c.Health(ctx)
		if err == nil {
			return st, nil
		}
		if time.Now().After(deadline) {
			return Status{}, fmt.Errorf("vault not reachable at %s after %s: %w", c.Addr, timeout, err)
		}
		select {
		case <-ctx.Done():
			return Status{}, ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
}

// Init initializes a fresh server with a single unseal key (local dev only) and
// returns that key plus the root token.
func (c *Client) Init(ctx context.Context) (unsealKey, rootToken string, err error) {
	var out struct {
		KeysB64   []string `json:"keys_base64"`
		RootToken string   `json:"root_token"`
	}
	if err := c.call(ctx, http.MethodPut, "/v1/sys/init", map[string]int{"secret_shares": 1, "secret_threshold": 1}, &out); err != nil {
		return "", "", fmt.Errorf("vault init: %w", err)
	}
	if len(out.KeysB64) == 0 || out.RootToken == "" {
		return "", "", fmt.Errorf("vault init: empty response")
	}
	return out.KeysB64[0], out.RootToken, nil
}

// Unseal submits the unseal key. It is a no-op on an unsealed server.
func (c *Client) Unseal(ctx context.Context, key string) error {
	var st Status
	if err := c.call(ctx, http.MethodPut, "/v1/sys/unseal", map[string]string{"key": key}, &st); err != nil {
		return fmt.Errorf("vault unseal: %w", err)
	}
	if st.Sealed {
		return fmt.Errorf("vault unseal: server is still sealed")
	}
	return nil
}

// EnableKV2 mounts a KV v2 secrets engine at mount. An already-mounted path is
// treated as success so bootstrap can be retried.
func (c *Client) EnableKV2(ctx context.Context, mount string) error {
	body := map[string]any{"type": "kv", "options": map[string]string{"version": "2"}}
	err := c.call(ctx, http.MethodPost, "/v1/sys/mounts/"+mount, body, nil)
	if err != nil && strings.Contains(err.Error(), "path is already in use") {
		return nil
	}
	return err
}

// KVGet reads the latest version of a KV v2 secret. Non-string values are
// rendered as JSON so they survive a round-trip through env vars.
func (c *Client) KVGet(ctx context.Context, mount, path string) (map[string]string, error) {
	var out struct {
		Data struct {
			Data map[string]any `json:"data"`
		} `json:"data"`
	}
	code, err := c.do(ctx, http.MethodGet, "/v1/"+mount+"/data/"+cleanPath(path), nil, &out)
	if err != nil {
		return nil, err
	}
	if code == http.StatusNotFound {
		return nil, ErrNotFound
	}
	if code != http.StatusOK {
		return nil, fmt.Errorf("vault read %s/%s: HTTP %d", mount, path, code)
	}
	res := make(map[string]string, len(out.Data.Data))
	for k, v := range out.Data.Data {
		res[k] = stringify(v)
	}
	return res, nil
}

// KVPut writes data as a new version of the secret at path (full replace).
func (c *Client) KVPut(ctx context.Context, mount, path string, data map[string]string) error {
	if err := c.call(ctx, http.MethodPost, "/v1/"+mount+"/data/"+cleanPath(path), map[string]any{"data": data}, nil); err != nil {
		return fmt.Errorf("vault write %s/%s: %w", mount, path, err)
	}
	return nil
}

// KVList lists the keys under prefix (sub-folders end with "/"), sorted.
func (c *Client) KVList(ctx context.Context, mount, prefix string) ([]string, error) {
	var out struct {
		Data struct {
			Keys []string `json:"keys"`
		} `json:"data"`
	}
	code, err := c.do(ctx, "LIST", "/v1/"+mount+"/metadata/"+cleanPath(prefix), nil, &out)
	if err != nil {
		return nil, err
	}
	if code == http.StatusNotFound {
		return nil, nil // empty folder
	}
	if code != http.StatusOK {
		return nil, fmt.Errorf("vault list %s/%s: HTTP %d", mount, prefix, code)
	}
	sort.Strings(out.Data.Keys)
	return out.Data.Keys, nil
}

// KVDelete permanently removes a secret and all of its versions.
func (c *Client) KVDelete(ctx context.Context, mount, path string) error {
	if err := c.call(ctx, http.MethodDelete, "/v1/"+mount+"/metadata/"+cleanPath(path), nil, nil); err != nil {
		return fmt.Errorf("vault delete %s/%s: %w", mount, path, err)
	}
	return nil
}

// call is do() that treats any non-2xx status as an error carrying Vault's
// error messages.
func (c *Client) call(ctx context.Context, method, path string, in, out any) error {
	code, err := c.do(ctx, method, path, in, out)
	if err != nil {
		return err
	}
	if code < 200 || code > 299 {
		return fmt.Errorf("HTTP %d", code)
	}
	return nil
}

// do performs one request. It decodes the body into out on success and turns a
// Vault {"errors":[...]} body into an error, returning the status code either way.
func (c *Client) do(ctx context.Context, method, path string, in, out any) (int, error) {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return 0, err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.Addr+path, body)
	if err != nil {
		return 0, err
	}
	if c.Token != "" {
		req.Header.Set("X-Vault-Token", c.Token)
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, err
	}

	// 404 and the sys/health status codes are meaningful answers, not failures.
	if resp.StatusCode >= 400 && resp.StatusCode != http.StatusNotFound && !isHealthCode(path, resp.StatusCode) {
		var verr struct {
			Errors []string `json:"errors"`
		}
		if json.Unmarshal(raw, &verr) == nil && len(verr.Errors) > 0 {
			return resp.StatusCode, fmt.Errorf("%s", strings.Join(verr.Errors, "; "))
		}
		return resp.StatusCode, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	if out != nil && len(raw) > 0 && resp.StatusCode != http.StatusNotFound {
		if err := json.Unmarshal(raw, out); err != nil {
			return resp.StatusCode, fmt.Errorf("decode %s: %w", path, err)
		}
	}
	return resp.StatusCode, nil
}

func isHealthCode(path string, code int) bool {
	return path == "/v1/sys/health" && (code == 429 || code == 472 || code == 473 || code == 501 || code == 503)
}

func cleanPath(p string) string { return strings.Trim(p, "/") }

func stringify(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case nil:
		return ""
	default:
		b, err := json.Marshal(t)
		if err != nil {
			return fmt.Sprint(t)
		}
		return string(b)
	}
}
