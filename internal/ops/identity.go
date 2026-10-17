package ops

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/svallejo-dev/keel-cli/internal/domain"
)

// IdentityOps manages users of an Identity Platform instance through the
// emulator's REST API: the same identitytoolkit endpoints the Admin SDKs call,
// authorized with the emulator's "Bearer owner" admin token.
type IdentityOps struct {
	http *http.Client
}

// IdentityUser is one account in the identity provider.
type IdentityUser struct {
	UID           string
	Email         string
	DisplayName   string
	EmailVerified bool
	Disabled      bool
	CustomClaims  string // JSON object, "" when unset
	CreatedAt     time.Time
}

// SignInResult carries the tokens a client receives after signing in.
type SignInResult struct {
	UID          string
	IDToken      string // JWT to send as "Authorization: Bearer <token>"
	RefreshToken string
	ExpiresIn    string // seconds
}

type identityTarget struct {
	base    string // http://host:port/identitytoolkit.googleapis.com/v1
	root    string // http://host:port
	project string
	apiKey  string
}

func (o *IdentityOps) target(inst *domain.Instance) (identityTarget, error) {
	if err := requireReal(inst); err != nil {
		return identityTarget{}, err
	}
	ep, ok := inst.PrimaryEndpoint()
	if !ok {
		return identityTarget{}, fmt.Errorf("identity %q has no endpoint", inst.Name)
	}
	root := fmt.Sprintf("http://%s:%d", ep.Host, ep.Port)
	key := inst.Cred.Extra["api_key"]
	if key == "" {
		key = "fake-api-key"
	}
	return identityTarget{
		base:    root + "/identitytoolkit.googleapis.com/v1",
		root:    root,
		project: inst.Resources["project"],
		apiKey:  key,
	}, nil
}

// ListUsers returns every account (the emulator holds a dev-sized user set).
func (o *IdentityOps) ListUsers(ctx context.Context, inst *domain.Instance) ([]IdentityUser, error) {
	t, err := o.target(inst)
	if err != nil {
		return nil, err
	}
	var out struct {
		Users []rawUser `json:"users"`
	}
	url := fmt.Sprintf("%s/projects/%s/accounts:batchGet?maxResults=1000", t.base, t.project)
	if err := o.call(ctx, http.MethodGet, url, true, nil, &out); err != nil {
		return nil, err
	}
	users := make([]IdentityUser, 0, len(out.Users))
	for _, u := range out.Users {
		users = append(users, u.user())
	}
	sort.Slice(users, func(i, j int) bool { return users[i].Email < users[j].Email })
	return users, nil
}

// CreateUser creates an email/password account and returns its uid.
func (o *IdentityOps) CreateUser(ctx context.Context, inst *domain.Instance, email, password, displayName string) (string, error) {
	t, err := o.target(inst)
	if err != nil {
		return "", err
	}
	body := map[string]any{"email": email, "password": password}
	if displayName != "" {
		body["displayName"] = displayName
	}
	var out struct {
		LocalID string `json:"localId"`
	}
	url := fmt.Sprintf("%s/projects/%s/accounts", t.base, t.project)
	if err := o.call(ctx, http.MethodPost, url, true, body, &out); err != nil {
		return "", err
	}
	return out.LocalID, nil
}

// DeleteUser removes an account by uid or email.
func (o *IdentityOps) DeleteUser(ctx context.Context, inst *domain.Instance, uidOrEmail string) error {
	t, err := o.target(inst)
	if err != nil {
		return err
	}
	uid, err := o.resolveUID(ctx, t, uidOrEmail)
	if err != nil {
		return err
	}
	url := fmt.Sprintf("%s/projects/%s/accounts:delete", t.base, t.project)
	return o.call(ctx, http.MethodPost, url, true, map[string]string{"localId": uid}, nil)
}

// SetClaims replaces a user's custom claims (roles, tenant…) with claimsJSON,
// a JSON object; "{}" clears them. They appear in the user's next ID token.
func (o *IdentityOps) SetClaims(ctx context.Context, inst *domain.Instance, uidOrEmail, claimsJSON string) error {
	t, err := o.target(inst)
	if err != nil {
		return err
	}
	var probe map[string]any
	if err := json.Unmarshal([]byte(claimsJSON), &probe); err != nil {
		return fmt.Errorf("claims must be a JSON object: %w", err)
	}
	uid, err := o.resolveUID(ctx, t, uidOrEmail)
	if err != nil {
		return err
	}
	url := fmt.Sprintf("%s/projects/%s/accounts:update", t.base, t.project)
	return o.call(ctx, http.MethodPost, url, true, map[string]string{"localId": uid, "customAttributes": claimsJSON}, nil)
}

// SignIn performs an email/password sign-in like a client app would and returns
// the ID token — handy to call a protected API by hand.
func (o *IdentityOps) SignIn(ctx context.Context, inst *domain.Instance, email, password string) (SignInResult, error) {
	t, err := o.target(inst)
	if err != nil {
		return SignInResult{}, err
	}
	var out struct {
		LocalID      string `json:"localId"`
		IDToken      string `json:"idToken"`
		RefreshToken string `json:"refreshToken"`
		ExpiresIn    string `json:"expiresIn"`
	}
	url := fmt.Sprintf("%s/accounts:signInWithPassword?key=%s", t.base, t.apiKey)
	body := map[string]any{"email": email, "password": password, "returnSecureToken": true}
	if err := o.call(ctx, http.MethodPost, url, false, body, &out); err != nil {
		return SignInResult{}, err
	}
	return SignInResult{UID: out.LocalID, IDToken: out.IDToken, RefreshToken: out.RefreshToken, ExpiresIn: out.ExpiresIn}, nil
}

// Reset deletes every account in the project (emulator-only endpoint).
func (o *IdentityOps) Reset(ctx context.Context, inst *domain.Instance) error {
	t, err := o.target(inst)
	if err != nil {
		return err
	}
	url := fmt.Sprintf("%s/emulator/v1/projects/%s/accounts", t.root, t.project)
	return o.call(ctx, http.MethodDelete, url, false, nil, nil)
}

func (o *IdentityOps) resolveUID(ctx context.Context, t identityTarget, uidOrEmail string) (string, error) {
	if !strings.Contains(uidOrEmail, "@") {
		return uidOrEmail, nil
	}
	var out struct {
		Users []rawUser `json:"users"`
	}
	url := fmt.Sprintf("%s/projects/%s/accounts:lookup", t.base, t.project)
	if err := o.call(ctx, http.MethodPost, url, true, map[string][]string{"email": {uidOrEmail}}, &out); err != nil {
		return "", err
	}
	if len(out.Users) == 0 {
		return "", fmt.Errorf("no user with email %s", uidOrEmail)
	}
	return out.Users[0].LocalID, nil
}

// call sends one JSON request. admin adds the emulator's owner credential, which
// grants Admin SDK privileges. Google-style {"error":{"message":…}} bodies become
// the returned error.
func (o *IdentityOps) call(ctx context.Context, method, url string, admin bool, in, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return err
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if admin {
		req.Header.Set("Authorization", "Bearer owner")
	}
	client := o.http
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode >= 300 {
		var gerr struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(raw, &gerr) == nil && gerr.Error.Message != "" {
			return fmt.Errorf("identity platform: %s", gerr.Error.Message)
		}
		return fmt.Errorf("identity platform: HTTP %d", resp.StatusCode)
	}
	if out != nil && len(raw) > 0 {
		return json.Unmarshal(raw, out)
	}
	return nil
}

type rawUser struct {
	LocalID          string `json:"localId"`
	Email            string `json:"email"`
	DisplayName      string `json:"displayName"`
	EmailVerified    bool   `json:"emailVerified"`
	Disabled         bool   `json:"disabled"`
	CustomAttributes string `json:"customAttributes"`
	CreatedAt        string `json:"createdAt"` // epoch millis, as a string
}

func (u rawUser) user() IdentityUser {
	out := IdentityUser{
		UID:           u.LocalID,
		Email:         u.Email,
		DisplayName:   u.DisplayName,
		EmailVerified: u.EmailVerified,
		Disabled:      u.Disabled,
		CustomClaims:  u.CustomAttributes,
	}
	if ms, err := strconv.ParseInt(u.CreatedAt, 10, 64); err == nil {
		out.CreatedAt = time.UnixMilli(ms)
	}
	return out
}
