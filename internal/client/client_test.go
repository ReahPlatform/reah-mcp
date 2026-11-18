package client

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const testID = "11111111-1111-4111-8111-111111111111"
const testOtherID = "22222222-2222-4222-8222-222222222222"

type memoryStore struct {
	mu sync.Mutex
	s  *Session
}

func (m *memoryStore) Read() (*Session, error) {
	if m.s == nil {
		return nil, nil
	}
	b, _ := json.Marshal(m.s)
	var s Session
	_ = json.Unmarshal(b, &s)
	return &s, nil
}
func (m *memoryStore) Write(s *Session) error {
	b, _ := json.Marshal(s)
	var copy Session
	_ = json.Unmarshal(b, &copy)
	m.s = &copy
	return nil
}
func (m *memoryStore) Remove() error { m.s = nil; return nil }
func (m *memoryStore) Exclusive(ctx context.Context, fn func() error) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if e := ctx.Err(); e != nil {
		return e
	}
	return fn()
}
func testKey(t *testing.T) DeviceKey {
	t.Helper()
	key, e := generateKey()
	if e != nil {
		t.Fatal(e)
	}
	return key
}
func testContext(key DeviceKey, server string) SigningContext {
	c := SigningContext{UserID: testID, ConnectionID: testOtherID, Resource: server}
	c.Binding.EntityID = testID
	c.Binding.PublicKey = key.PublicKey
	c.Binding.SuborgID = testOtherID
	c.Binding.ExpiresAt = time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)
	return c
}
func transferArgs() document {
	return document{"entityId": testID, "requestId": testOtherID, "instrumentAccountId": testID, "walletId": testID, "networkId": testID, "instrumentId": testID, "fromAddress": "0x1111111111111111111111111111111111111111", "toAddress": "0x2222222222222222222222222222222222222222", "amount": "15250000", "asset": "eip155:1/erc20:0x3333333333333333333333333333333333333333", "gasSource": "GAS_SOURCE_GAS_STATION", "notes": "Invoice 1"}
}
func verifyText(t *testing.T, key DeviceKey, text, signature string, der bool) {
	t.Helper()
	k, e := privateKey(key)
	if e != nil {
		t.Fatal(e)
	}
	b, e := hex.DecodeString(signature)
	if e != nil {
		t.Fatal(e)
	}
	h := sha256.Sum256([]byte(text))
	if der {
		if !ecdsa.VerifyASN1(&k.PublicKey, h[:], b) {
			t.Fatal("invalid DER signature")
		}
		return
	}
	if len(b) != 64 {
		t.Fatal("invalid P1363 length")
	}
	point := elliptic.MarshalCompressed(k.Curve, k.X, k.Y)
	x, y := elliptic.UnmarshalCompressed(elliptic.P256(), point)
	pub := ecdsa.PublicKey{Curve: elliptic.P256(), X: x, Y: y}
	if !ecdsa.Verify(&pub, h[:], newInt(b[:32]), newInt(b[32:])) {
		t.Fatal("invalid P1363 signature")
	}
}
func newInt(b []byte) *big.Int { return new(big.Int).SetBytes(b) }

func TestConfigure(t *testing.T) {
	for _, server := range []string{"http://platform.reah.com/mcp", "https://user:secret@platform.reah.com/mcp", "https://platform.reah.com/mcp?x=1", "https://platform.reah.com/mcp#x", "https://platform.reah.com/mcp/", "https://platform.reah.com/%6dcp"} {
		if _, e := Configure(server, "default"); e == nil {
			t.Errorf("accepted %s", server)
		}
	}
	a, e := Configure(DefaultServer, "default")
	if e != nil {
		t.Fatal(e)
	}
	b, _ := Configure(DefaultServer, "other")
	if a.ID == b.ID {
		t.Fatal("profiles share vault identity")
	}
}

func TestExecutionContinuationAndAmbiguity(t *testing.T) {
	key, provider := testKey(t), testKey(t)
	c := testContext(key, DefaultServer)
	pub, _ := privateKey(provider)
	der, _ := x509.MarshalPKIXPublicKey(&pub.PublicKey)
	trusted := string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
	payload, _ := json.Marshal(document{"organizationId": testOtherID, "type": "ACTIVITY_TYPE_SIGN_TRANSACTION_V2", "timestampMs": strconv.FormatInt(time.Now().UnixMilli(), 10), "parameters": document{"signWith": "wallet"}})
	derHex, e := signText(provider, string(payload), true)
	if e != nil {
		t.Fatal(e)
	}
	derSig, _ := hex.DecodeString(derHex)
	item := document{"requestId": "provider-request", "requestPayload": string(payload), "signature": base64.StdEncoding.EncodeToString(derSig)}
	args := document{"entityId": testID, "approvalId": testOtherID, "business": "crypto_convert"}
	calls := 0
	call := func(_ context.Context, name string, input document) (*mcp.CallToolResult, error) {
		if name == "reah_signing_context" {
			return &mcp.CallToolResult{StructuredContent: c}, nil
		}
		calls++
		proof, ok := input["_proof"].(map[string]any)
		if !ok {
			t.Fatal("missing operation proof")
		}
		verifyText(t, key, stringAt(proof, "payload"), stringAt(proof, "signature"), false)
		var decoded document
		_ = json.Unmarshal([]byte(stringAt(proof, "payload")), &decoded)
		bound := decoded["arguments"].(map[string]any)
		if bound["business"] != "crypto_convert" || bound["approvalId"] != testOtherID {
			t.Fatal("operation terms not bound")
		}
		if calls == 1 {
			return &mcp.CallToolResult{StructuredContent: document{"requiresSignatures": []any{item}}}, nil
		}
		stamps, ok := input["signatures"].([]any)
		if !ok || len(stamps) != 1 {
			t.Fatal("signature continuation missing")
		}
		stamp := stamps[0].(map[string]any)
		b, e := base64.RawURLEncoding.DecodeString(stringAt(stamp, "signature"))
		if e != nil {
			t.Fatal(e)
		}
		var s document
		_ = json.Unmarshal(b, &s)
		if s["publicKey"] != key.PublicKey || s["scheme"] != "SIGNATURE_SCHEME_TK_API_P256" {
			t.Fatal("invalid provider stamp")
		}
		verifyText(t, key, string(payload), stringAt(s, "signature"), true)
		return &mcp.CallToolResult{StructuredContent: document{"submitted": true}}, nil
	}
	_, e = callSigned(context.Background(), "execute-test", "turnkey-execution-v1", args, key, DefaultServer, call, trusted)
	if e != nil || calls != 2 {
		t.Fatalf("execution continuation failed: %v (%d)", e, calls)
	}
	mutated := cloneDocument(item)
	mutated["requestPayload"] = string(payload) + " "
	if _, e = executionStamp(key, c, mutated, trusted); e == nil {
		t.Fatal("signed altered provider bytes")
	}
	other := c
	other.Binding.SuborgID = testID
	if _, e = executionStamp(key, other, item, trusted); e == nil {
		t.Fatal("signed another organization")
	}
	unknown := c
	unknown.Resource = "https://untrusted.example/mcp"
	if _, e = executionStamp(key, unknown, item, ""); e == nil {
		t.Fatal("unknown environment accepted")
	}
	attempts := 0
	fail := func(_ context.Context, name string, _ document) (*mcp.CallToolResult, error) {
		if name == "reah_signing_context" {
			return &mcp.CallToolResult{StructuredContent: c}, nil
		}
		attempts++
		return nil, errors.New("lost response")
	}
	_, e = callSigned(context.Background(), "execute-test", "turnkey-execution-v1", args, key, DefaultServer, fail, trusted)
	if e == nil || !strings.Contains(e.Error(), "unconfirmed") || attempts != 1 {
		t.Fatal("ambiguous execution retried or not reported")
	}
}

func TestRefreshRotatesOnceAndNeverReplays(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		_ = r.ParseForm()
		if r.Form.Get("refresh_token") != "old-refresh" {
			t.Error("unexpected refresh token")
		}
		_ = json.NewEncoder(w).Encode(document{"access_token": "fresh-access", "refresh_token": "fresh-refresh", "expires_in": 900, "scope": Scope, "token_type": "Bearer"})
	}))
	defer server.Close()
	c, _ := Configure(server.URL+"/mcp", "test")
	store := &memoryStore{s: &Session{Version: 1, Server: c.Server, ClientID: "client", DeviceKey: testKey(t), Tokens: &Tokens{"expired", "old-refresh", 1}}}
	o := &OAuth{c, store, server.Client()}
	originalKey := store.s.DeviceKey
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			token, e := o.AccessToken(context.Background())
			if e != nil || token != "fresh-access" {
				t.Errorf("refresh: %v", e)
			}
		}()
	}
	wg.Wait()
	if store.s.DeviceKey != originalKey {
		t.Fatal("refresh replaced login key")
	}
	if requests.Load() != 1 {
		t.Fatal("concurrent refresh replayed token")
	}
	store.s.Tokens.ExpiresAt = 1
	o.HTTP = &http.Client{Transport: roundTripper(func(*http.Request) (*http.Response, error) { requests.Add(1); return nil, errors.New("ambiguous") })}
	if _, e := o.AccessToken(context.Background()); e == nil {
		t.Fatal("accepted failed refresh")
	}
	n := requests.Load()
	if _, e := o.AccessToken(context.Background()); e == nil {
		t.Fatal("accepted consumed refresh")
	}
	if requests.Load() != n {
		t.Fatal("replayed consumed refresh")
	}
	if store.s.Tokens != nil || store.s.PendingRevocation != "fresh-refresh" {
		t.Fatal("failed refresh not persisted safely")
	}
}

type roundTripper func(*http.Request) (*http.Response, error)

func (f roundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestOAuthLoginPKCECallbackAndLogout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var origin, challenge, redirect string
	var revoked atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var d document
		switch r.URL.Path {
		case "/.well-known/oauth-protected-resource/mcp":
			d = document{"resource": origin + "/mcp", "authorization_servers": []string{origin}}
		case "/.well-known/oauth-authorization-server":
			d = document{"issuer": origin, "code_challenge_methods_supported": []string{"S256"}, "scopes_supported": []string{Scope}}
			for field, name := range map[string]string{"authorization_endpoint": "authorize", "token_endpoint": "token", "revocation_endpoint": "revoke", "registration_endpoint": "register"} {
				d[field] = origin + "/mcp/oauth/" + name
			}
		case "/mcp/oauth/register":
			var input document
			_ = json.NewDecoder(r.Body).Decode(&input)
			if input["token_endpoint_auth_method"] != "none" {
				t.Error("not a native public client")
			}
			d = document{"client_id": "registered-client"}
		case "/mcp/oauth/token":
			_ = r.ParseForm()
			h := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
			if base64.RawURLEncoding.EncodeToString(h[:]) != challenge || r.Form.Get("redirect_uri") != redirect || r.Form.Get("code") != "one-use-code" {
				t.Error("incorrect PKCE exchange")
			}
			d = document{"access_token": "access", "refresh_token": "refresh", "expires_in": 900, "scope": Scope, "token_type": "Bearer"}
		case "/mcp/oauth/revoke":
			_ = r.ParseForm()
			if r.Form.Get("token") != "refresh" {
				t.Error("incorrect revoke")
			}
			revoked.Add(1)
			return
		default:
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(d)
	}))
	defer server.Close()
	origin = server.URL
	c, _ := Configure(origin+"/mcp", "test")
	store := &memoryStore{}
	o := &OAuth{c, store, server.Client()}
	completed := make(chan error, 1)
	login := func() error {
		return o.Login(ctx, false, func(address string) error {
			u, e := url.Parse(address)
			if e != nil {
				return e
			}
			q := u.Query()
			challenge = q.Get("code_challenge")
			redirect = q.Get("redirect_uri")
			callback, _ := url.Parse(redirect)
			values := url.Values{"state": {q.Get("state")}, "code": {"one-use-code"}, "iss": {origin}}
			callback.RawQuery = values.Encode()
			go func() {
				bad := *callback
				bq := bad.Query()
				bq.Set("state", "wrong")
				bad.RawQuery = bq.Encode()
				response, e := http.Get(bad.String())
				if e != nil {
					completed <- e
					return
				}
				_ = response.Body.Close()
				if response.StatusCode != 400 {
					completed <- errors.New("wrong state accepted")
					return
				}
				response, e = http.Get(callback.String())
				if e == nil {
					defer response.Body.Close()
					b, _ := io.ReadAll(response.Body)
					if response.StatusCode != 200 || !strings.Contains(string(b), "Authentication complete") {
						e = errors.New("callback did not complete")
					}
				}
				completed <- e
			}()
			return nil
		})
	}
	e := login()
	if e != nil {
		t.Fatal(e)
	}
	if e = <-completed; e != nil {
		t.Fatal(e)
	}
	if store.s == nil || store.s.Tokens == nil || store.s.Tokens.AccessToken != "access" {
		t.Fatal("tokens not saved")
	}
	firstPublicKey := store.s.DeviceKey.PublicKey
	if firstPublicKey == "" {
		t.Fatal("successful login did not create a device key")
	}
	if err := login(); err != nil {
		t.Fatal(err)
	}
	if err := <-completed; err != nil {
		t.Fatal(err)
	}
	if store.s.DeviceKey.PublicKey == "" || store.s.DeviceKey.PublicKey == firstPublicKey {
		t.Fatal("new successful consent retained old login key")
	}
	previousRevocations := revoked.Load()

	status, e := o.Status(ctx)
	if e != nil {
		t.Fatal(e)
	}
	safe, _ := json.Marshal(status)
	if strings.Contains(string(safe), "refreshToken") || strings.Contains(string(safe), "privateKey") {
		t.Fatal("status leaked secrets")
	}
	if e = o.Logout(ctx); e != nil {
		t.Fatal(e)
	}
	if store.s != nil || revoked.Load() != previousRevocations+1 {
		t.Fatal("logout did not revoke and remove")
	}
}

func TestCatalogAndRemoteReadIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	remoteServer := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1"}, &mcp.ServerOptions{SupportedProtocolVersions: []string{"2025-11-25"}})
	remoteServer.AddTool(&mcp.Tool{Name: "reah_list_entities", InputSchema: document{"type": "object", "properties": document{}}, Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}}, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return &mcp.CallToolResult{Content: []mcp.Content{}, StructuredContent: document{"entityId": testID}}, nil
	})
	remoteServer.AddTool(&mcp.Tool{Name: "reah_signing_context", InputSchema: document{"type": "object"}, Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}}, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		t.Error("internal helper exposed")
		return nil, nil
	})
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return remoteServer }, &mcp.StreamableHTTPOptions{Stateless: true})
	var received atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer current-token" {
			http.Error(w, "unauthorized", 401)
			return
		}
		received.Add(1)
		handler.ServeHTTP(w, r)
	}))
	defer server.Close()
	c, _ := Configure(server.URL+"/mcp", "test")
	o := &OAuth{c, &memoryStore{s: &Session{Version: 1, Server: c.Server, ClientID: "client", DeviceKey: testKey(t), Tokens: &Tokens{"current-token", "refresh", time.Now().Add(time.Hour).UnixMilli()}}}, server.Client()}
	remote := &Remote{OAuth: o, Version: "test"}
	defer remote.Close()
	s := NewServer(remote, "test")
	st, ct := mcp.NewInMemoryTransports()
	ss, e := s.Connect(ctx, st, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer ss.Close()
	host := mcp.NewClient(&mcp.Implementation{Name: "host", Version: "test"}, &mcp.ClientOptions{MultiRoundTrip: &mcp.MultiRoundTripOptions{Disabled: true}})
	cs, e := host.Connect(ctx, ct, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer cs.Close()
	list, e := cs.ListTools(ctx, nil)
	if e != nil {
		t.Fatal(e)
	}
	if len(list.Tools) != 2 || list.Tools[0].Name != connectionStatusToolName || list.Tools[1].Name != "reah_list_entities" {
		t.Fatal("incorrect filtered catalog")
	}
	result, e := cs.CallTool(ctx, &mcp.CallToolParams{Name: "reah_list_entities", Arguments: document{}})
	if e != nil || result.IsError {
		t.Fatalf("read call failed: %v", e)
	}
	if received.Load() < 3 {
		t.Fatal("remote HTTP was not exercised")
	}
	result, e = cs.CallTool(ctx, &mcp.CallToolParams{Name: "reah_signing_context", Arguments: document{}})
	if e != nil || !result.IsError {
		t.Fatal("internal helper was callable")
	}
	result, e = cs.CallTool(ctx, &mcp.CallToolParams{Name: "reah_list_entities", Arguments: document{"_proof": "injected"}})
	if e != nil || !result.IsError {
		t.Fatal("model signing material was accepted")
	}
}

func TestCatalogRejectsUnknownSigning(t *testing.T) {
	tools := []*mcp.Tool{}
	for _, format := range []string{preparedApprovalProtocol, "turnkey-execution-v1", "future-format", "approval-send-v2"} {
		tools = append(tools, &mcp.Tool{Name: format, InputSchema: document{"type": "object", "properties": document{"entityId": document{"type": "string"}, "_proof": document{}, "requestSignature": document{}, "signatures": document{}}, "required": []string{"entityId", "_proof", "requestSignature"}}, Meta: mcp.Meta{localMeta: document{"version": 1, "mode": "signed", "signing": format, "inputSchema": document{"type": "object", "additionalProperties": false, "properties": document{"entityId": document{"type": "string"}}}}}, Annotations: &mcp.ToolAnnotations{ReadOnlyHint: false}})
	}
	out, e := publicTools(tools)
	if e != nil {
		t.Fatal(e)
	}
	if len(out) != 2 {
		t.Fatal("unknown signing protocol exposed")
	}
	b, _ := json.Marshal(out[0].InputSchema)
	if strings.Contains(string(b), "_proof") || strings.Contains(string(b), "requestSignature") {
		t.Fatal("private signing inputs exposed")
	}
	if reflect.DeepEqual(out[0].InputSchema, tools[0].InputSchema) {
		t.Fatal("original schema modified")
	}
	if _, e = publicTools(append(tools, tools[0])); e == nil {
		t.Fatal("duplicate tool names accepted")
	}
}
