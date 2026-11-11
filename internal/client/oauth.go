package client

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type OAuth struct {
	Config Config
	Store  Store
	HTTP   *http.Client
}

func NewOAuth(c Config) *OAuth {
	return &OAuth{Config: c, Store: &Vault{Config: c}, HTTP: httpClient()}
}

type document = map[string]any

func stringAt(d document, k string) string { s, _ := d[k].(string); return s }
func randomText() string {
	b := make([]byte, 32)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func (o *OAuth) request(ctx context.Context, path string, body any, token string) (document, error) {
	var reader io.Reader
	contentType := "application/json"
	method := "GET"
	if body != nil {
		method = "POST"
		if f, ok := body.(url.Values); ok {
			reader = strings.NewReader(f.Encode())
			contentType = "application/x-www-form-urlencoded"
		} else {
			b, e := json.Marshal(body)
			if e != nil {
				return nil, errConnection
			}
			reader = bytes.NewReader(b)
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	r, e := http.NewRequestWithContext(ctx, method, o.Config.Origin+path, reader)
	if e != nil {
		return nil, errConnection
	}
	r.Header.Set("Accept", "application/json")
	if body != nil {
		r.Header.Set("Content-Type", contentType)
	}
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	response, e := o.HTTP.Do(r)
	if e != nil {
		return nil, errConnection
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, oauthHTTPError(response, path)
	}
	if strings.HasSuffix(path, "/revoke") {
		return document{}, nil
	}
	raw, e := io.ReadAll(io.LimitReader(response.Body, 65537))
	var d document
	if e != nil || len(raw) > 65536 || json.Unmarshal(raw, &d) != nil || d == nil {
		return nil, errors.New("invalid Reah OAuth response")
	}
	return d, nil
}
func (o *OAuth) discover(ctx context.Context, enroll bool) (string, error) {
	resource, e := o.request(ctx, "/.well-known/oauth-protected-resource/mcp", nil, "")
	if e != nil {
		return "", e
	}
	m, e := o.request(ctx, "/.well-known/oauth-authorization-server", nil, "")
	if e != nil {
		return "", e
	}
	contains := func(v any, want string) bool {
		xs, ok := v.([]any)
		if !ok {
			return false
		}
		for _, x := range xs {
			if x == want {
				return true
			}
		}
		return false
	}
	if resource["resource"] != o.Config.Server || !contains(resource["authorization_servers"], o.Config.Origin) || m["issuer"] != o.Config.Origin || !contains(m["code_challenge_methods_supported"], "S256") || !contains(m["scopes_supported"], Scope) {
		return "", errors.New("OAuth discovery does not match this Reah server")
	}
	for field, name := range map[string]string{"authorization_endpoint": "authorize", "token_endpoint": "token", "revocation_endpoint": "revoke", "registration_endpoint": "register"} {
		if m[field] != o.Config.Origin+"/mcp/oauth/"+name {
			return "", errors.New("unexpected OAuth endpoint")
		}
	}
	if !enroll {
		return "", nil
	}
	if m["reah_credential_enrollment_supported"] != true {
		return "", errors.New("this server does not support credential registration; use ordinary login for read-only access")
	}
	page := stringAt(m, "reah_credential_registration_uri")
	u, e := url.Parse(page)
	if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Path != "/mcp/credentials" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return "", errors.New("invalid Reah credential registration page")
	}
	return page, nil
}
func parseTokens(d document) (*Tokens, error) {
	a, r := stringAt(d, "access_token"), stringAt(d, "refresh_token")
	n, ok := d["expires_in"].(float64)
	if a == "" || r == "" || strings.IndexFunc(a, func(r rune) bool { return r < 33 || r > 126 }) >= 0 || d["token_type"] != "Bearer" || d["scope"] != Scope || !ok || n < 1 || n > 86400 || n != float64(int64(n)) {
		return nil, errors.New("invalid Reah token response; reconnect")
	}
	return &Tokens{a, r, time.Now().Add(time.Duration(n) * time.Second).UnixMilli()}, nil
}
func (o *OAuth) revoke(ctx context.Context, s *Session, token string) error {
	_, e := o.request(ctx, "/mcp/oauth/revoke", url.Values{"client_id": {s.ClientID}, "resource": {o.Config.Server}, "token": {token}, "token_type_hint": {"refresh_token"}}, "")
	return e
}

func (o *OAuth) AccessToken(ctx context.Context) (string, error) {
	var token string
	e := o.Store.Exclusive(ctx, func() error {
		s, e := o.Store.Read()
		if e != nil {
			return e
		}
		if s == nil || s.Tokens == nil {
			return &connectionError{Code: "login_required", Message: "Reah has no usable local OAuth credentials; run status with this server and profile for the exact login command"}
		}
		if s.Tokens.ExpiresAt > time.Now().Add(30*time.Second).UnixMilli() {
			token = s.Tokens.AccessToken
			return nil
		}
		old := s.Tokens
		s.Tokens = nil
		s.PendingRevocation = old.RefreshToken
		// Persist consumption before network I/O: never replay a rotated refresh token.
		if e = o.Store.Write(s); e != nil {
			return e
		}
		d, e := o.request(ctx, "/mcp/oauth/token", url.Values{"grant_type": {"refresh_token"}, "client_id": {s.ClientID}, "resource": {o.Config.Server}, "refresh_token": {old.RefreshToken}}, "")
		if e != nil {
			return fmt.Errorf("refresh outcome is unconfirmed; the consumed refresh token will not be retried; run status for a new login command: %w", e)
		}
		s.Tokens, e = parseTokens(d)
		if e != nil {
			return &connectionError{Code: "login_required", Message: "refresh returned unusable credentials; the consumed token will not be retried; run status for a new login command"}
		}
		s.PendingRevocation = ""
		if e = o.Store.Write(s); e != nil {
			return e
		}
		token = s.Tokens.AccessToken
		return nil
	})
	return token, e
}

func (o *OAuth) Login(ctx context.Context, enroll bool, open func(string) error) error {
	return o.LoginWithPolicyProfile(ctx, enroll, "wallet-transfer-v1", open)
}

// LoginWithPolicyProfile always creates new consent; it never upgrades a grant.
func (o *OAuth) LoginWithPolicyProfile(ctx context.Context, enroll bool, profile string, open func(string) error) error {
	return o.LoginWithProgress(ctx, enroll, profile, func(step LoginStep) error {
		if step.URL != "" {
			return open(step.URL)
		}
		return nil
	})
}

// LoginStep distinguishes browser consent from provider credential registration.
// URLs are private continuation links; do not include them in shared logs.
type LoginStep struct {
	Stage   string `json:"stage"`
	Message string `json:"message"`
	URL     string `json:"url,omitempty"`
}

func (o *OAuth) LoginWithProgress(ctx context.Context, enroll bool, profile string, progress func(LoginStep) error) error {
	if profile != "wallet-transfer-v1" && profile != "financial-operations-v1" || !enroll && profile != "wallet-transfer-v1" {
		return errors.New("use a supported policy profile with --enroll-key for financial signing")
	}
	page, e := o.discover(ctx, enroll)
	if e != nil {
		return e
	}
	return o.Store.Exclusive(ctx, func() error {
		s, e := o.Store.Read()
		if e != nil {
			return e
		}
		path := "/callback/reah-mcp/" + o.Config.ID[:24]
		if s == nil {
			d, e := o.request(ctx, "/mcp/oauth/register", document{"client_name": "Reah Go MCP", "redirect_uris": []string{"http://127.0.0.1" + path}, "grant_types": []string{"authorization_code", "refresh_token"}, "response_types": []string{"code"}, "token_endpoint_auth_method": "none", "scope": Scope}, "")
			if e != nil {
				return e
			}
			id := stringAt(d, "client_id")
			if id == "" || len(id) > 4096 {
				return errors.New("invalid OAuth client registration")
			}
			s = &Session{Version: 1, Server: o.Config.Server, ClientID: id}
		}
		old := s.PendingRevocation
		if s.Tokens != nil {
			old = s.Tokens.RefreshToken
		}
		s.DeviceKey, e = generateKey()
		if e != nil {
			return e
		}
		s.Tokens = nil
		s.Enrollment = nil
		s.CredentialEnrollmentRequested = enroll
		s.CredentialPolicyProfile = profile
		s.PendingRevocation = old
		if e = o.Store.Write(s); e != nil {
			return e
		}
		if old != "" {
			if e = o.revoke(ctx, s, old); e != nil {
				return e
			}
		}
		s.PendingRevocation = ""
		if e = o.Store.Write(s); e != nil {
			return e
		}
		verifier, state := randomText(), randomText()
		hash := sha256.Sum256([]byte(verifier))
		ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
		defer cancel()
		listener, e := newCallback(ctx, path, state, o.Config.Origin)
		if e != nil {
			return e
		}
		defer listener.close()
		u, _ := url.Parse(o.Config.Origin + "/mcp/oauth/authorize")
		q := url.Values{"response_type": {"code"}, "client_id": {s.ClientID}, "redirect_uri": {listener.redirect}, "scope": {Scope}, "resource": {o.Config.Server}, "state": {state}, "code_challenge": {base64.RawURLEncoding.EncodeToString(hash[:])}, "code_challenge_method": {"S256"}}
		if enroll {
			q.Set("credential_public_key", s.DeviceKey.PublicKey)
		}
		u.RawQuery = q.Encode()
		if e = progress(LoginStep{"oauth_consent_required", "Sign in to Reah, select businesses and duration, and authorize. Keep this command running for the callback. A new login replaces this profile's previous connection.", u.String()}); e != nil {
			return e
		}
		var callback callbackResult
		select {
		case callback = <-listener.result:
		case <-ctx.Done():
			return errors.New("authorization timed out or was cancelled; start a new login")
		}
		if callback.code == "" {
			callback.finish(false)
			return errors.New("Reah authorization was declined")
		}
		d, e := o.request(ctx, "/mcp/oauth/token", url.Values{"grant_type": {"authorization_code"}, "client_id": {s.ClientID}, "resource": {o.Config.Server}, "code": {callback.code}, "code_verifier": {verifier}, "redirect_uri": {listener.redirect}}, "")
		if e != nil {
			callback.finish(false)
			return e
		}
		s.Tokens, e = parseTokens(d)
		if e != nil {
			callback.finish(false)
			return e
		}
		if e = o.Store.Write(s); e != nil {
			callback.finish(false)
			return e
		}
		callback.finish(true)
		if e = progress(LoginStep{"oauth_connected", "OAuth credentials are saved. This alone does not enable financial signing.", ""}); e != nil {
			return e
		}
		if !enroll {
			return nil
		}
		if e = o.prepareKey(ctx, s); e != nil {
			return e
		}
		if s.Enrollment.State == "read_only" {
			return progress(LoginStep{"read_only_connected", readOnlySigningMessage, ""})
		}
		u, _ = url.Parse(page)
		q = u.Query()
		q.Set("connectionId", s.Enrollment.ConnectionID)
		u.RawQuery = q.Encode()
		return progress(LoginStep{"credential_registration_required", registrationInstructions, u.String()})
	})
}
func (o *OAuth) Logout(ctx context.Context) error {
	return o.Store.Exclusive(ctx, func() error {
		s, e := o.Store.Read()
		if e != nil || s == nil {
			return e
		}
		old := s.PendingRevocation
		if s.Tokens != nil {
			old = s.Tokens.RefreshToken
		}
		s.Tokens = nil
		s.PendingRevocation = old
		if e = o.Store.Write(s); e != nil {
			return e
		}
		if old != "" {
			if e = o.revoke(ctx, s, old); e != nil {
				return errors.New("local access is disabled, but remote revocation is unconfirmed; disconnect this app in Reah Settings")
			}
		}
		return o.Store.Remove()
	})
}
func (o *OAuth) Status(ctx context.Context) (document, error) {
	var out document
	e := o.Store.Exclusive(ctx, func() error {
		s, e := o.Store.Read()
		if e != nil {
			return e
		}
		out = document{"server": o.Config.Server, "profile": o.Config.Profile, "credentialsSaved": s != nil && s.Tokens != nil, "revocationPending": s != nil && s.PendingRevocation != ""}
		out["readOnlyStatus"], out["signingStatus"] = "not_checked", "not_requested"
		out["phase"] = "login_required"
		out["nextAction"] = o.action("login", "Authorize a new connection. Login replaces this profile's previous grant.", "login")
		if s != nil {
			if s.PendingRevocation != "" {
				out["recoveryNote"] = "Local access was disabled before an unconfirmed refresh or revocation. Never replay the saved refresh token manually; a new login will attempt revocation before fresh consent."
			}
			out["publicKey"] = s.DeviceKey.PublicKey
			if s.Tokens != nil {
				out["accessExpiresAt"] = time.UnixMilli(s.Tokens.ExpiresAt).UTC().Format(time.RFC3339)
				out["phase"] = "local_credentials_saved"
				out["nextAction"] = o.action("check_connection", "Verify the live connection before claiming readiness.", "status", "--check")
			}
			out["credentialEnrollmentRequested"] = s.CredentialEnrollmentRequested
			if s.CredentialEnrollmentRequested {
				out["policyProfile"] = policyProfile(s)
				out["signingStatus"] = "not_checked"
				if s.Tokens == nil {
					out["nextAction"] = o.action("login", "Authorize a new connection with the original signing policy. Login replaces the previous grant.", "login", "--enroll-key", "--policy-profile", policyProfile(s))
				} else if s.Enrollment == nil {
					out["phase"] = "signing_not_checked"
					out["nextAction"] = o.action("check_connection", "Check current signing eligibility before attempting enrollment. Read-only OAuth may still work.", "status", "--check")
				} else {
					out["phase"] = "registration_unverified"
				}
			}
			if s.Enrollment != nil {
				out["enrollment"] = s.Enrollment
				if s.Tokens != nil && s.Enrollment.State == "read_only" {
					out["phase"], out["signingStatus"] = "read_only_connected", "read_only"
					out["nextAction"] = o.action("check_connection", "Verify the saved read-only connection. Financial signing is not enabled for the selected businesses.", "status", "--check")
				}
			}
		}
		return nil
	})
	return out, e
}

type callbackResult struct {
	code   string
	finish func(bool)
}
type callback struct {
	redirect string
	result   chan callbackResult
	close    func()
}

func newCallback(ctx context.Context, path, state, issuer string) (*callback, error) {
	l, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		return nil, errors.New("cannot open local authorization callback")
	}
	result := make(chan callbackResult, 1)
	host := l.Addr().String()
	var mu sync.Mutex
	used := false
	s := &http.Server{ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 5 * time.Second, MaxHeaderBytes: 16 * 1024}
	s.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		q, e := url.ParseQuery(r.URL.RawQuery)
		valid := e == nil
		for k, v := range q {
			if len(v) != 1 || (k != "state" && k != "code" && k != "iss" && k != "error" && k != "error_description" && k != "error_uri") {
				valid = false
			}
		}
		if !valid || r.Method != "GET" || r.Host != host || r.URL.Path != path || len(r.Header.Values("Origin")) != 0 || subtle.ConstantTimeCompare([]byte(q.Get("state")), []byte(state)) != 1 || (q.Get("iss") != "" && q.Get("iss") != issuer) || (q.Get("code") == "") == (q.Get("error") == "") {
			http.Error(w, "Invalid authorization callback", 400)
			return
		}
		mu.Lock()
		if used {
			mu.Unlock()
			http.Error(w, "Authorization callback already received", 409)
			return
		}
		used = true
		mu.Unlock()
		finished := make(chan bool, 1)
		result <- callbackResult{q.Get("code"), func(ok bool) { finished <- ok }}
		select {
		case ok := <-finished:
			if ok {
				_, _ = io.WriteString(w, "Authentication complete. You may close this window.")
			} else {
				http.Error(w, "Authorization could not be completed. Start a new login.", 400)
			}
		case <-ctx.Done():
			http.Error(w, "Authorization expired", 400)
		}
	})
	go func() { _ = s.Serve(l) }()
	return &callback{"http://" + host + path, result, func() {
		shutdown, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := s.Shutdown(shutdown); err != nil {
			_ = s.Close()
		}
	}}, nil
}
