package client

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestStatusGivesProfileSpecificRecoveryWithoutSecretsOrNetwork(t *testing.T) {
	c, _ := Configure(DefaultServer, "my-signing-profile")
	s := &Session{DeviceKey: testKey(t), CredentialEnrollmentRequested: true, CredentialPolicyProfile: "financial-operations-v1", PendingRevocation: "secret-refresh"}
	o := &OAuth{c, &memoryStore{s: s}, &http.Client{Transport: roundTripper(func(*http.Request) (*http.Response, error) {
		t.Fatal("local status used the network")
		return nil, nil
	})}}
	out, err := (&Remote{OAuth: o}).CheckStatus(context.Background())
	if err != nil || out["phase"] != "login_required" {
		t.Fatalf("missing local recovery: %v %v", out, err)
	}
	args := out["nextAction"].(document)["args"].([]string)
	want := []string{"login", "--enroll-key", "--policy-profile", "financial-operations-v1", "--no-browser", "--json", "--server", c.Server, "--profile", c.Profile}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("wrong recovery command: %v", args)
	}
	b, _ := json.Marshal(out)
	for _, secret := range []string{s.DeviceKey.PrivateKey, "secret-refresh", "refreshToken", "privateKey"} {
		if strings.Contains(string(b), secret) {
			t.Fatal("status leaked a credential")
		}
	}
}

func discoveryFixture(origin string) document {
	return document{"issuer": origin, "code_challenge_methods_supported": []string{"S256"}, "scopes_supported": []string{Scope}, "authorization_endpoint": origin + "/mcp/oauth/authorize", "token_endpoint": origin + "/mcp/oauth/token", "revocation_endpoint": origin + "/mcp/oauth/revoke", "registration_endpoint": origin + "/mcp/oauth/register", "reah_credential_enrollment_supported": true, "reah_credential_registration_uri": origin + "/mcp/credentials"}
}

func TestResumeRegistrationPreservesGrantAndKey(t *testing.T) {
	var origin string
	var credential document
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/oauth-protected-resource/mcp":
			_ = json.NewEncoder(w).Encode(document{"resource": origin + "/mcp", "authorization_servers": []string{origin}})
		case "/.well-known/oauth-authorization-server":
			_ = json.NewEncoder(w).Encode(discoveryFixture(origin))
		case "/mcp/credentials/status":
			_ = json.NewEncoder(w).Encode(credential)
		default:
			t.Errorf("resume unexpectedly modified or reproved credentials: %s", r.URL.Path)
			http.Error(w, "unexpected", 500)
		}
	}))
	defer server.Close()
	origin = server.URL
	c, _ := Configure(origin+"/mcp", "resume")
	s := &Session{Version: 1, Server: c.Server, ClientID: "client", DeviceKey: testKey(t), Tokens: &Tokens{"saved-access", "saved-refresh", time.Now().Add(time.Hour).UnixMilli()}, CredentialEnrollmentRequested: true}
	credential = proofFixture(t, c, s)
	credential["state"] = "awaiting_provider"
	s.Enrollment = &Receipt{testID, "awaiting_provider", stringAt(credential, "credential_expires_at"), digest(stringAt(credential, "proof_payload"))}
	store := &memoryStore{s: s}
	before, _ := json.Marshal(s)
	o := &OAuth{c, store, server.Client()}
	var step LoginStep
	if err := o.ResumeRegistration(context.Background(), func(value LoginStep) error { step = value; return nil }); err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(step.URL)
	if step.Stage != "credential_registration_required" || u.Path != "/mcp/credentials" || u.Query().Get("connectionId") != testID || !strings.Contains(step.Message, "REAUTH_REQUIRED") {
		t.Fatalf("missing browser registration recovery: %+v", step)
	}
	after, _ := json.Marshal(store.s)
	if string(before) != string(after) {
		t.Fatal("resume changed the existing grant or key")
	}
	credential["state"], credential["proof_payload"] = "expired", ""
	if err := o.ResumeRegistration(context.Background(), func(LoginStep) error { t.Fatal("opened a closed registration"); return nil }); err == nil {
		t.Fatal("resumed expired enrollment")
	}
}

func TestOAuthStagesDistinguishConsentAndFinancialRegistration(t *testing.T) {
	var origin string
	var o *OAuth
	var proof document
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var d document
		switch r.URL.Path {
		case "/.well-known/oauth-protected-resource/mcp":
			d = document{"resource": origin + "/mcp", "authorization_servers": []string{origin}}
		case "/.well-known/oauth-authorization-server":
			d = discoveryFixture(origin)
		case "/mcp/oauth/register":
			d = document{"client_id": "client"}
		case "/mcp/oauth/token":
			d = document{"access_token": "access", "refresh_token": "refresh", "expires_in": 900, "scope": Scope, "token_type": "Bearer"}
		case "/mcp/credentials/prepare":
			s, _ := o.Store.Read()
			proof = proofFixture(t, o.Config, s)
			d = proof
		case "/mcp/credentials/prove":
			proof["state"] = "awaiting_provider"
			d = proof
		default:
			t.Errorf("unexpected login request: %s", r.URL.Path)
			http.Error(w, "unexpected", 500)
			return
		}
		_ = json.NewEncoder(w).Encode(d)
	}))
	defer server.Close()
	origin = server.URL
	c, _ := Configure(origin+"/mcp", "stages")
	o = &OAuth{c, &memoryStore{}, server.Client()}
	var stages []string
	callbackDone := make(chan error, 1)
	err := o.LoginWithProgress(context.Background(), true, "wallet-transfer-v1", func(step LoginStep) error {
		stages = append(stages, step.Stage)
		if step.Stage == "oauth_consent_required" {
			u, _ := url.Parse(step.URL)
			q := u.Query()
			callback, _ := url.Parse(q.Get("redirect_uri"))
			callback.RawQuery = url.Values{"code": {"code"}, "state": {q.Get("state")}, "iss": {origin}}.Encode()
			go func() {
				response, err := http.Get(callback.String())
				if err == nil {
					_ = response.Body.Close()
				}
				callbackDone <- err
			}()
		}
		if step.Stage == "credential_registration_required" {
			u, _ := url.Parse(step.URL)
			if u.Query().Get("connectionId") != testID || !strings.Contains(step.Message, "not yet verified") {
				t.Error("second page not identified as pending financial registration")
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := <-callbackDone; err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(stages, []string{"oauth_consent_required", "oauth_connected", "credential_registration_required"}) {
		t.Fatalf("wrong stage order: %v", stages)
	}
	status, _ := o.Status(context.Background())
	if status["phase"] != "registration_unverified" || status["signingStatus"] != "not_checked" {
		t.Fatal("login falsely claimed signing readiness")
	}
}

func TestLiveStatusVerifiesEveryBusinessAndFailsClosed(t *testing.T) {
	for _, scenario := range []string{"all_ready", "partial", "wrong_key", "wrong_connection", "wrong_policy", "catalog_read_only", "tampered_entities", "expired_registered", "expired_partial", "network_failure", "revoked"} {
		t.Run(scenario, func(t *testing.T) {
			ctx := context.Background()
			var origin string
			var credential document
			key := testKey(t)
			var bindingCalls atomic.Int32
			upstream := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1"}, nil)
			upstream.AddTool(&mcp.Tool{Name: "read", InputSchema: document{"type": "object"}, Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}}, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				t.Error("status invoked a business tool")
				return nil, nil
			})
			if scenario != "catalog_read_only" {
				upstream.AddTool(&mcp.Tool{Name: "future_signed_tool", InputSchema: document{"type": "object"}, Annotations: &mcp.ToolAnnotations{}, Meta: mcp.Meta{localMeta: document{"version": 1, "mode": "signed", "signing": "approval-send-v2"}}}, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
					t.Fatal("diagnosis attempted a financial operation")
					return nil, nil
				})
			}
			upstream.AddTool(&mcp.Tool{Name: "reah_signing_context", InputSchema: document{"type": "object"}, Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}}, func(_ context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				bindingCalls.Add(1)
				var args document
				_ = json.Unmarshal(req.Params.Arguments, &args)
				id := stringAt(args, "entityId")
				if (scenario == "partial" || scenario == "expired_partial") && id == testOtherID {
					return &mcp.CallToolResult{IsError: true, StructuredContent: document{"error": document{"code": "CREDENTIAL_REQUIRED"}}}, nil
				}
				c := testContext(key, origin+"/mcp")
				c.ConnectionID = testID
				c.Binding.EntityID, c.Binding.PolicyProfile = id, "wallet-transfer-v1"
				switch scenario {
				case "wrong_key":
					c.Binding.PublicKey = "wrong-key"
				case "wrong_connection":
					c.ConnectionID = testOtherID
				case "wrong_policy":
					c.Binding.PolicyProfile = "financial-operations-v1"
				}
				return &mcp.CallToolResult{Content: []mcp.Content{}, StructuredContent: c}, nil
			})
			handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return upstream }, &mcp.StreamableHTTPOptions{Stateless: true})
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				if req.Header.Get("Authorization") != "Bearer access" {
					t.Error("missing authenticated read")
				}
				if scenario == "network_failure" {
					http.Error(w, "secret upstream body", 503)
					return
				}
				if req.URL.Path == "/mcp/credentials/status" {
					if req.Method != "POST" {
						t.Error("wrong status method")
					}
					_ = json.NewEncoder(w).Encode(credential)
					return
				}
				handler.ServeHTTP(w, req)
			}))
			defer server.Close()
			origin = server.URL
			config, _ := Configure(origin+"/mcp", "live")
			s := &Session{Version: 1, Server: config.Server, ClientID: "client", DeviceKey: key, Tokens: &Tokens{"access", "refresh", time.Now().Add(time.Hour).UnixMilli()}, CredentialEnrollmentRequested: true}
			credential = proofFixture(t, config, s)
			credential["state"] = "awaiting_provider"
			s.Enrollment = &Receipt{testID, "awaiting_provider", stringAt(credential, "credential_expires_at"), digest(stringAt(credential, "proof_payload"))}
			if scenario == "expired_registered" || scenario == "expired_partial" {
				credential["state"], credential["proof_payload"] = "expired", ""
			}
			if scenario == "tampered_entities" {
				credential["entity_ids"] = []any{testID}
			}
			if scenario == "revoked" {
				credential["state"], credential["proof_payload"] = "revoked", ""
			}
			store := &memoryStore{s: s}
			before, _ := json.Marshal(s)
			remote := &Remote{OAuth: &OAuth{config, store, server.Client()}, Version: "test"}
			defer remote.Close()
			out, err := remote.CheckStatus(ctx)
			wantPhase := map[string]string{"all_ready": "signing_ready", "partial": "credential_registration_required", "wrong_key": "signing_check_failed", "wrong_connection": "signing_check_failed", "wrong_policy": "signing_check_failed", "catalog_read_only": "server_capability_unavailable", "tampered_entities": "signing_check_failed", "expired_registered": "signing_ready", "expired_partial": "registration_expired", "network_failure": "connection_check_failed", "revoked": "credential_inactive"}[scenario]
			if out["phase"] != wantPhase {
				t.Fatalf("phase = %v, want %s; %v", out, wantPhase, err)
			}
			if scenario != "network_failure" && scenario != "tampered_entities" && err != nil {
				t.Fatal(err)
			}
			after, _ := json.Marshal(store.s)
			if string(before) != string(after) {
				t.Fatal("diagnosis changed the grant or signing key")
			}
			if checks, ok := out["businesses"]; ok && (len(checks.([]document)) != 2 || bindingCalls.Load() != 2) {
				t.Fatal("diagnosis did not cover both enrolled businesses")
			}
			b, _ := json.Marshal(out)
			if strings.Contains(string(b), "secret upstream body") || strings.Contains(string(b), key.PrivateKey) {
				t.Fatal("live diagnosis leaked secrets")
			}
		})
	}
}

func TestSafeOAuthErrorsAndTransportWrapping(t *testing.T) {
	for _, item := range []struct {
		status     int
		body, code string
	}{
		{401, `{"error":"invalid_token","error_description":"secret-token"}`, "login_required"},
		{400, `{"error":"invalid_grant"}`, "login_required"},
		{409, `{"error":"credential_not_ready"}`, "credential_not_ready"},
		{403, "<html>secret-token</html>", "access_denied"},
		{429, "", "rate_limited"},
		{503, `{"error":"secret-token"}`, "request_failed"},
	} {
		err := oauthHTTPError(&http.Response{StatusCode: item.status, Body: io.NopCloser(strings.NewReader(item.body))}, "/mcp/oauth/register")
		if !strings.Contains(err.Error(), item.code) || strings.Contains(err.Error(), "secret-token") {
			t.Fatal("missing safe error classification", err)
		}
		wrapped := &url.Error{Op: "Post", URL: "https://secret-token.example", Err: err}
		if safe := safeConnectionError(wrapped); safe != err || strings.Contains(safe.Error(), "https://secret-token") {
			t.Fatal("transport wrapping lost the safe cause")
		}
	}
	if safeConnectionError(errors.New("secret-token")) != errConnection {
		t.Fatal("unknown transport cause leaked")
	}
}

func TestDiagnosticRemainsDiscoverableBeforeLogin(t *testing.T) {
	config, _ := Configure(DefaultServer, "not-connected")
	remote := &Remote{OAuth: &OAuth{Config: config, Store: &memoryStore{}}}
	server := NewServer(remote, "test")
	st, ct := mcp.NewInMemoryTransports()
	ss, err := server.Connect(context.Background(), st, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	host := mcp.NewClient(&mcp.Implementation{Name: "host", Version: "test"}, nil)
	cs, err := host.Connect(context.Background(), ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	list, err := cs.ListTools(context.Background(), nil)
	if err != nil || len(list.Tools) != 1 || list.Tools[0].Name != connectionStatusToolName {
		t.Fatalf("recovery tool unavailable: %v %v", list, err)
	}
	result, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: connectionStatusToolName, Arguments: document{"check": true}})
	if err != nil || result.IsError {
		t.Fatalf("diagnosis failed: %v", err)
	}
	b, _ := json.Marshal(result.StructuredContent)
	if !strings.Contains(string(b), "login_required") || !strings.Contains(string(b), "not-connected") {
		t.Fatal("missing next action")
	}
	for _, args := range []document{{"check": "true"}, {"token": "secret"}} {
		result, err = cs.CallTool(context.Background(), &mcp.CallToolParams{Name: connectionStatusToolName, Arguments: args})
		if err != nil || !result.IsError {
			t.Fatal("invalid diagnostic arguments accepted")
		}
	}
}
