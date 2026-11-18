package client

import (
	"context"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestBankTransferExecutionRequiresAdvertisedBusiness(t *testing.T) {
	for _, advertised := range []bool{false, true} {
		t.Run(strconv.FormatBool(advertised), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			var contextCalls, executionCalls atomic.Int32
			remoteServer := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1"}, &mcp.ServerOptions{SupportedProtocolVersions: []string{"2025-11-25"}})
			businesses := []string{"crypto_convert"}
			if advertised {
				businesses = append(businesses, "bank_internal_transfer")
			}
			remoteServer.AddTool(&mcp.Tool{Name: "renamed-financial-execution", Meta: mcp.Meta{localMeta: document{"version": 1, "mode": "signed", "signing": "turnkey-execution-v1"}}, InputSchema: document{"type": "object", "properties": document{"entityId": document{"type": "string"}, "approvalId": document{"type": "string"}, "business": document{"type": "string", "enum": businesses}, "_proof": document{"type": "object"}}, "required": []string{"entityId", "approvalId", "business", "_proof"}}, Annotations: &mcp.ToolAnnotations{ReadOnlyHint: false}}, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				executionCalls.Add(1)
				t.Error("catalog compatibility test reached financial execution")
				return nil, nil
			})
			remoteServer.AddTool(&mcp.Tool{Name: "reah_signing_context", InputSchema: document{"type": "object"}, Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}}, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				contextCalls.Add(1)
				return errorResult("signing context intentionally closed"), nil
			})
			handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return remoteServer }, &mcp.StreamableHTTPOptions{Stateless: true})
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer current-token" {
					http.Error(w, "unauthorized", http.StatusUnauthorized)
					return
				}
				handler.ServeHTTP(w, r)
			}))
			defer server.Close()
			config, _ := Configure(server.URL+"/mcp", "test")
			oauth := &OAuth{config, &memoryStore{s: &Session{AuthorizationGeneration: "fixture-login", Version: 1, Server: config.Server, ClientID: "client", CredentialEnrollmentRequested: true, Enrollment: &Receipt{State: "awaiting_provider"}, DeviceKey: testKey(t), Tokens: &Tokens{"current-token", "refresh", time.Now().Add(time.Hour).UnixMilli()}}}, server.Client()}
			remote := &Remote{OAuth: oauth, Version: "test"}
			defer remote.Close()
			result, err := remote.Call(ctx, "renamed-financial-execution", document{"entityId": testID, "approvalId": testOtherID, "business": "bank_internal_transfer"})
			if advertised {
				if err != nil || result == nil || !result.IsError || contextCalls.Load() != 1 {
					t.Fatalf("advertised business did not reach context: result=%v err=%v calls=%d", result, err, contextCalls.Load())
				}
			} else if err == nil || !strings.Contains(err.Error(), "authenticated Gateway catalog") || contextCalls.Load() != 0 {
				t.Fatalf("older catalog admitted bank execution: err=%v context calls=%d", err, contextCalls.Load())
			}
			if executionCalls.Load() != 0 {
				t.Fatal("financial execution was dispatched")
			}
		})
	}
}

func TestBankTransferExecutionSignsExactAuthenticatedRequest(t *testing.T) {
	key, provider := testKey(t), testKey(t)
	c := testContext(key, DefaultServer)
	c.Binding.PolicyProfile = "financial-operations-v1"
	providerKey, _ := privateKey(provider)
	der, _ := x509.MarshalPKIXPublicKey(&providerKey.PublicKey)
	trusted := string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
	payload, _ := json.Marshal(document{"organizationId": testOtherID, "type": "ACTIVITY_TYPE_SIGN_RAW_PAYLOADS", "timestampMs": strconv.FormatInt(time.Now().UnixMilli(), 10), "parameters": document{"signWith": "0x1111111111111111111111111111111111111111", "payloads": []string{strings.Repeat("ab", 32)}, "encoding": "PAYLOAD_ENCODING_HEXADECIMAL", "hashFunction": "HASH_FUNCTION_NO_OP"}})
	providerSignature, err := signText(provider, string(payload), true)
	if err != nil {
		t.Fatal(err)
	}
	providerBytes, _ := hex.DecodeString(providerSignature)
	for _, scenario := range []string{"complete", "tampered body", "tampered provider signature", "unknown continuation"} {
		t.Run(scenario, func(t *testing.T) {
			calls := 0
			item := document{"requestId": "bank-pending-original", "requestPayload": string(payload), "signature": base64.StdEncoding.EncodeToString(providerBytes)}
			if scenario == "tampered body" {
				item["requestPayload"] = string(payload) + " "
			}
			if scenario == "tampered provider signature" {
				item["signature"] = base64.StdEncoding.EncodeToString([]byte("invalid"))
			}
			call := func(_ context.Context, name string, input document) (*mcp.CallToolResult, error) {
				if name == "reah_signing_context" {
					return &mcp.CallToolResult{StructuredContent: c}, nil
				}
				calls++
				if name != "renamed-bank-execution" || input["entityId"] != testID || input["approvalId"] != testOtherID || input["business"] != "bank_internal_transfer" {
					t.Fatal("original operation identity changed")
				}
				proof := input["_proof"].(document)
				verifyText(t, key, stringAt(proof, "payload"), stringAt(proof, "signature"), false)
				var decoded document
				if json.Unmarshal([]byte(stringAt(proof, "payload")), &decoded) != nil {
					t.Fatal("invalid operation proof")
				}
				bound, _ := json.Marshal(decoded["arguments"])
				unsigned := cloneDocument(input)
				delete(unsigned, "_proof")
				expected, _ := json.Marshal(unsigned)
				if string(bound) != string(expected) {
					t.Fatal("proof does not bind exact continuation arguments")
				}
				if calls == 1 {
					return &mcp.CallToolResult{StructuredContent: document{"requiresSignatures": []any{item}}}, nil
				}
				signatures, ok := input["signatures"].([]any)
				if !ok || len(signatures) != 1 {
					t.Fatal("expected one bank signing continuation")
				}
				stamp := signatures[0].(document)
				if stamp["requestId"] != item["requestId"] {
					t.Fatal("pending request identity changed")
				}
				stampBytes, e := base64.RawURLEncoding.DecodeString(stringAt(stamp, "signature"))
				var signed document
				if e != nil || json.Unmarshal(stampBytes, &signed) != nil || signed["publicKey"] != key.PublicKey || signed["scheme"] != "SIGNATURE_SCHEME_TK_API_P256" {
					t.Fatal("invalid exact-body stamp")
				}
				verifyText(t, key, string(payload), stringAt(signed, "signature"), true)
				if scenario == "unknown continuation" {
					return nil, errors.New("response lost")
				}
				return &mcp.CallToolResult{StructuredContent: document{"success": true, "requiresSignatures": []any{}}}, nil
			}
			_, err := callSigned(context.Background(), "renamed-bank-execution", "turnkey-execution-v1", document{"entityId": testID, "approvalId": testOtherID, "business": "bank_internal_transfer"}, key, DefaultServer, call, trusted)
			switch scenario {
			case "complete":
				if err != nil || calls != 2 {
					t.Fatalf("bank continuation failed: err=%v calls=%d", err, calls)
				}
			case "unknown continuation":
				if err == nil || !strings.Contains(err.Error(), "unconfirmed") || calls != 2 {
					t.Fatalf("unknown outcome retried: err=%v calls=%d", err, calls)
				}
			default:
				if err == nil || !strings.Contains(err.Error(), "untrusted") || calls != 1 {
					t.Fatalf("tampered request signed: err=%v calls=%d", err, calls)
				}
			}
		})
	}
}
