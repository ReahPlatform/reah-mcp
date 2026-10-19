package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ReahPlatform/reah-mcp/internal/gateway"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func fixture(t *testing.T) (*mcp.Server, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	u := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var payload struct {
			Variables map[string]any `json:"variables"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		if payload.Variables["entityId"] != "entity-a" {
			t.Error("scope missing")
		}
		fmt.Fprint(w, `{"data":{"read_result":{"amount":"123.456789","cursor":"next"}}}`)
	}))
	t.Cleanup(u.Close)
	c, err := gateway.New(u.URL, "upstream-test-token", "entity-a")
	if err != nil {
		t.Fatal(err)
	}
	s, err := New(c)
	if err != nil {
		t.Fatal(err)
	}
	return s, &calls
}

func TestMCPToolsEndToEnd(t *testing.T) {
	s, calls := fixture(t)
	ctx := context.Background()
	a, b := mcp.NewInMemoryTransports()
	ss, err := s.Connect(ctx, a, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	cs, err := client.Connect(ctx, b, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	listed, err := cs.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		t.Fatal(err)
	}
	if len(listed.Tools) != len(Surfaces)+3 {
		t.Fatalf("wrong tool count: %d", len(listed.Tools))
	}
	for _, tool := range listed.Tools {
		if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
			t.Errorf("missing read-only annotation: %s", tool.Name)
		}
	}
	for _, name := range []string{"reah_catalog", "reah_describe_type"} {
		args := map[string]any{}
		if name == "reah_describe_type" {
			args["type"] = "Query"
		}
		r, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil || r.IsError {
			t.Fatalf("%s: %v %#v", name, err, r)
		}
	}
	if calls.Load() != 0 {
		t.Fatal("discovery contacted upstream")
	}
	for _, surface := range Surfaces {
		t.Run(surface.Name, func(t *testing.T) {
			args := map[string]any{}
			if surface.Field == "analysisSettledMovements" {
				args["arguments"] = map[string]any{"settledFrom": "2026-10-01T00:00:00Z", "settledTo": "2026-11-01T00:00:00Z"}
			}
			if surface.Field == "globalSearch" {
				args["arguments"] = map[string]any{"query": "test", "limit": 100}
			}
			r, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: surface.Name, Arguments: args})
			if err != nil || r.IsError {
				t.Fatalf("read: %v %#v content=%v", err, r, r.Content)
			}
			if !strings.Contains(r.Content[0].(*mcp.TextContent).Text, "123.456789") {
				t.Fatal("lost raw amount")
			}
		})
	}
	before := calls.Load()
	for _, args := range []map[string]any{
		{"query": `mutation { _empty }`},
		{"query": `query($entityId:ID!){entity(id:$entityId){id}}`, "variables": map[string]any{"entityId": "other"}},
	} {
		r, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "reah_query", Arguments: args})
		if err != nil {
			t.Fatal(err)
		}
		if !r.IsError {
			t.Fatal("unsafe query accepted over MCP")
		}
	}
	if calls.Load() != before {
		t.Fatal("unsafe MCP request reached upstream")
	}
}

func TestHTTPAuthAndMCPProtocol(t *testing.T) {
	s, calls := fixture(t)
	const token = "local-http-token-at-least-32-characters"
	h, err := HTTPHandler(s, token)
	if err != nil {
		t.Fatal(err)
	}
	u := httptest.NewServer(h)
	defer u.Close()
	for _, auth := range []string{"", "Bearer wrong", "Bearer upstream-test-token"} {
		req, _ := http.NewRequest("POST", u.URL+"/mcp", strings.NewReader(`{}`))
		req.Header.Set("Authorization", auth)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 401 {
			t.Fatalf("auth status %d", resp.StatusCode)
		}
	}
	transport := &mcp.StreamableClientTransport{Endpoint: u.URL + "/mcp", HTTPClient: &http.Client{Transport: authTransport{token: token}}}
	c := mcp.NewClient(&mcp.Implementation{Name: "http-test", Version: "1"}, nil)
	session, err := c.Connect(context.Background(), transport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	r, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "reah_get_entity", Arguments: map[string]any{}})
	if err != nil || r.IsError {
		t.Fatalf("HTTP MCP failed: %v %#v", err, r)
	}
	if calls.Load() != 1 {
		t.Fatalf("upstream calls = %d", calls.Load())
	}
	req, _ := http.NewRequest("POST", u.URL+"/mcp", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Origin", "https://untrusted.example")
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 403 {
		t.Fatalf("cross origin status %d", resp.StatusCode)
	}
	if _, err := HTTPHandler(s, "short"); err == nil {
		t.Fatal("weak HTTP token accepted")
	}
}

type authTransport struct{ token string }

func (a authTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+a.token)
	return http.DefaultTransport.RoundTrip(r)
}
