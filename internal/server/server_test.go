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
	"github.com/vektah/gqlparser/v2/ast"
	"github.com/vektah/gqlparser/v2/parser"
)

func fixture(t *testing.T) (*mcp.Server, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	u := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var payload struct {
			Variables map[string]any `json:"variables"`
			Query     string         `json:"query"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		if payload.Variables["entityId"] != "entity-a" {
			t.Error("scope missing")
		}
		fixtures := map[string]string{
			"instrumentBalances":       `{"entity":{"instrumentBalances":{"balances":[{"nativeAmount":"1.23456789","usdAmount":"12345.6789","aggregation":{"__typename":"Instrument","id":"instrument-1","decimals":18,"asset":{"id":"asset-1","symbol":"ETH"}}}],"cursor":"next"}}}`,
			"wallets":                  `{"entity":{"wallets":[{"id":"wallet-1","totalBalance":{"usdAmount":"12345.6789"}}]}}`,
			"bankingAccounts":          `{"entity":{"bankingAccounts":[{"id":"bank-1","totalBalance":{"usdAmount":"12345.6789"}}]}}`,
			"transactions":             `{"entity":{"transactions":{"transactions":[{"id":"tx-1","state":"completed"}],"cursor":"next"}}}`,
			"analysisSettledMovements": `{"entity":{"analysisSettledMovements":{"movements":[{"signedAmountSmallest":"-1234567","instrumentDecimals":6,"assetSymbol":"USDC","usdValue":{"amountMinor":"-123","currency":"USD","scale":2},"role":"PRINCIPAL","valuationStatus":"EXACT"}],"nextPageToken":"next"}}}`,
			"contacts":                 `{"entity":{"contacts":{"contacts":[{"id":"contact-1"}],"cursor":"next"}}}`,
			"invoices":                 `{"entity":{"invoices":{"invoices":[{"id":"invoice-1","status":"SENT"}],"cursor":"next"}}}`,
			"approvalRecords":          `{"entity":{"approvalRecords":{"records":[{"summary":{"id":"approval-1","status":"APPROVED"}}],"cursor":"next"}}}`,
			"cardAccounts":             `{"entity":{"cardAccounts":[{"id":"cards-1","totalBalance":{"usdAmount":"10000"},"cards":{"cards":[{"id":"card-1","limitAmountMinor":"12345"}],"cursor":"next"}}]}}`,
			"cardTransactions":         `{"cardTransactions":{"transactions":[{"transactionId":"card-deposit","currency":"USDC","transactionType":"DEPOSIT"},{"transactionId":"card-withdraw","currency":"USDC","transactionType":"WITHDRAW"},{"transactionId":"card-spend","currency":"USD","transactionType":"SPEND"}],"cursor":"next"}}`,
			"treausryPositionAccounts": `{"entity":{"treausryPositionAccounts":[{"id":"position-1","currentValueInAssets":"1.23456789","currentValueInUSD":"12345.6789","investedInUSD":"10000","unrealizedProfitInUSD":"2345.6789","vault":{"id":"vault-1","apy":"0.04","underlyingInstrument":{"id":"instrument-1","decimals":18,"asset":{"id":"asset-1","symbol":"ETH"}}}}]}}`,
			"statementExports":         `{"statementExports":{"tasks":[{"id":"export-1","status":"COMPLETED","rowCount":100}],"cursor":"next"}}`,
			"userMemberships":          `{"entity":{"userMemberships":{"memberships":[{"user":{"id":"user-1","name":"Reader"}}],"cursor":"next"}}}`,
			"assets":                   `{"entity":{"assets":{"assets":[{"id":"asset-1","symbol":"ETH","defaultDecimals":18}]}}}`,
			"globalSearch":             `{"globalSearch":{"groups":[{"type":"TRANSACTIONS","items":[{"__typename":"Transaction","transactionId":"tx-1"}]}],"total":1}}`,
		}
		data := `{"entity":{"id":"entity-a","totalBalance":{"usdAmount":"12345.6789"}}}`
		doc, err := parser.ParseQuery(&ast.Source{Input: payload.Query})
		if err != nil {
			t.Error(err)
			return
		}
		root := doc.Operations[0].SelectionSet[0].(*ast.Field)
		field := root.Name
		if field == "entity" && len(root.SelectionSet) == 1 {
			field = root.SelectionSet[0].(*ast.Field).Name
		}
		if candidate, ok := fixtures[field]; ok {
			data = candidate
		}
		fmt.Fprintf(w, `{"data":%s}`, data)
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
			var response gateway.Response
			if err := json.Unmarshal([]byte(r.Content[0].(*mcp.TextContent).Text), &response); err != nil {
				t.Fatal(err)
			}
			if len(response.Data) == 0 || len(response.FieldSemantics) == 0 {
				t.Fatal("missing data or field semantics")
			}
			for _, field := range response.FieldSemantics {
				if field.Description == "" || field.Evidence == "" {
					t.Fatalf("undocumented field: %#v", field)
				}
			}
			for _, v := range response.NormalizedValues {
				if v.Status != "available" || v.Amount == nil {
					t.Fatalf("missing deterministic amount: %#v", v)
				}
			}
			if surface.Field == "cardTransactions" {
				var data struct {
					CardTransactions struct {
						Transactions []struct{ Currency string }
						Cursor       string
					}
				}
				if err := json.Unmarshal(response.Data, &data); err != nil {
					t.Fatal(err)
				}
				page := data.CardTransactions
				if len(page.Transactions) != 3 || page.Cursor != "next" || page.Transactions[0].Currency != "USDC" || page.Transactions[2].Currency != "USD" || len(response.NormalizedValues) != 0 {
					t.Fatalf("lost funding activity, spending metadata, or pagination: %s", response.Data)
				}
			}
			if surface.Field == "treausryPositionAccounts" {
				c := response.ReadCoverage["/entity/treausryPositionAccounts"]
				if c.Status != "complete" || c.SourceLimit != 200 || c.ReturnedItems == nil || *c.ReturnedItems != 1 {
					t.Fatalf("missing MCP coverage: %#v", response.ReadCoverage)
				}
			}
		})
	}
	before := calls.Load()
	for _, args := range []map[string]any{
		{"query": `mutation { _empty }`},
		{"query": `query($entityId:ID!){entity(id:$entityId){id}}`, "variables": map[string]any{"entityId": "other"}},
		{"query": `{cardTransactions(entityId:"entity-a"){transactions{amountMinor currency}}}`},
		{"query": `{entity(id:"entity-a"){treausryPositionAccounts{currentAPY}}}`},
		{"query": `{entity(id:"entity-a"){assets(first:1){assets{id}}}}`},
		{"query": `{entity(id:"entity-a"){assets(after:"next"){assets{id}}}}`},
	} {
		r, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "reah_query", Arguments: args})
		if err != nil {
			t.Fatal(err)
		}
		if !r.IsError {
			t.Fatal("unsafe query accepted over MCP")
		}
	}
	for _, args := range []map[string]any{
		{"arguments": map[string]any{"first": 1}},
		{"arguments": map[string]any{"after": "next"}},
	} {
		r, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "reah_list_assets", Arguments: args})
		if err != nil || !r.IsError {
			t.Fatalf("accepted nonfunctional asset pagination: %v %#v", err, r)
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
