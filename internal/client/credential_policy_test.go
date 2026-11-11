package client

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

func entityPolicyFixture() document {
	entities := []document{}
	for i := 1; i <= 87; i++ {
		entities = append(entities, document{"id": fmt.Sprintf("%08x-1111-4111-8111-111111111111", i), "name": fmt.Sprintf("Business %d", i)})
	}
	return document{"error": "credential_not_ready", "error_description": "secret-provider-diagnostics", "reason": signingEntityNotReady, "next_action": "review_business_selection", "retryable": false, "blocked_entities": entities}
}
func TestCredentialPolicyLargeRuntimeDiagnosticsAndSafeFallback(t *testing.T) {
	valid := entityPolicyFixture()
	raw, _ := json.Marshal(valid)
	if len(raw) <= 4096 {
		t.Fatal("fixture does not cover old body limit")
	}
	policy := parseCredentialPolicy(raw, 409, "/mcp/credentials/status")
	if policy == nil || len(policy.BlockedEntities) != 87 || !strings.Contains(policy.message(), "Business 87") || strings.Contains(policy.message(), "secret-provider") || strings.Contains(policy.message(), "login --enroll-key") {
		t.Fatal("lost safe runtime diagnostics")
	}
	for _, mutate := range []func(document){
		func(d document) { d["reason"] = "unknown" },
		func(d document) { d["next_action"] = "enroll_key" },
		func(d document) { delete(d, "retryable") },
		func(d document) { d["retryable"] = true },
		func(d document) { d["blocked_entities"] = []document{} },
		func(d document) {
			d["blocked_entities"] = []document{{"id": "11111111-1111-4111-8111-111111111111", "name": "bad\nline"}}
		},
		func(d document) {
			d["blocked_entities"] = []document{{"id": "11111111-1111-4111-8111-111111111111", "name": "bad\u202ename"}}
		},
		func(d document) {
			d["blocked_entities"] = []document{{"id": "11111111-1111-4111-8111-111111111111", "name": strings.Repeat("x", 1025)}}
		},
		func(d document) { d["blocked_entities"] = []document{{"id": "not-an-id", "name": "name"}} },
		func(d document) {
			e := document{"id": "11111111-1111-4111-8111-111111111111", "name": "name"}
			d["blocked_entities"] = []document{e, e}
		},
	} {
		d := entityPolicyFixture()
		mutate(d)
		b, _ := json.Marshal(d)
		if parseCredentialPolicy(b, 409, "/mcp/credentials/prepare") != nil {
			t.Fatal("accepted unsafe metadata")
		}
	}
	for _, bad := range []string{string(raw) + `{}`, `{"reason":"ENTITY_SIGNING_NOT_READY","reason":"unknown"}`, strings.Repeat("x", credentialPolicyBodyLimit+1), string([]byte{0xff})} {
		if parseCredentialPolicy([]byte(bad), 409, "/mcp/credentials/status") != nil {
			t.Fatal("accepted invalid policy body")
		}
	}
	for _, path := range []string{"/mcp/oauth/register", "/mcp/oauth/token", "/mcp/credentials"} {
		if parseCredentialPolicy(raw, 409, path) != nil {
			t.Fatal("accepted policy on unrelated endpoint")
		}
	}
	if parseCredentialPolicy(raw, 500, "/mcp/credentials/status") != nil {
		t.Fatal("accepted policy with wrong status")
	}
	err := oauthHTTPError(&http.Response{StatusCode: 409, Body: io.NopCloser(strings.NewReader(string(raw)))}, "/mcp/credentials/status")
	if strings.Contains(err.Error(), "secret-provider") || !strings.Contains(err.Error(), "Business 87") {
		t.Fatal("unsafe policy display")
	}
	c, _ := Configure(DefaultServer, "saved-profile")
	o := &OAuth{Config: c}
	out := document{"policyProfile": "financial-operations-v1", "credentialEnrollmentRequested": true}
	if !o.applyCredentialPolicy(out, err) || out["phase"] != "read_only_ready" || out["retryable"] != false {
		t.Fatal("legacy entity issue globally blocked read access")
	}
	d := document{"error": "financial_authorization_too_long", "reason": financialAuthorizationTooLong, "next_action": "reauthorize", "retryable": false, "max_authorization_seconds": 86400, "error_description": "secret-provider"}
	b, _ := json.Marshal(d)
	err = oauthHTTPError(&http.Response{StatusCode: 409, Body: io.NopCloser(strings.NewReader(string(b)))}, "/mcp/credentials/prove")
	if !o.applyCredentialPolicy(out, err) || out["reason"] != financialAuthorizationTooLong {
		t.Fatal("missing duration recovery")
	}
	args := out["nextAction"].(document)["args"].([]string)
	if !reflect.DeepEqual(args, []string{"login", "--enroll-key", "--policy-profile", "financial-operations-v1", "--no-browser", "--json", "--server", DefaultServer, "--profile", "saved-profile"}) {
		t.Fatal("duration recovery lost original signing profile", args)
	}
	d["max_authorization_seconds"] = 86401
	b, _ = json.Marshal(d)
	if parseCredentialPolicy(b, 409, "/mcp/credentials/status") != nil {
		t.Fatal("accepted unsupported duration")
	}
	unknown := oauthHTTPError(&http.Response{StatusCode: 409, Body: io.NopCloser(strings.NewReader(`{"error":"credential_not_ready","error_description":"secret-provider"}`))}, "/mcp/credentials/status")
	if !o.applyCredentialPolicy(out, unknown) || out["nextAction"].(document)["action"] != "inspect_connection" || strings.Contains(unknown.Error(), "secret-provider") {
		t.Fatal("unsafe generic recovery")
	}
}

func TestMissingEnrollmentPolicyRequiresExactStatusContract(t *testing.T) {
	d := document{"error": "credential_not_ready", "reason": credentialEnrollmentNotFound, "next_action": "enroll_key", "retryable": false, "error_description": "secret-provider"}
	raw, _ := json.Marshal(d)
	p := parseCredentialPolicy(raw, http.StatusConflict, "/mcp/credentials/status")
	if p == nil || p.Reason != credentialEnrollmentNotFound || strings.Contains(p.message(), "secret-provider") {
		t.Fatal("missing safe enrollment recovery")
	}
	for _, path := range []string{"/mcp/credentials/prepare", "/mcp/credentials/prove", "/mcp/oauth/token"} {
		if parseCredentialPolicy(raw, http.StatusConflict, path) != nil {
			t.Fatal("missing status contract accepted on other path")
		}
	}
	for _, mutate := range []func(document){
		func(d document) { d["reason"] = "CREDENTIAL_NOT_READY" },
		func(d document) { d["next_action"] = "inspect_connection" },
		func(d document) { d["retryable"] = true },
		func(d document) { delete(d, "retryable") },
		func(d document) { d["error"] = "invalid_grant" },
	} {
		candidate := document{}
		for k, v := range d {
			candidate[k] = v
		}
		mutate(candidate)
		b, _ := json.Marshal(candidate)
		if parseCredentialPolicy(b, http.StatusConflict, "/mcp/credentials/status") != nil {
			t.Fatal("generic/malformed status incorrectly offered enrollment")
		}
	}
}
