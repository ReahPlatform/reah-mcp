package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/ReahPlatform/reah-mcp/internal/client"
)

func TestCommandContractWithoutAccessingCredentials(t *testing.T) {
	for _, name := range []string{"login", "status", "logout", "enroll-key", "serve"} {
		cmd := command()
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&out)
		cmd.SetArgs([]string{name, "--help"})
		if e := cmd.Execute(); e != nil {
			t.Fatal(e)
		}
		if !strings.Contains(out.String(), "--profile") || !strings.Contains(out.String(), "--server") {
			t.Fatalf("missing shared flags: %s", name)
		}
		if !strings.Contains(out.String(), `default "https://platform.reah.com/mcp"`) {
			t.Fatalf("missing production server default: %s", name)
		}
	}
	for _, args := range [][]string{{"serve", "extra"}, {"status", "--server", "http://invalid.example/mcp"}, {"login", "--profile", "../../unsafe"}} {
		cmd := command()
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&out)
		cmd.SetArgs(args)
		if e := cmd.Execute(); e == nil {
			t.Errorf("accepted invalid CLI: %v", args)
		}
		if out.Len() != 0 {
			t.Fatal("error wrote help or credentials to stdout")
		}
	}
}

type cliStore struct{ session *client.Session }

func (s *cliStore) Read() (*client.Session, error)    { return s.session, nil }
func (s *cliStore) Write(value *client.Session) error { s.session = value; return nil }
func (s *cliStore) Remove() error                     { s.session = nil; return nil }
func (s *cliStore) Exclusive(ctx context.Context, fn func() error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return fn()
}

func TestStatusCommandPreservesDiagnosticJSONOnCheckFailure(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "private upstream error", 503) }))
	defer server.Close()
	store := &cliStore{session: &client.Session{DeviceKey: client.DeviceKey{PublicKey: "public"}, Tokens: &client.Tokens{AccessToken: "secret-access", RefreshToken: "secret-refresh", ExpiresAt: time.Now().Add(time.Hour).UnixMilli()}}}
	cmd := commandWithOAuth(func(c client.Config) *client.OAuth {
		return &client.OAuth{Config: c, Store: store, HTTP: server.Client()}
	})
	var out, stderr bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"status", "--check", "--server", server.URL + "/mcp", "--profile", "cli-profile"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("failed check exited successfully")
	}
	var status map[string]any
	if err := json.Unmarshal(out.Bytes(), &status); err != nil || status["phase"] != "connection_check_failed" {
		t.Fatalf("missing failure diagnosis: %s, %v", out.String(), err)
	}
	for _, secret := range []string{"secret-access", "secret-refresh", "private upstream error"} {
		if strings.Contains(out.String()+stderr.String(), secret) {
			t.Fatal("CLI leaked credentials or upstream content")
		}
	}
}

func TestResumeCommandEmitsRegistrationStageAndUnverifiedResult(t *testing.T) {
	var origin string
	var credential map[string]any
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		switch r.URL.Path {
		case "/.well-known/oauth-protected-resource/mcp":
			body = map[string]any{"resource": origin + "/mcp", "authorization_servers": []string{origin}}
		case "/.well-known/oauth-authorization-server":
			body = map[string]any{"issuer": origin, "code_challenge_methods_supported": []string{"S256"}, "scopes_supported": []string{"reah.read"}, "authorization_endpoint": origin + "/mcp/oauth/authorize", "token_endpoint": origin + "/mcp/oauth/token", "revocation_endpoint": origin + "/mcp/oauth/revoke", "registration_endpoint": origin + "/mcp/oauth/register", "reah_credential_enrollment_supported": true, "reah_credential_registration_uri": origin + "/mcp/credentials"}
		case "/mcp/credentials/status":
			body = credential
		default:
			t.Error("resume touched an unexpected endpoint", r.URL.Path)
			http.Error(w, "unexpected", 500)
			return
		}
		_ = json.NewEncoder(w).Encode(body)
	}))
	defer server.Close()
	origin = server.URL
	store := &cliStore{session: &client.Session{DeviceKey: client.DeviceKey{PublicKey: "public"}, Tokens: &client.Tokens{AccessToken: "secret-access", RefreshToken: "secret-refresh", ExpiresAt: time.Now().Add(time.Hour).UnixMilli()}, CredentialEnrollmentRequested: true, Enrollment: &client.Receipt{ConnectionID: "11111111-1111-4111-8111-111111111111", State: "awaiting_provider", CredentialExpiresAt: time.Now().Add(time.Hour).UTC().Format(time.RFC3339)}}}
	proof := `{"targets":[{"entityId":"11111111-1111-4111-8111-111111111111"}]}`
	hash := sha256.Sum256([]byte(proof))
	store.session.Enrollment.ProofHash = hex.EncodeToString(hash[:])
	credential = map[string]any{"connection_id": store.session.Enrollment.ConnectionID, "public_key": "public", "credential_expires_at": store.session.Enrollment.CredentialExpiresAt, "entity_ids": []string{store.session.Enrollment.ConnectionID}, "state": "awaiting_provider", "proof_payload": proof}
	cmd := commandWithOAuth(func(c client.Config) *client.OAuth {
		return &client.OAuth{Config: c, Store: store, HTTP: server.Client()}
	})
	var out, stderr bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"enroll-key", "--json", "--no-browser", "--server", origin + "/mcp", "--profile", "resume-profile"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(&out)
	var step client.LoginStep
	if err := decoder.Decode(&step); err != nil || step.Stage != "credential_registration_required" || !strings.Contains(step.URL, "connectionId=") {
		t.Fatalf("missing registration continuation: %+v %v", step, err)
	}
	var event struct {
		Stage  string
		Status map[string]any
	}
	if err := decoder.Decode(&event); err != nil || event.Stage != "local_status" || event.Status["phase"] != "registration_unverified" {
		t.Fatalf("wrong readiness: %+v %v", event, err)
	}
	if strings.Contains(stderr.String(), "login complete") {
		t.Fatal("reported registration completed")
	}
}

func TestLocalStatusDoesNotNeedNetwork(t *testing.T) {
	cmd := commandWithOAuth(func(c client.Config) *client.OAuth {
		return &client.OAuth{Config: c, Store: &cliStore{}, HTTP: &http.Client{Transport: rejectedTransport{}}}
	})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"status", "--profile", "offline-profile"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "login_required") || !strings.Contains(out.String(), "offline-profile") {
		t.Fatal("offline recovery missing")
	}
}

type rejectedTransport struct{}

func (rejectedTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("network must not be used")
}

func TestStdioInitializationWithoutCredentials(t *testing.T) {
	executable, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	child := exec.CommandContext(ctx, executable, "-test.run=^TestStdioHelper$")
	child.Env = append(os.Environ(), "REAH_TEST_STDIO_HELPER=1", "PATH=/usr/bin:/bin")
	var stderr bytes.Buffer
	child.Stderr = &stderr
	in, e := child.StdinPipe()
	if e != nil {
		t.Fatal(e)
	}
	out, e := child.StdoutPipe()
	if e != nil {
		t.Fatal(e)
	}
	if e = child.Start(); e != nil {
		t.Fatal(e)
	}
	defer func() { _ = child.Process.Kill() }()
	encoder := json.NewEncoder(in)
	if e = encoder.Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{"protocolVersion": "2025-11-25", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "test-host", "version": "1"}}}); e != nil {
		t.Fatal(e)
	}
	var reply struct {
		Result struct {
			Capabilities map[string]any `json:"capabilities"`
		} `json:"result"`
	}
	if e = json.NewDecoder(out).Decode(&reply); e != nil {
		t.Fatalf("stdio initialization: %v; %s", e, stderr.String())
	}
	if _, ok := reply.Result.Capabilities["tools"]; !ok {
		t.Fatal("tools capability missing")
	}
	if e = encoder.Encode(map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"}); e != nil {
		t.Fatal(e)
	}
	_ = in.Close()
	if e = child.Wait(); e != nil {
		t.Fatalf("stdio shutdown: %v; %s", e, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("unexpected initialization output: %s", stderr.String())
	}
}

func TestStdioHelper(t *testing.T) {
	if os.Getenv("REAH_TEST_STDIO_HELPER") != "1" {
		return
	}
	cmd := command()
	cmd.SetArgs([]string{"serve"})
	if e := cmd.Execute(); e != nil {
		os.Exit(1)
	}
	os.Exit(0)
}
