package client

import (
	"context"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestExecutionMultipleVerifiedRounds(t *testing.T) {
	key, provider := testKey(t), testKey(t)
	c := testContext(key, DefaultServer)
	pub, _ := privateKey(provider)
	der, _ := x509.MarshalPKIXPublicKey(&pub.PublicKey)
	trusted := string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
	request := func(n int) document {
		body, _ := json.Marshal(document{"organizationId": testOtherID, "type": "ACTIVITY_TYPE_SIGN_RAW_PAYLOADS", "timestampMs": strconv.FormatInt(time.Now().UnixMilli(), 10), "parameters": document{"signWith": "wallet", "payloads": []string{fmt.Sprint(n)}}})
		sig, e := signText(provider, string(body), true)
		if e != nil {
			t.Fatal(e)
		}
		bytes, _ := hex.DecodeString(sig)
		return document{"requestId": fmt.Sprint(n), "requestPayload": string(body), "signature": base64.StdEncoding.EncodeToString(bytes)}
	}
	for _, scenario := range []string{"complete", "repeated", "limit", "unknown"} {
		t.Run(scenario, func(t *testing.T) {
			calls := 0
			call := func(_ context.Context, name string, input document) (*mcp.CallToolResult, error) {
				if name == "reah_signing_context" {
					return &mcp.CallToolResult{StructuredContent: c}, nil
				}
				calls++
				if input["entityId"] != testID || input["approvalId"] != testOtherID || input["business"] != "crypto_convert" {
					t.Fatal("continuation changed original operation identity")
				}
				if calls > 1 && len(input["signatures"].([]any)) != 1 {
					t.Fatal("continuation did not carry verified signature")
				}
				if scenario == "unknown" && calls == 2 {
					return nil, errors.New("response lost")
				}
				if scenario == "complete" && calls == 4 {
					return &mcp.CallToolResult{StructuredContent: document{"submitted": true}}, nil
				}
				n := calls
				if scenario == "repeated" {
					n = 1
				}
				return &mcp.CallToolResult{StructuredContent: document{"requiresSignatures": []any{request(n)}}}, nil
			}
			_, e := callSigned(context.Background(), "renamed-execution", "turnkey-execution-v1", document{"entityId": testID, "approvalId": testOtherID, "business": "crypto_convert"}, key, DefaultServer, call, trusted)
			switch scenario {
			case "complete":
				if e != nil || calls != 4 {
					t.Fatalf("multi-leg execution: %v, calls=%d", e, calls)
				}
			case "repeated":
				if e == nil || !strings.Contains(e.Error(), "repeated") || calls != 2 {
					t.Fatalf("duplicate continuation was replayed: %v, calls=%d", e, calls)
				}
			case "limit":
				if e == nil || !strings.Contains(e.Error(), "limit") || calls != 9 {
					t.Fatalf("unbounded continuation: %v, calls=%d", e, calls)
				}
			case "unknown":
				if e == nil || !strings.Contains(e.Error(), "unconfirmed") || calls != 2 {
					t.Fatalf("ambiguous continuation retried: %v, calls=%d", e, calls)
				}
			}
		})
	}
}
