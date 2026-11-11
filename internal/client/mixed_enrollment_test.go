package client

import (
	"context"
	"encoding/json"
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

func readOnlyFixture(t *testing.T, c Config, s *Session) document {
	t.Helper()
	d := proofFixture(t, c, s)
	d["state"], d["entity_ids"], d["proof_payload"] = "read_only", []any{}, ""
	d["enrollment_expires_at"] = d["credential_expires_at"]
	return d
}

func TestReadOnlyEnrollmentRejectsMalformedOrDowngradedProof(t *testing.T) {
	c, _ := Configure(DefaultServer, "readonly")
	s := &Session{Server: c.Server, ClientID: "client", DeviceKey: testKey(t), CredentialEnrollmentRequested: true}
	valid := readOnlyFixture(t, c, s)
	receipt, _, err := parseEnrollment(valid, c, s)
	if err != nil || receipt.State != "read_only" || receipt.ProofHash != digest("") {
		t.Fatal("valid read-only receipt rejected", err)
	}
	for _, mutate := range []func(document){
		func(d document) { d["entity_ids"] = []any{testID} },
		func(d document) { d["proof_payload"] = "unexpected proof" },
		func(d document) { delete(d, "proof_payload") },
		func(d document) { d["proof_payload"] = nil },
		func(d document) { d["public_key"] = "wrong-key" },
		func(d document) { d["public_key_fingerprint"] = "wrong-fingerprint" },
		func(d document) { d["enrollment_expires_at"] = time.Now().Add(time.Minute).Format(time.RFC3339Nano) },
		func(d document) { d["entity_ids"] = nil },
		func(d document) { d["connection_id"] = "not-a-uuid" },
		func(d document) {
			d["credential_expires_at"] = time.Now().Add(-time.Hour).Format(time.RFC3339Nano)
			d["enrollment_expires_at"] = d["credential_expires_at"]
		},
	} {
		d := cloneDocument(valid)
		mutate(d)
		if _, _, err := parseEnrollment(d, c, s); err == nil {
			t.Fatal("accepted malformed read-only response")
		}
	}
	s.Enrollment = &Receipt{testID, "awaiting_provider", stringAt(valid, "credential_expires_at"), digest("frozen proof")}
	if _, _, err := parseEnrollment(valid, c, s); err == nil {
		t.Fatal("silently downgraded a frozen signing proof")
	}
	s.Enrollment = receipt
	expanded := proofFixture(t, c, s)
	// A read-only marker has never signed a roster; explicit enrollment may
	// establish its first proof, with the same connection/expiry and full validation.
	expanded["credential_expires_at"] = valid["credential_expires_at"]
	var payload document
	_ = json.Unmarshal([]byte(stringAt(expanded, "proof_payload")), &payload)
	payload["credentialExpiresAt"] = valid["credential_expires_at"]
	raw, _ := json.Marshal(payload)
	expanded["proof_payload"] = string(raw)
	if _, _, err := parseEnrollment(expanded, c, s); err != nil {
		t.Fatal("unsigned read-only marker could not establish first proof", err)
	}
}

func TestReadOnlyEnrollmentAndResumeNeverSignOrOpenProvider(t *testing.T) {
	var origin string
	var credential document
	var prepareCalls atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var d document
		switch r.URL.Path {
		case "/.well-known/oauth-protected-resource/mcp":
			d = document{"resource": origin + "/mcp", "authorization_servers": []string{origin}}
		case "/.well-known/oauth-authorization-server":
			d = discoveryFixture(origin)
		case "/mcp/credentials/prepare":
			prepareCalls.Add(1)
			d = credential
		case "/mcp/credentials/status":
			d = credential
		default:
			t.Error("read-only enrollment attempted an unexpected operation", r.URL.Path)
			http.Error(w, "unexpected", 500)
			return
		}
		_ = json.NewEncoder(w).Encode(d)
	}))
	defer server.Close()
	origin = server.URL
	c, _ := Configure(origin+"/mcp", "readonly")
	s := &Session{Version: 1, Server: c.Server, ClientID: "client", DeviceKey: testKey(t), Tokens: &Tokens{"access", "refresh", time.Now().Add(time.Hour).UnixMilli()}, CredentialEnrollmentRequested: true}
	credential = readOnlyFixture(t, c, s)
	store := &memoryStore{s: s}
	o := &OAuth{c, store, server.Client()}
	if err := o.EnrollKey(context.Background()); err != nil {
		t.Fatal(err)
	}
	if store.s.Enrollment.State != "read_only" {
		t.Fatal("missing compact read-only marker")
	}
	if err := o.ResumeRegistration(context.Background(), func(step LoginStep) error {
		if step.Stage != "read_only_connected" || step.URL != "" || !strings.Contains(step.Message, "Read access") {
			t.Error("opened provider registration for read-only connection")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if prepareCalls.Load() != 2 {
		t.Fatal("explicit resume did not recheck availability")
	}
	if _, err := o.signingKey(context.Background()); err == nil || !strings.Contains(err.Error(), "Read access") || strings.Contains(err.Error(), "run enroll-key") {
		t.Fatal("read-only write attempt did not explain permissions", err)
	}
	raw, _ := json.Marshal(store.s)
	if len(raw) > 2500 || strings.Contains(string(raw), "entity_ids") {
		t.Fatal("read-only receipt exceeded compact vault contract")
	}
}

func TestMixedStatusVerifiesEnrolledSubsetAndKeepsOtherReadAccess(t *testing.T) {
	for _, mode := range []string{"mixed", "read_only", "read_only_missing_receipt", "missing_receipt", "changed_proof", "duration_error", "entity_error", "unknown_error", "missing_enrollment_error", "not_ready_error"} {
		t.Run(mode, func(t *testing.T) {
			var origin string
			var credential document
			var contextCalls, financialCalls atomic.Int32
			key := testKey(t)
			upstream := mcp.NewServer(&mcp.Implementation{Name: "mixed-fixture", Version: "1"}, nil)
			upstream.AddTool(&mcp.Tool{Name: "read", InputSchema: document{"type": "object"}, Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}}, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				return &mcp.CallToolResult{Content: []mcp.Content{}, StructuredContent: document{"entities": []string{testID, testOtherID}}}, nil
			})
			upstream.AddTool(&mcp.Tool{Name: "test_signed", InputSchema: document{"type": "object"}, Annotations: &mcp.ToolAnnotations{}, Meta: mcp.Meta{localMeta: document{"version": 1, "mode": "signed", "signing": "approval-send-v2"}}}, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				financialCalls.Add(1)
				return nil, nil
			})
			upstream.AddTool(&mcp.Tool{Name: "reah_signing_context", InputSchema: document{"type": "object"}, Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}}, func(_ context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				contextCalls.Add(1)
				var args document
				_ = json.Unmarshal(request.Params.Arguments, &args)
				if args["entityId"] == testOtherID {
					return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{}, StructuredContent: document{"error": document{"code": "CREDENTIAL_REQUIRED", "message": "This business has read access only."}}}, nil
				}
				c := testContext(key, origin+"/mcp")
				c.ConnectionID = testID
				c.Binding.EntityID = testID
				c.Binding.PolicyProfile = "wallet-transfer-v1"
				return &mcp.CallToolResult{Content: []mcp.Content{}, StructuredContent: c}, nil
			})
			handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return upstream }, &mcp.StreamableHTTPOptions{Stateless: true})
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/mcp/credentials/status" {
					if strings.HasSuffix(mode, "_error") {
						w.WriteHeader(http.StatusConflict)
					}
					_ = json.NewEncoder(w).Encode(credential)
					return
				}
				if r.URL.Path != "/mcp" {
					t.Error("status prepared or proved a key", r.URL.Path)
					http.Error(w, "unexpected", 500)
					return
				}
				handler.ServeHTTP(w, r)
			}))
			defer server.Close()
			origin = server.URL
			c, _ := Configure(origin+"/mcp", "mixed")
			s := &Session{Version: 1, Server: c.Server, ClientID: "client", DeviceKey: key, Tokens: &Tokens{"access", "refresh", time.Now().Add(time.Hour).UnixMilli()}, CredentialEnrollmentRequested: true}
			credential = proofFixture(t, c, s)
			credential["state"] = "awaiting_provider"
			var proof document
			_ = json.Unmarshal([]byte(stringAt(credential, "proof_payload")), &proof)
			proof["targets"] = proof["targets"].([]any)[:1]
			raw, _ := json.Marshal(proof)
			credential["proof_payload"] = string(raw)
			credential["entity_ids"] = []any{testID}
			s.Enrollment = &Receipt{testID, "awaiting_provider", stringAt(credential, "credential_expires_at"), digest(string(raw))}
			if strings.HasPrefix(mode, "read_only") {
				credential = readOnlyFixture(t, c, s)
				s.Enrollment = &Receipt{testID, "read_only", stringAt(credential, "credential_expires_at"), digest("")}
			}
			if mode == "missing_receipt" || mode == "read_only_missing_receipt" {
				s.Enrollment = nil
			}
			if mode == "changed_proof" {
				credential["proof_payload"] = string(raw) + " "
			}
			if strings.HasSuffix(mode, "_error") {
				s.Enrollment = nil
				switch mode {
				case "entity_error":
					credential = entityPolicyFixture()
				case "duration_error":
					credential = document{"error": "financial_authorization_too_long", "reason": financialAuthorizationTooLong, "next_action": "reauthorize", "retryable": false, "max_authorization_seconds": 86400}
				case "missing_enrollment_error":
					credential = document{"error": "credential_not_ready", "reason": credentialEnrollmentNotFound, "next_action": "enroll_key", "retryable": false, "error_description": "secret-provider"}
				case "not_ready_error":
					credential = document{"error": "credential_not_ready", "reason": "CREDENTIAL_NOT_READY", "next_action": "inspect_connection", "retryable": false, "error_description": "secret-provider"}
				case "unknown_error":
					credential = document{"error": "credential_not_ready", "reason": "UNKNOWN", "error_description": "secret-provider"}
				}
			}
			store := &memoryStore{s: s}
			before, _ := json.Marshal(s)
			remote := &Remote{OAuth: &OAuth{c, store, server.Client()}}
			defer remote.Close()
			out, err := remote.CheckStatus(context.Background())
			if strings.HasSuffix(mode, "_error") {
				if contextCalls.Load() != 0 || out["readOnlyStatus"] != "ready" || store.s.Enrollment != nil {
					t.Fatal("policy status prepared or cached enrollment")
				}
				after, _ := json.Marshal(store.s)
				if string(before) != string(after) {
					t.Fatal("policy diagnosis changed session")
				}
				if mode == "entity_error" {
					if err != nil || out["phase"] != "read_only_ready" || len(out["blockedEntities"].([]blockedSigningEntity)) != 87 {
						t.Fatal("legacy entity policy broke read access")
					}
				} else if mode == "missing_enrollment_error" {
					next := out["nextAction"].(document)
					args := next["args"].([]string)
					if err != nil || out["phase"] != "possession_proof_required" || out["reason"] != credentialEnrollmentNotFound || next["action"] != "resume_registration" || len(args) == 0 || args[0] != "enroll-key" || !strings.Contains(strings.Join(args, " "), "--profile mixed") {
						t.Fatal("interrupted preparation lost grant-preserving explicit recovery", out, err)
					}
				} else if err == nil {
					t.Fatal("missing signing diagnosis")
				}
				if mode == "duration_error" && out["reason"] != financialAuthorizationTooLong {
					t.Fatal("lost duration reason")
				}
				if (mode == "unknown_error" || mode == "not_ready_error") && out["nextAction"].(document)["action"] != "inspect_connection" {
					t.Fatal("unknown policy advised blind enrollment")
				}
				return
			}
			if mode == "changed_proof" {
				if err == nil {
					t.Fatal("adopted mutated frozen proof")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if out["readOnlyStatus"] != "ready" || out["readOnlyScope"] != "all_authorized_businesses" {
				t.Fatal("read access was blocked")
			}
			switch mode {
			case "read_only", "read_only_missing_receipt":
				if out["phase"] != "read_only_ready" || out["signingStatus"] != "read_only" || out["signingScope"] != "none" || contextCalls.Load() != 0 {
					t.Fatal("read-only status suggested enrollment", out)
				}
			case "missing_receipt":
				if out["phase"] != "possession_proof_required" || contextCalls.Load() != 0 || store.s.Enrollment != nil {
					t.Fatal("status adopted an unconfirmed receipt", out)
				}
			case "mixed":
				if out["phase"] != "signing_ready" || out["signingScope"] != "enrolled_businesses" || len(out["businesses"].([]document)) != 1 || contextCalls.Load() != 1 {
					t.Fatal("subset readiness treated as full OAuth readiness", out)
				}
				denied, err := remote.Call(context.Background(), "test_signed", document{"entityId": testOtherID})
				if err != nil || !denied.IsError || financialCalls.Load() != 0 {
					t.Fatal("read-only entity reached financial signing", err)
				}
			}
			_, missingErr := remote.Call(context.Background(), "unadvertised_name", document{})
			if missingErr == nil || !strings.Contains(missingErr.Error(), "tool is unavailable in the current authenticated Reah catalog") || strings.Contains(missingErr.Error(), "enroll-key") {
				t.Fatal("missing catalog tool lost safe denial", missingErr)
			}
			if strings.Contains(missingErr.Error(), "saved connection is read-only") != (mode == "read_only") {
				t.Fatal("missing tool inferred an unsupported business capability", missingErr)
			}
			if financialCalls.Load() != 0 {
				t.Fatal("unadvertised tool reached financial execution")
			}
			result, err := remote.Call(context.Background(), "read", document{})
			if err != nil || result.IsError {
				t.Fatal("read-only tool stopped working", err)
			}
			after, _ := json.Marshal(store.s)
			if !reflect.DeepEqual(before, after) {
				t.Fatal("status changed persisted scope")
			}
		})
	}
}

func TestReadOnlyLoginEmitsConnectedStageWithoutProvider(t *testing.T) {
	var origin string
	var o *OAuth
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
			d = readOnlyFixture(t, o.Config, s)
		default:
			t.Error("read-only login attempted proof/provider operation", r.URL.Path)
			http.Error(w, "unexpected", 500)
			return
		}
		_ = json.NewEncoder(w).Encode(d)
	}))
	defer server.Close()
	origin = server.URL
	c, _ := Configure(origin+"/mcp", "readonly-login")
	o = &OAuth{c, &memoryStore{}, server.Client()}
	var stages []string
	done := make(chan error, 1)
	err := o.LoginWithProgress(context.Background(), true, "wallet-transfer-v1", func(step LoginStep) error {
		stages = append(stages, step.Stage)
		if step.Stage == "oauth_consent_required" {
			u, _ := url.Parse(step.URL)
			callback, _ := url.Parse(u.Query().Get("redirect_uri"))
			callback.RawQuery = url.Values{"code": {"fixture-code"}, "state": {u.Query().Get("state")}, "iss": {origin}}.Encode()
			go func() {
				r, err := http.Get(callback.String())
				if err == nil {
					_ = r.Body.Close()
				}
				done <- err
			}()
		}
		if step.Stage == "read_only_connected" && step.URL != "" {
			t.Error("provider page opened")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(stages, []string{"oauth_consent_required", "oauth_connected", "read_only_connected"}) {
		t.Fatal("wrong read-only login stages", stages)
	}
}
