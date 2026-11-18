package client

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type catalogFixture struct {
	remote                 *Remote
	store                  *memoryStore
	lists, calls           atomic.Int32
	deny, fail, extra      atomic.Bool
	rotateOnDiscoveryStart atomic.Bool
	rotations              atomic.Int32
	churnOnList            atomic.Bool
}

func newCatalogFixture(t *testing.T) *catalogFixture {
	t.Helper()
	f := &catalogFixture{}
	sdk := mcp.NewServer(&mcp.Implementation{Name: "catalog-test", Version: "1"}, nil)
	for _, name := range []string{"reah_read", "reah_new", "reah_mutate"} {
		tool := &mcp.Tool{Name: name, InputSchema: document{"type": "object", "properties": document{}}, Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}}
		if name == "reah_mutate" {
			tool.Meta = mcp.Meta{localMeta: document{"version": 1, "mode": "proxy"}}
			tool.Annotations.ReadOnlyHint = false
		}
		sdk.AddTool(tool, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			f.calls.Add(1)
			return &mcp.CallToolResult{StructuredContent: document{"ok": true}, Content: []mcp.Content{}}, nil
		})
	}
	sdk.AddTool(&mcp.Tool{Name: "reah_catalog", InputSchema: document{"type": "object"}, Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}}, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		f.calls.Add(1)
		return &mcp.CallToolResult{Content: []mcp.Content{}, StructuredContent: document{"write_operations": []any{document{"requestTool": "reah_new"}}}}, nil
	})
	sdk.AddTool(&mcp.Tool{Name: "reah_future", InputSchema: document{"type": "object"}, Meta: mcp.Meta{localMeta: document{"version": 1, "mode": "signed", "signing": "unknown-protocol"}}, Annotations: &mcp.ToolAnnotations{}}, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		t.Error("unknown signing protocol was dispatched")
		return nil, nil
	})
	sdk.AddReceivingMiddleware(func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, request mcp.Request) (mcp.Result, error) {
			out, err := next(ctx, method, request)
			if list, ok := out.(*mcp.ListToolsResult); ok {
				f.lists.Add(1)
				if !f.extra.Load() {
					copy := *list
					copy.Tools = nil
					for _, v := range list.Tools {
						if v.Name != "reah_new" {
							copy.Tools = append(copy.Tools, v)
						}
					}
					out = &copy
				}
			}
			return out, err
		}
	})
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return sdk }, &mcp.StreamableHTTPOptions{Stateless: true})
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/mcp/oauth/token" {
			f.rotations.Add(1)
			_ = json.NewEncoder(w).Encode(document{"access_token": "rotated-access", "refresh_token": "rotated-refresh", "expires_in": 900, "scope": Scope, "token_type": "Bearer"})
			return
		}
		// Read the method without logging or consuming the SDK request.
		raw, _ := io.ReadAll(r.Body)
		r.Body = io.NopCloser(bytes.NewReader(raw))
		var req struct {
			Method string `json:"method"`
		}
		_ = json.Unmarshal(raw, &req)
		if req.Method == "tools/list" && f.churnOnList.Load() {
			if err := f.store.Exclusive(r.Context(), func() error { f.store.s.Tokens.AccessToken += "x"; return nil }); err != nil {
				t.Error(err)
			}
		}

		if (req.Method == "initialize" || req.Method == "server/discover") && f.rotateOnDiscoveryStart.CompareAndSwap(true, false) {
			if err := f.store.Exclusive(r.Context(), func() error { f.store.s.Tokens.ExpiresAt = time.Now().Add(-time.Second).UnixMilli(); return nil }); err != nil {
				t.Error(err)
			}
		}
		if f.deny.Load() {
			http.Error(w, "denied", http.StatusUnauthorized)
			return
		}
		if f.fail.Load() && req.Method == "tools/call" {
			f.calls.Add(1)
			http.Error(w, "unconfirmed", http.StatusServiceUnavailable)
			return
		}
		handler.ServeHTTP(w, r)
	}))
	config, _ := Configure(server.URL+"/mcp", "cache-test")
	f.store = &memoryStore{s: &Session{Version: 1, Server: config.Server, ClientID: "client", DeviceKey: testKey(t), Tokens: &Tokens{"access", "refresh", time.Now().Add(time.Hour).UnixMilli()}}}
	f.remote = &Remote{OAuth: &OAuth{config, f.store, server.Client()}, Version: "test"}
	t.Cleanup(func() { f.remote.Close(); server.Close() })
	return f
}

func TestRemoteCatalogReusesMetadataAndExplicitDiscoveryRefreshes(t *testing.T) {
	f := newCatalogFixture(t)
	ctx := context.Background()
	for range 3 {
		if _, err := f.remote.Call(ctx, "reah_read", document{}); err != nil {
			t.Fatal(err)
		}
	}
	if f.lists.Load() != 1 || f.calls.Load() != 3 {
		t.Fatalf("list/call counts %d/%d", f.lists.Load(), f.calls.Load())
	}
	tools, err := f.remote.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	tools[0].Name = "caller-modified"
	tools[0].InputSchema.(map[string]any)["type"] = "modified"
	copy, cached, err := f.remote.catalog(ctx, false)
	if err != nil || !cached || copy[0].Name == "caller-modified" || copy[0].InputSchema.(map[string]any)["type"] != "object" {
		t.Fatal("caller mutation entered the cached snapshot", err)
	}
	if _, err = f.remote.Call(ctx, "reah_read", document{}); err != nil {
		t.Fatal("caller changed cached metadata", err)
	}
	if f.lists.Load() != 2 {
		t.Fatal("explicit discovery did not refresh")
	}
	f.extra.Store(true)
	if _, err = f.remote.Call(ctx, "reah_new", document{}); err != nil {
		t.Fatal(err)
	}
	if f.lists.Load() != 3 || f.calls.Load() != 5 {
		t.Fatal("unknown tool was not discovered before its one dispatch")
	}
	if _, err = f.remote.Call(ctx, "missing", document{}); err == nil {
		t.Fatal("unknown tool dispatched")
	}
	if f.calls.Load() != 5 {
		t.Fatal("missing tool invoked")
	}
}

func TestRemoteCatalogCannotEnableUnknownSigningProtocol(t *testing.T) {
	f := newCatalogFixture(t)
	if _, err := f.remote.List(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := f.remote.Call(context.Background(), "reah_future", document{}); err == nil {
		t.Fatal("unknown protocol accepted")
	}
	if f.calls.Load() != 0 {
		t.Fatal("unknown protocol invoked")
	}
}

func TestRemoteCatalogLifecycleAndExpiry(t *testing.T) {
	f := newCatalogFixture(t)
	ctx := context.Background()
	call := func() {
		t.Helper()
		if _, err := f.remote.Call(ctx, "reah_read", document{}); err != nil {
			t.Fatal(err)
		}
	}
	call()
	changes := []func(*Session){
		func(s *Session) { s.Tokens.AccessToken = "replacement" },
		func(s *Session) { s.DeviceKey = testKey(t) },
		func(s *Session) { s.CredentialPolicyProfile = "financial-operations-v1" },
		func(s *Session) {
			s.Enrollment = &Receipt{ConnectionID: testID, State: "awaiting_provider", CredentialExpiresAt: "expiry"}
		},
		func(s *Session) { s.Enrollment.ConnectionID = testOtherID },
	}
	for i, change := range changes {
		if err := f.store.Exclusive(ctx, func() error { change(f.store.s); return nil }); err != nil {
			t.Fatal(err)
		}
		call()
		if f.lists.Load() != int32(i+2) {
			t.Fatal("reused another credential lifecycle")
		}
	}
	f.remote.catalogMu.Lock()
	f.remote.catalogSnapshot.expires = time.Now().Add(-time.Second)
	f.remote.catalogMu.Unlock()
	call()
	before := f.lists.Load()
	f.remote.Close()
	call()
	if f.lists.Load() != before+1 {
		t.Fatal("Close retained snapshot")
	}
	if err := f.store.Exclusive(ctx, f.store.Remove); err != nil {
		t.Fatal(err)
	}
	if _, err := f.remote.Call(ctx, "reah_read", document{}); err == nil {
		t.Fatal("logout reused metadata")
	}
	f.remote.catalogMu.Lock()
	cached := len(f.remote.catalogSnapshot.tools)
	f.remote.catalogMu.Unlock()
	if cached != 0 {
		t.Fatal("ended credentials retained metadata")
	}
}

func TestRemoteCachedCatalogDoesNotCacheAuthorityOrReplay(t *testing.T) {
	for _, denied := range []bool{true, false} {
		t.Run(map[bool]string{true: "revoked", false: "ambiguous mutation"}[denied], func(t *testing.T) {
			f := newCatalogFixture(t)
			ctx := context.Background()
			if _, err := f.remote.List(ctx); err != nil {
				t.Fatal(err)
			}
			if denied {
				f.deny.Store(true)
			} else {
				f.fail.Store(true)
			}
			if _, err := f.remote.Call(ctx, "reah_mutate", document{}); err == nil {
				t.Fatal("cached schema bypassed rejection")
			}
			if f.lists.Load() != 1 {
				t.Fatal("rejection triggered catalog retry")
			}
			want := int32(1)
			if denied {
				want = 0
			}
			if f.calls.Load() != want {
				t.Fatal("dispatched call was replayed")
			}
		})
	}
}

func TestRemoteCatalogConcurrentSnapshotsAreIsolated(t *testing.T) {
	f := newCatalogFixture(t)
	ctx := context.Background()
	if _, err := f.remote.List(ctx); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tools, cached, err := f.remote.catalog(ctx, false)
			if err != nil || !cached {
				t.Error("snapshot unavailable", err)
				return
			}
			tools[0].InputSchema.(map[string]any)["type"] = "caller-only"
		}()
	}
	wg.Wait()
	if _, err := f.remote.Call(ctx, "reah_read", document{}); err != nil {
		t.Fatal(err)
	}
	if f.lists.Load() != 1 {
		t.Fatal("snapshot was not reused")
	}
}

func TestRemoteCatalogAcceptsTransportRotationAfterPreflight(t *testing.T) {
	f := newCatalogFixture(t)
	f.rotateOnDiscoveryStart.Store(true)
	if _, err := f.remote.Call(context.Background(), "reah_read", document{}); err != nil {
		t.Fatal("successful transport rotation rejected discovery", err)
	}
	if f.rotations.Load() != 1 || f.lists.Load() != 2 || f.calls.Load() != 1 {
		t.Fatalf("rotation/list/dispatch counts %d/%d/%d", f.rotations.Load(), f.lists.Load(), f.calls.Load())
	}
	if _, err := f.remote.Call(context.Background(), "reah_read", document{}); err != nil {
		t.Fatal(err)
	}
	if f.lists.Load() != 2 {
		t.Fatal("fresh rotated catalog was not cached")
	}
}

func TestRemoteExplicitCatalogObservesProviderRegistrationWithoutVaultChange(t *testing.T) {
	f := newCatalogFixture(t)
	ctx := context.Background()
	if err := f.store.Exclusive(ctx, func() error {
		f.store.s.Enrollment = &Receipt{ConnectionID: testID, State: "awaiting_provider"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	before, err := f.remote.catalogIdentity(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	out, err := f.remote.Call(ctx, "reah_catalog", document{})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.StructuredContent.(document)["write_operations"].([]any)) != 0 {
		t.Fatal("hidden operation advertised")
	}
	f.extra.Store(true)
	out, err = f.remote.Call(ctx, "reah_catalog", document{})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.StructuredContent.(document)["write_operations"].([]any)) != 1 {
		t.Fatal("fresh capability discovery was filtered with stale schema")
	}
	after, err := f.remote.catalogIdentity(ctx, false)
	if err != nil || before != after {
		t.Fatal("fixture unexpectedly changed saved enrollment", err)
	}
	if f.lists.Load() != 2 || f.calls.Load() != 2 {
		t.Fatal("explicit catalog must refresh once and invoke once")
	}
}

func TestRemoteCatalogLifecycleChurnStopsBeforeDispatch(t *testing.T) {
	f := newCatalogFixture(t)
	f.churnOnList.Store(true)
	if _, err := f.remote.Call(context.Background(), "reah_mutate", document{}); err == nil {
		t.Fatal("unstable discovery accepted")
	}
	if f.lists.Load() != 2 || f.calls.Load() != 0 {
		t.Fatal("discovery retry was unbounded or reached dispatch")
	}
}
