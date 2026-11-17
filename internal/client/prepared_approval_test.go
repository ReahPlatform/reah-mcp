package client

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func preparedFixture(t *testing.T, c SigningContext, args document, now time.Time) document {
	t.Helper()
	expires, _ := time.Parse(time.RFC3339Nano, c.Binding.ExpiresAt)
	input := cloneDocument(args)
	delete(input, "preparationContext")
	return document{"protocol": preparedApprovalProtocol, "resource": c.Resource, "connectionId": c.ConnectionID, "userId": c.UserID, "entityId": c.Binding.EntityID, "publicKey": c.Binding.PublicKey, "policyProfile": c.Binding.PolicyProfile, "tool": "new-business-tool", "requestId": testOtherID, "issuedAt": now.UTC().Format(time.RFC3339Nano), "expiresAt": expires.UTC().Format(time.RFC3339Nano), "arguments": args, "input": input, "payloadBytes": base64.StdEncoding.EncodeToString([]byte("opaque Gateway approval payload"))}
}

func TestPreparedApprovalRejectsAlteredBindings(t *testing.T) {
	key := testKey(t)
	c := testContext(key, DefaultServer)
	now := time.Now()
	args := document{"entityId": testID, "amount": "12.34", "preparationContext": document{"requestId": testOtherID, "futureBusinessField": "server-owned"}}
	for name, change := range map[string]func(document){
		"protocol":            func(d document) { d["protocol"] = "future" },
		"connection":          func(d document) { d["connectionId"] = testID },
		"resource":            func(d document) { d["resource"] = "https://other.example/mcp" },
		"key":                 func(d document) { d["publicKey"] = "another-key" },
		"entity":              func(d document) { d["entityId"] = testOtherID },
		"user":                func(d document) { d["userId"] = testOtherID },
		"profile":             func(d document) { d["policyProfile"] = "upgraded" },
		"tool":                func(d document) { d["tool"] = "other-tool" },
		"nonce":               func(d document) { d["requestId"] = testID },
		"arguments":           func(d document) { d["arguments"] = document{"entityId": testID, "amount": "999"} },
		"input":               func(d document) { d["input"] = document{"entityId": testID, "amount": "999"} },
		"old response":        func(d document) { d["issuedAt"] = now.Add(-2 * time.Minute).Format(time.RFC3339Nano) },
		"future response":     func(d document) { d["issuedAt"] = now.Add(time.Minute).Format(time.RFC3339Nano) },
		"expired":             func(d document) { d["expiresAt"] = now.Add(-time.Minute).Format(time.RFC3339Nano) },
		"extended":            func(d document) { d["expiresAt"] = now.Add(2 * time.Hour).Format(time.RFC3339Nano) },
		"noncanonical base64": func(d document) { d["payloadBytes"] = stringAt(d, "payloadBytes") + "\n" },
	} {
		t.Run(name, func(t *testing.T) {
			d := preparedFixture(t, c, args, now)
			change(d)
			if _, _, err := preparedApproval(key, c, "new-business-tool", args, d, now); err == nil {
				t.Fatal("signed altered preparation")
			}
		})
	}
	d := preparedFixture(t, c, args, now)
	signature, input, err := preparedApproval(key, c, "new-business-tool", args, d, now)
	if err != nil || !sameJSON(input, document{"entityId": testID, "amount": "12.34"}) {
		t.Fatalf("valid prepared approval: %v", err)
	}
	verifyText(t, key, stringAt(signature, "payloadBytes"), stringAt(signature, "signature"), false)
	// Client source contains no knowledge of this new business's fields or details.
	if args["preparationContext"] == nil {
		t.Fatal("mutated original arguments")
	}
}

func TestPreparedApprovalAllowsDescriptiveAnnotationsWithoutSigningThem(t *testing.T) {
	key := testKey(t)
	c := testContext(key, DefaultServer)
	now := time.Now()
	args := document{"entityId": testID, "requestId": testOtherID, "futureBusiness": "original terms"}
	for _, annotation := range []any{
		nil, false, "server guidance", []any{"future", true},
		document{"stage": "prepared_intent_only", "approvalCreated": false, "providerStateMayHaveChanged": true},
	} {
		d := preparedFixture(t, c, args, now)
		d["financialState"] = annotation
		d["futureAnnotation"] = document{"next": "server-owned guidance"}
		d["method"] = "signAnything"
		d["requestSignature"] = document{"payloadBytes": "untrusted annotation"}
		signature, input, err := preparedApproval(key, c, "new-business-tool", args, d, now)
		if err != nil || !sameJSON(input, args) {
			t.Fatalf("annotations changed preparation: %v", err)
		}
		if stringAt(signature, "payloadBytes") != stringAt(d, "payloadBytes") {
			t.Fatal("annotation changed the signed payload")
		}
		verifyText(t, key, stringAt(signature, "payloadBytes"), stringAt(signature, "signature"), false)
		for _, field := range []string{"financialState", "futureAnnotation", "method", "requestSignature"} {
			if _, exists := input[field]; exists {
				t.Fatalf("outer annotation entered submission input: %s", field)
			}
		}
	}
}

func TestPreparedApprovalSignsOpaqueGatewayPayload(t *testing.T) {
	key := testKey(t)
	c := testContext(key, DefaultServer)
	now := time.Now()
	args := document{"entityId": testID, "requestId": testOtherID}
	for _, payload := range [][]byte{
		[]byte("new server-owned approval format"),
		{0xff, 0x00, 0x01, 0x80},
	} {
		d := preparedFixture(t, c, args, now)
		d["payloadBytes"] = base64.StdEncoding.EncodeToString(payload)
		signature, input, err := preparedApproval(key, c, "new-business-tool", args, d, now)
		if err != nil || !sameJSON(input, args) {
			t.Fatalf("client interpreted an opaque approval payload: %v", err)
		}
		if stringAt(signature, "payloadBytes") != d["payloadBytes"] {
			t.Fatal("changed the server's opaque payload")
		}
		verifyText(t, key, stringAt(signature, "payloadBytes"), stringAt(signature, "signature"), false)
	}
}

func TestPreparedApprovalStillRequiresProtectedFieldsAndBoundedResponse(t *testing.T) {
	key := testKey(t)
	c := testContext(key, DefaultServer)
	now := time.Now()
	args := document{"entityId": testID, "requestId": testOtherID}
	for _, field := range []string{
		"protocol", "resource", "connectionId", "userId", "entityId", "publicKey", "policyProfile",
		"tool", "requestId", "issuedAt", "expiresAt", "arguments", "input", "payloadBytes",
	} {
		t.Run(field, func(t *testing.T) {
			d := preparedFixture(t, c, args, now)
			delete(d, field)
			d["annotation"] = "descriptive fields cannot replace a protected field"
			if _, _, err := preparedApproval(key, c, "new-business-tool", args, d, now); err == nil {
				t.Fatal("signed preparation with a missing protected field")
			}
		})
	}
	d := preparedFixture(t, c, args, now)
	d["annotation"] = strings.Repeat("x", 192*1024)
	if _, _, err := preparedApproval(key, c, "new-business-tool", args, d, now); err == nil {
		t.Fatal("signed an oversized preparation response")
	}
}

func TestPreparedCallBindsBothStagesAndNeverRetries(t *testing.T) {
	for _, unknown := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "ambiguous"}[unknown], func(t *testing.T) {
			key := testKey(t)
			c := testContext(key, DefaultServer)
			args := document{"entityId": testID, "requestId": testOtherID, "futureBusiness": "value"}
			var prepared, submitted int
			call := func(_ context.Context, name string, input document) (*mcp.CallToolResult, error) {
				if name == "reah_signing_context" {
					return &mcp.CallToolResult{StructuredContent: c}, nil
				}
				proof := input["_proof"].(document)
				verifyText(t, key, stringAt(proof, "payload"), stringAt(proof, "signature"), false)
				var bound document
				if json.Unmarshal([]byte(stringAt(proof, "payload")), &bound) != nil {
					t.Fatal("invalid proof")
				}
				if bound["tool"] != "new-business-tool" {
					t.Fatal("wrong operation binding")
				}
				if name == "reah_prepare_approval_signature" {
					prepared++
					if input["tool"] != "new-business-tool" || !sameJSON(bound["arguments"], args) || !sameJSON(input["arguments"], args) {
						t.Fatal("unbound preparation")
					}
					value := preparedFixture(t, c, args, time.Now())
					value["financialState"] = document{"stage": "prepared_intent_only", "approvalCreated": false}
					value["futureAnnotation"] = "descriptive only"
					return &mcp.CallToolResult{StructuredContent: value}, nil
				}
				submitted++
				if name != "new-business-tool" {
					t.Fatal("unexpected tool")
				}
				for _, field := range []string{"financialState", "futureAnnotation"} {
					if _, exists := input[field]; exists {
						t.Fatalf("outer annotation entered final operation proof: %s", field)
					}
				}
				sig := input["requestSignature"].(document)
				verifyText(t, key, stringAt(sig, "payloadBytes"), stringAt(sig, "signature"), false)
				body := cloneDocument(input)
				delete(body, "_proof")
				if !sameJSON(bound["arguments"], body) {
					t.Fatal("submission proof omitted signed terms")
				}
				if unknown {
					return nil, errors.New("lost response")
				}
				return &mcp.CallToolResult{StructuredContent: document{"approvalId": testOtherID}}, nil
			}
			_, err := callSigned(t.Context(), "new-business-tool", preparedApprovalProtocol, args, key, DefaultServer, call, "")
			if prepared != 1 || submitted != 1 || (unknown && (err == nil || !strings.Contains(err.Error(), "unconfirmed"))) || (!unknown && err != nil) {
				t.Fatalf("dispatch prepared=%d submitted=%d: %v", prepared, submitted, err)
			}
		})
	}
}

func TestPreparedCatalogUsesServerSchemaWithoutBusinessAllowlist(t *testing.T) {
	schema := document{"type": "object", "additionalProperties": false, "properties": document{"futureBusinessField": document{"type": "string"}}}
	tool := &mcp.Tool{Name: "new-business-tool", InputSchema: document{"type": "object", "properties": document{"requestSignature": document{}}}, Annotations: &mcp.ToolAnnotations{}, Meta: mcp.Meta{localMeta: document{"version": 1, "mode": "signed", "signing": preparedApprovalProtocol, "inputSchema": schema, "description": " Server-owned guidance."}}}
	out, err := publicTools([]*mcp.Tool{tool})
	if err != nil || len(out) != 1 || !sameJSON(out[0].InputSchema, schema) || !strings.Contains(out[0].Description, "Server-owned") {
		t.Fatalf("catalog: %v", err)
	}
	schema["properties"].(document)["requestSignature"] = document{}
	out, err = publicTools([]*mcp.Tool{tool})
	if err != nil || len(out) != 0 {
		t.Fatal("private signing field exposed")
	}
}

func TestSigningRejectsMismatchedPrivateKey(t *testing.T) {
	key := testKey(t)
	key.PublicKey = testKey(t).PublicKey
	if _, err := signText(key, "data", false); err == nil {
		t.Fatal("accepted mismatched private key")
	}
}
