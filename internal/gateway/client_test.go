package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestReadBoundaryBeforeNetwork(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		fmt.Fprint(w, `{"data":{"entity":{"id":"entity-a"}}}`)
	}))
	defer upstream.Close()
	c, err := New(upstream.URL, "test-token", "entity-a")
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, query string
		vars        map[string]any
	}{
		{"mutation", `mutation { _empty }`, nil},
		{"subscription", `subscription { entity(id:"entity-a") { id } }`, nil},
		{"multiple reads", `query A {entity(id:"entity-a"){id}} query B {entity(id:"entity-a"){id}}`, nil},
		{"mixed operations", `query {entity(id:"entity-a"){id}} mutation { _empty }`, nil},
		{"cross entity alias", `{ harmless:entity(id:"entity-b"){id} }`, nil},
		{"cross entity fragment", `query{...Scope} fragment Scope on Query{entity(id:"entity-b"){id}}`, nil},
		{"cross entity variable", `query($entityId:ID!){entity(id:$entityId){id}}`, map[string]any{"entityId": "entity-b"}},
		{"unbound variable", `query($id:ID!){entity(id:$id){id}}`, map[string]any{"id": "entity-b"}},
		{"nested identity escape", `{entity(id:"entity-a"){userMemberships{memberships{user{userGroupMemberships(entityID:"entity-b"){userGroup{id}}}}}}}`, nil},
		{"agent state", `{entity(id:"entity-a"){conversations{cursor}}}`, nil},
		{"card secrets", `{entity(id:"entity-a"){cardAccounts{cards{cards{cardSensitiveData(sessionId:"x"){data{pan}}}}}}}`, nil},
		{"introspection", `{__schema{queryType{name}}}`, nil},
		{"auth root", `{me{id}}`, nil},
		{"oversized page", `{entity(id:"entity-a"){transactions(first:201){cursor}}}`, nil},
		{"negative page", `{entity(id:"entity-a"){transactions(first:-1){cursor}}}`, nil},
		{"zero page variable", `query($entityId:ID!,$n:Int){entity(id:$entityId){transactions(first:$n){cursor}}}`, map[string]any{"n": 0}},
		{"fragment cycle", `query{...A} fragment A on Query{...A}`, nil},
		{"malformed", `query {`, nil},
		{"oversized source", strings.Repeat(" ", 64<<10) + `{entity(id:"entity-a"){id}}`, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := c.Query(context.Background(), tc.query, tc.vars); err == nil {
				t.Fatal("unsafe query accepted")
			}
		})
	}
	if calls.Load() != 0 {
		t.Fatalf("rejected queries made %d upstream requests", calls.Load())
	}
}

func TestScopedReadPreservesMoneyPaginationAndErrors(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.Header.Get("Authorization") != "Bearer test-token" {
			t.Error("missing upstream authentication")
		}
		var payload struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		if payload.Variables["entityId"] != "entity-a" {
			t.Error("entity not bound")
		}
		fmt.Fprint(w, `{"data":{"x":{"totalBalance":{"usdAmount":"9007199254740993.123456789"},"transactions":{"cursor":"next-page"}}},"errors":[{"message":"one field unavailable","path":["x","name"]}]}`)
	}))
	defer upstream.Close()
	c, err := New(upstream.URL, "test-token", "entity-a")
	if err != nil {
		t.Fatal(err)
	}
	query := `query Read($entityId:ID!, $n:Int) { ...Scope } fragment Scope on Query { x:entity(id:$entityId){name totalBalance{usdAmount} transactions(first:$n){cursor}} }`
	result, err := c.Query(context.Background(), query, map[string]any{"n": 100})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(result.Data), "9007199254740993.123456789") || !strings.Contains(string(result.Data), "next-page") || len(result.Errors) == 0 {
		t.Fatalf("lost raw facts: %#v", result)
	}
}

func TestResponseFailuresAndRedirects(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{
		{"unauthorized", 401, `sensitive backend error`}, {"malformed", 200, `not json`}, {"empty envelope", 200, `{}`}, {"oversized", 200, strings.Repeat("x", maxResponseBytes+1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			u := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.status); fmt.Fprint(w, tc.body) }))
			defer u.Close()
			c, err := New(u.URL, "token", "entity-a")
			if err != nil {
				t.Fatal(err)
			}
			_, err = c.Query(context.Background(), `{entity(id:"entity-a"){id}}`, nil)
			if err == nil || strings.Contains(err.Error(), "sensitive backend error") {
				t.Fatalf("unsafe response handling: %v", err)
			}
		})
	}
	var redirected atomic.Int32
	destination := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { redirected.Add(1) }))
	defer destination.Close()
	u := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL, http.StatusTemporaryRedirect)
	}))
	defer u.Close()
	c, err := New(u.URL, "token", "entity-a")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Query(context.Background(), `{entity(id:"entity-a"){id}}`, nil); err == nil {
		t.Fatal("followed redirect")
	}
	if redirected.Load() != 0 {
		t.Fatal("credential-bearing request followed redirect")
	}
}

func TestConfig(t *testing.T) {
	for _, endpoint := range []string{"http://api.example.com/graphql", "ftp://localhost/graphql", "https://user:pass@example.com/graphql", "https://example.com/graphql?token=secret", "https://example.com/graphql#fragment", "/graphql"} {
		if _, err := New(endpoint, "token", "e"); err == nil {
			t.Errorf("accepted %s", endpoint)
		}
	}
	if _, err := New("https://api.example.com/graphql", "", "e"); err == nil {
		t.Fatal("accepted empty credential")
	}
}

func TestGraphQLErrorsCannotEstablishTreasuryCompleteness(t *testing.T) {
	u := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"data":{"entity":{"positions":[{"id":"position-1"}]}},"errors":[{"message":"partial upstream data","path":["entity","positions",0,"id"]}]}`)
	}))
	defer u.Close()
	c, err := New(u.URL, "token", "entity-a")
	if err != nil {
		t.Fatal(err)
	}
	result, err := c.Query(context.Background(), `{entity(id:"entity-a"){positions:treausryPositionAccounts{id}}}`, nil)
	if err != nil {
		t.Fatal(err)
	}
	coverage := result.ReadCoverage["/entity/positions"]
	if coverage.Status != "not_proven" || coverage.SourceLimit != 200 || coverage.ReturnedItems == nil || *coverage.ReturnedItems != 1 || len(result.Errors) == 0 {
		t.Fatalf("partial result claimed complete coverage: %#v", result)
	}
}
