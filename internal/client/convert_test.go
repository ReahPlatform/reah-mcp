package client

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/dynamicpb"
)

func convertArgs() document {
	return document{"entityId": testID, "originInstrumentAccountId": testID, "destinationInstrumentId": testOtherID, "amount": "0.000001", "amountMode": "EXACT_INPUT", "destinationAddress": "0x2222222222222222222222222222222222222222", "gasSource": "GAS_SOURCE_GAS_STATION", "notes": "Swap fixture", "executionConstraint": document{"percentage": "1", "minimumReceive": "0.0025"}, "convertContext": document{"requestId": testOtherID, "originNetworkId": testID, "destinationNetworkId": testID, "sourceAsset": "eip155:8453/slip44:60", "destinationAsset": "eip155:8453/erc20:0x3333333333333333333333333333333333333333", "originTokenAddress": "", "destinationTokenAddress": "0x3333333333333333333333333333333333333333", "originAddress": "0x1111111111111111111111111111111111111111", "sourceDecimals": 18, "destinationDecimals": 6}}
}

func TestConvertWebWireFixtures(t *testing.T) {
	key := testKey(t)
	c := testContext(key, DefaultServer)
	c.Binding.PolicyProfile = "financial-operations-v1"
	now := time.Date(2026, 10, 28, 1, 2, 3, 123000000, time.UTC)
	c.Binding.ExpiresAt = now.Add(time.Hour).Format(time.RFC3339Nano)
	for _, mode := range []string{"EXACT_INPUT", "MINIMUM_OUTPUT"} {
		t.Run(mode, func(t *testing.T) {
			args := convertArgs()
			file := "testdata/convert-v4.base64"
			if mode == "MINIMUM_OUTPUT" {
				args["amountMode"], args["requestedTargetAmount"] = mode, "0.0025"
				file = "testdata/convert-minimum-v4.base64"
			}
			intent, body, e := convertIntent(key, c, args, now)
			if e != nil {
				t.Fatal(e)
			}
			fixture, e := os.ReadFile(file)
			if e != nil || strings.TrimSpace(string(fixture)) != stringAt(intent, "payloadBytes") {
				t.Fatalf("Go conversion differs from independent Web protobuf fixture: %v", e)
			}
			verifyText(t, key, stringAt(intent, "payloadBytes"), stringAt(intent, "signature"), false)
			if body["convertContext"] != nil || args["convertContext"] == nil {
				t.Fatal("local context leaked upstream or input mutated")
			}
		})
	}
}

func TestConvertRejectsUnsupportedTerms(t *testing.T) {
	key := testKey(t)
	c := testContext(key, DefaultServer)
	c.Binding.PolicyProfile = "financial-operations-v1"
	mutations := map[string]func(document){
		"fractional smallest unit": func(a document) { a["amount"] = "0.0000000000000000001" },
		"numeric amount":           func(a document) { a["amount"] = 0.1 },
		"scientific amount":        func(a document) { a["amount"] = "1e-6" },
		"zero amount":              func(a document) { a["amount"] = "0" },
		"exact output":             func(a document) { a["amountMode"] = "EXACT_OUTPUT" },
		"missing floor":            func(a document) { delete(a["executionConstraint"].(document), "minimumReceive") },
		"zero floor":               func(a document) { a["executionConstraint"].(document)["minimumReceive"] = "0" },
		"negative percentage":      func(a document) { a["executionConstraint"].(document)["percentage"] = "-1" },
		"oversized percentage":     func(a document) { a["executionConstraint"].(document)["percentage"] = "101" },
		"floor precision":          func(a document) { a["executionConstraint"].(document)["minimumReceive"] = "0.0025001" },
		"target mismatch":          func(a document) { a["amountMode"], a["requestedTargetAmount"] = "MINIMUM_OUTPUT", "0.003" },
		"unexpected target":        func(a document) { a["requestedTargetAmount"] = "0.0025" },
		"smart balance":            func(a document) { a["balanceSource"] = "SMART_BALANCE" },
		"points":                   func(a document) { a["pointsPreference"] = "MAX" },
		"points quote":             func(a document) { a["pointsQuoteToken"] = "token" },
		"bank source":              func(a document) { a["bankingAccountId"] = testID },
		"arbitrary signing":        func(a document) { a["requestSignature"] = document{} },
		"null decimals":            func(a document) { a["convertContext"].(document)["sourceDecimals"] = nil },
		"unknown context":          func(a document) { a["convertContext"].(document)["payload"] = "arbitrary" },
		"inferred token":           func(a document) { a["convertContext"].(document)["destinationTokenAddress"] = "" },
		"asset token mismatch": func(a document) {
			a["convertContext"].(document)["destinationTokenAddress"] = "0x4444444444444444444444444444444444444444"
		},
		"chain network mismatch": func(a document) {
			a["convertContext"].(document)["destinationAsset"] = "eip155:1/erc20:0x3333333333333333333333333333333333333333"
		},
		"invalid source": func(a document) {
			a["convertContext"].(document)["originAddress"] = "0x0000000000000000000000000000000000000000"
		},
		"other entity": func(a document) { a["entityId"] = testOtherID },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			args := convertArgs()
			mutate(args)
			if _, _, e := convertIntent(key, c, args, time.Now()); e == nil {
				t.Fatal("signed unsupported conversion")
			}
		})
	}
	c.Binding.PolicyProfile = "wallet-transfer-v1"
	if _, _, e := convertIntent(key, c, convertArgs(), time.Now()); e == nil {
		t.Fatal("wallet-only credential signed conversion")
	}
}

func TestConvertCallBindsExactTerms(t *testing.T) {
	key := testKey(t)
	c := testContext(key, DefaultServer)
	c.Binding.PolicyProfile = "financial-operations-v1"
	args := convertArgs()
	// Cross-chain conversion reuses the same adapter as a same-chain swap.
	args["convertContext"].(document)["destinationNetworkId"] = testOtherID
	args["convertContext"].(document)["destinationAsset"] = "eip155:1/erc20:0x3333333333333333333333333333333333333333"
	calls := 0
	call := func(_ context.Context, name string, input document) (*mcp.CallToolResult, error) {
		if name == "reah_signing_context" {
			return &mcp.CallToolResult{StructuredContent: c}, nil
		}
		calls++
		if name != "renamed-convert" || input["convertContext"] != nil {
			t.Fatal("tool-name dependency or local context leaked")
		}
		proof := input["_proof"].(document)
		verifyText(t, key, stringAt(proof, "payload"), stringAt(proof, "signature"), false)
		var p document
		if json.Unmarshal([]byte(stringAt(proof, "payload")), &p) != nil {
			t.Fatal("invalid proof")
		}
		bound := p["arguments"].(document)
		if p["tool"] != name || bound["amount"] != args["amount"] || bound["destinationAddress"] != args["destinationAddress"] || bound["requestSignature"] == nil {
			t.Fatal("operation proof did not bind exact submitted terms")
		}
		signature := input["requestSignature"].(document)
		verifyText(t, key, stringAt(signature, "payloadBytes"), stringAt(signature, "signature"), false)
		wire, _ := base64.StdEncoding.DecodeString(stringAt(signature, "payloadBytes"))
		m := dynamicpb.NewMessage(approvalFile.Messages().ByName("SignedApprovalIntent"))
		if proto.Unmarshal(wire, m) != nil {
			t.Fatal("invalid intent")
		}
		text, _ := protojson.Marshal(m)
		for _, expected := range []string{"eip155:1/erc20:", `"nonce":"` + testOtherID + `"`, `"amountMode":"exact_input"`} {
			if !strings.Contains(string(text), expected) {
				t.Fatalf("missing signed conversion context: %s", expected)
			}
		}
		return &mcp.CallToolResult{StructuredContent: document{"approvalId": testOtherID}}, nil
	}
	if _, e := callSigned(context.Background(), "renamed-convert", "approval-financial-v1", args, key, DefaultServer, call, ""); e != nil || calls != 1 {
		t.Fatalf("conversion dispatch: %v, calls=%d", e, calls)
	}
}

func TestConvertCatalogCapabilities(t *testing.T) {
	makeTool := func(name string, bank bool) *mcp.Tool {
		props, required := document{}, []any{}
		for _, field := range convertRequired {
			props[field] = document{"type": "string"}
			required = append(required, field)
		}
		props["_proof"], props["requestSignature"] = document{}, document{}
		if bank {
			props["bankingAccountId"] = document{"type": "string"}
		}
		return &mcp.Tool{Name: name, Meta: mcp.Meta{localMeta: document{"version": 1, "mode": "signed", "signing": "approval-financial-v1"}}, InputSchema: document{"type": "object", "properties": props, "required": required}, Annotations: &mcp.ToolAnnotations{ReadOnlyHint: false}}
	}
	tools := []*mcp.Tool{makeTool("swap", false), makeTool("renamed-convert", false), makeTool("bank", true)}
	out, e := publicTools(tools)
	if e != nil || len(out) != 2 {
		t.Fatalf("supported intent shape selection: %v (%d)", e, len(out))
	}
	for _, tool := range out {
		schema := tool.InputSchema.(document)
		props := schema["properties"].(document)
		if props["convertContext"] == nil || props["requestSignature"] != nil || props["_proof"] != nil {
			t.Fatal("invalid local convert schema")
		}
	}
}
