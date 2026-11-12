package client

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/dynamicpb"
)

func bankTransferArgs() document {
	context := document{"expiresAt": time.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano), "sourceBankingAccountId": testID, "targetBankingAccountId": testOtherID, "walletId": testID, "networkId": testID, "instrumentId": testID, "asset": "eip155:8453/erc20:0x3333333333333333333333333333333333333333", "decimals": 6, "fromAddress": "0x1111111111111111111111111111111111111111", "toAddress": "0x2222222222222222222222222222222222222222", "amountCents": "1000", "amountAtomic": "10000000", "totalDebitCents": "1000", "gasFeeCents": "2"}
	party := func(id, address string) document {
		return document{"partyType": "PARTY_TYPE_BANKING", "bankingId": id, "address": address, "instrumentId": testID, "amount": "10000000", "estimatedUsd": "1000"}
	}
	send := document{"v": 1, "amount": "10000000", "notes": "Banking fixture", "gasSource": "gas_station", "gasUsdFee": "2", "gas": document{"usd": "2", "payerAccountType": "gas_station"}, "fromTo": document{"from": []any{party(testID, stringAt(context, "fromAddress"))}, "to": []any{party(testOtherID, stringAt(context, "toAddress"))}}}
	for _, name := range []string{"walletId", "networkId", "instrumentId", "fromAddress", "toAddress"} {
		send[name] = context[name]
	}
	raw, _ := json.Marshal(document{"sendTransaction": send})
	context["approvalDetailsJson"] = string(raw)
	return document{"entityId": testID, "quoteId": testID, "requestId": testOtherID, "bankTransferContext": context}
}

func mutateBankQuote(args document, f func(document)) {
	c := args["bankTransferContext"].(document)
	var d document
	if err := json.Unmarshal([]byte(stringAt(c, "approvalDetailsJson")), &d); err != nil {
		panic(err)
	}
	f(d["sendTransaction"].(document))
	raw, err := json.Marshal(d)
	if err != nil {
		panic(err)
	}
	c["approvalDetailsJson"] = string(raw)
}

func TestBankTransferWebWireFixture(t *testing.T) {
	key := testKey(t)
	c := testContext(key, DefaultServer)
	c.Binding.PolicyProfile = "financial-operations-v1"
	now := time.Date(2026, 10, 28, 1, 2, 3, 123000000, time.UTC)
	c.Binding.ExpiresAt = now.Add(time.Hour).Format(time.RFC3339Nano)
	args := bankTransferArgs()
	args["bankTransferContext"].(document)["expiresAt"] = now.Add(time.Minute).Format(time.RFC3339Nano)
	intent, body, e := bankTransferIntent(key, c, args, now)
	if e != nil {
		t.Fatal(e)
	}
	fixture, e := os.ReadFile("testdata/bank-internal-v1.base64")
	if e != nil || strings.TrimSpace(string(fixture)) != stringAt(intent, "payloadBytes") {
		t.Fatalf("Go differs from independent Web fixture: %v", e)
	}
	verifyText(t, key, stringAt(intent, "payloadBytes"), stringAt(intent, "signature"), false)
	if !exactFields(body, "entityId", "quoteId", "requestId") || args["bankTransferContext"] == nil {
		t.Fatal("context leaked or input mutated")
	}
}

func TestBankTransferRejectsUnsafeQuotes(t *testing.T) {
	key := testKey(t)
	c := testContext(key, DefaultServer)
	c.Binding.PolicyProfile = "financial-operations-v1"
	mutations := map[string]func(document){
		"expired": func(a document) {
			a["bankTransferContext"].(document)["expiresAt"] = time.Now().Add(-time.Second).Format(time.RFC3339Nano)
		},
		"other entity":     func(a document) { a["entityId"] = testOtherID },
		"same accounts":    func(a document) { a["bankTransferContext"].(document)["targetBankingAccountId"] = testID },
		"same addresses":   func(a document) { c := a["bankTransferContext"].(document); c["toAddress"] = c["fromAddress"] },
		"numeric amount":   func(a document) { a["bankTransferContext"].(document)["amountCents"] = 1000 },
		"atomic precision": func(a document) { a["bankTransferContext"].(document)["decimals"] = 18 },
		"null decimals":    func(a document) { a["bankTransferContext"].(document)["decimals"] = nil },
		"debit differs":    func(a document) { a["bankTransferContext"].(document)["totalDebitCents"] = "1002" },
		"native asset":     func(a document) { a["bankTransferContext"].(document)["asset"] = "eip155:8453/slip44:60" },
		"zero token": func(a document) {
			a["bankTransferContext"].(document)["asset"] = "eip155:8453/erc20:0x0000000000000000000000000000000000000000"
		},
		"unknown context":   func(a document) { a["bankTransferContext"].(document)["payload"] = "arbitrary" },
		"arbitrary signing": func(a document) { a["requestSignature"] = document{} },
		"future populated asset": func(a document) {
			mutateBankQuote(a, func(s document) { s["asset"] = a["bankTransferContext"].(document)["asset"] })
		},
		"wallet send": func(a document) { mutateBankQuote(a, func(s document) { s["v"] = 2 }) },
		"wrong recipient": func(a document) {
			mutateBankQuote(a, func(s document) { s["toAddress"] = "0x4444444444444444444444444444444444444444" })
		},
		"wrong atomic amount": func(a document) { mutateBankQuote(a, func(s document) { s["amount"] = "10000001" }) },
		"changed bank": func(a document) {
			mutateBankQuote(a, func(s document) { s["fromTo"].(document)["to"].([]any)[0].(document)["bankingId"] = testID })
		},
		"changed party amount": func(a document) {
			mutateBankQuote(a, func(s document) { s["fromTo"].(document)["from"].([]any)[0].(document)["amount"] = "10000001" })
		},
		"grouped address": func(a document) {
			mutateBankQuote(a, func(s document) {
				s["fromTo"].(document)["to"].([]any)[0].(document)["addresses"] = []any{s["toAddress"]}
			})
		},
		"gas split": func(a document) {
			mutateBankQuote(a, func(s document) {
				to := s["fromTo"].(document)["to"].([]any)
				s["fromTo"].(document)["to"] = append(to, to[0])
			})
		},
		"bank pays gas": func(a document) {
			mutateBankQuote(a, func(s document) { s["gas"].(document)["payerAccountType"] = "banking_account" })
		},
		"gas payer id": func(a document) {
			mutateBankQuote(a, func(s document) { s["gas"].(document)["payerAccountId"] = testID })
		},
		"changed gas fee": func(a document) { mutateBankQuote(a, func(s document) { s["gasUsdFee"] = "3" }) },
		"smart balance": func(a document) {
			mutateBankQuote(a, func(s document) { s["balanceSource"] = "BALANCE_SOURCE_SMART_BALANCE" })
		},
		"memo":                   func(a document) { mutateBankQuote(a, func(s document) { s["memo"] = "new" }) },
		"unknown protobuf field": func(a document) { mutateBankQuote(a, func(s document) { s["arbitraryPayload"] = "new" }) },
		"duplicate field": func(a document) {
			ctx := a["bankTransferContext"].(document)
			ctx["approvalDetailsJson"] = strings.Replace(stringAt(ctx, "approvalDetailsJson"), `"v":1`, `"v":1,"v":1`, 1)
		},
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			a := bankTransferArgs()
			mutate(a)
			if _, _, e := bankTransferIntent(key, c, a, time.Now()); e == nil {
				t.Fatal("signed unsafe quote")
			}
		})
	}
	c.Binding.PolicyProfile = "wallet-transfer-v1"
	if _, _, e := bankTransferIntent(key, c, bankTransferArgs(), time.Now()); e == nil {
		t.Fatal("wallet credential signed Banking")
	}
}

func TestBankTransferCallBindsFrozenTermsAndNeverRetries(t *testing.T) {
	key := testKey(t)
	c := testContext(key, DefaultServer)
	c.Binding.PolicyProfile = "financial-operations-v1"
	for _, unknown := range []bool{false, true} {
		calls := 0
		args := bankTransferArgs()
		call := func(_ context.Context, name string, input document) (*mcp.CallToolResult, error) {
			if name == "reah_signing_context" {
				return &mcp.CallToolResult{StructuredContent: c}, nil
			}
			calls++
			if name != "renamed-bank" || input["bankTransferContext"] != nil {
				t.Fatal("name dependency or context leaked")
			}
			proof := input["_proof"].(document)
			verifyText(t, key, stringAt(proof, "payload"), stringAt(proof, "signature"), false)
			var p document
			if json.Unmarshal([]byte(stringAt(proof, "payload")), &p) != nil {
				t.Fatal("proof")
			}
			bound := p["arguments"].(document)
			if p["tool"] != name || bound["quoteId"] != args["quoteId"] || bound["requestId"] != args["requestId"] || bound["requestSignature"] == nil {
				t.Fatal("unbound identity")
			}
			signature := input["requestSignature"].(document)
			verifyText(t, key, stringAt(signature, "payloadBytes"), stringAt(signature, "signature"), false)
			wire, _ := base64.StdEncoding.DecodeString(stringAt(signature, "payloadBytes"))
			message := dynamicpb.NewMessage(approvalFile.Messages().ByName("SignedApprovalIntent"))
			if proto.Unmarshal(wire, message) != nil {
				t.Fatal("intent")
			}
			raw, _ := protojson.Marshal(message)
			var intent document
			if json.Unmarshal(raw, &intent) != nil {
				t.Fatal("intent JSON")
			}
			var original document
			if json.Unmarshal([]byte(stringAt(args["bankTransferContext"].(document), "approvalDetailsJson")), &original) != nil {
				t.Fatal("quote")
			}
			actual, _ := json.Marshal(intent["details"])
			expected, _ := json.Marshal(original)
			if string(actual) != string(expected) || intent["nonce"] != args["requestId"] {
				t.Fatal("frozen quote changed")
			}
			if unknown {
				return nil, errors.New("transport closed")
			}
			return &mcp.CallToolResult{StructuredContent: document{"approvalId": testOtherID}}, nil
		}
		_, e := callSigned(context.Background(), "renamed-bank", "approval-financial-v1", args, key, DefaultServer, call, "")
		if calls != 1 || unknown != (e != nil) {
			t.Fatalf("dispatch unknown=%v calls=%d error=%v", unknown, calls, e)
		}
	}
}

func TestBankTransferCatalogShape(t *testing.T) {
	makeTool := func(name string, extra bool) *mcp.Tool {
		props := document{}
		required := []any{}
		for _, field := range bankTransferFields {
			props[field] = document{"type": "string"}
			required = append(required, field)
		}
		props["requestSignature"], props["_proof"] = document{}, document{}
		if extra {
			props["usdAmountMinor"] = document{"type": "string"}
		}
		return &mcp.Tool{Name: name, Meta: mcp.Meta{localMeta: document{"version": 1, "mode": "signed", "signing": "approval-financial-v1"}}, Annotations: &mcp.ToolAnnotations{ReadOnlyHint: false}, InputSchema: document{"type": "object", "properties": props, "required": required}}
	}
	out, e := publicTools([]*mcp.Tool{makeTool("bank", false), makeTool("renamed", false), makeTool("fiat", true)})
	if e != nil || len(out) != 2 {
		t.Fatalf("catalog %v %d", e, len(out))
	}
	for _, tool := range out {
		props := tool.InputSchema.(document)["properties"].(document)
		if props["bankTransferContext"] == nil || props["convertContext"] != nil || props["requestSignature"] != nil {
			t.Fatal("bad context")
		}
	}
}

func bankPaidTransferArgs() document {
	a := bankTransferArgs()
	c := a["bankTransferContext"].(document)
	c["totalDebitCents"] = "1002"
	mutateBankQuote(a, func(s document) {
		s["amount"] = "10020000"
		s["gas"].(document)["payerAccountType"], s["gas"].(document)["payerAccountId"] = "banking_account", testID
		parties := s["fromTo"].(document)
		parties["from"].([]any)[0].(document)["amount"] = "10020000"
		parties["to"] = append(parties["to"].([]any), document{"partyType": "PARTY_TYPE_ADDRESS", "address": "0x4444444444444444444444444444444444444444", "instrumentId": testID, "amount": "20000", "estimatedUsd": "2"})
	})
	return a
}

func TestBankPaidInternalFrozenQuote(t *testing.T) {
	key := testKey(t)
	c := testContext(key, DefaultServer)
	c.Binding.PolicyProfile = "financial-operations-v1"
	if _, _, err := bankTransferIntent(key, c, bankPaidTransferArgs(), time.Now()); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(document){
		"collector amount": func(s document) { s["fromTo"].(document)["to"].([]any)[1].(document)["amount"] = "20001" },
		"payer":            func(s document) { s["gas"].(document)["payerAccountId"] = testOtherID },
		"collector source": func(s document) { s["fromTo"].(document)["to"].([]any)[1].(document)["address"] = s["fromAddress"] },
		"extra output":     func(s document) { p := s["fromTo"].(document); p["to"] = append(p["to"].([]any), p["to"].([]any)[1]) },
	} {
		t.Run(name, func(t *testing.T) {
			a := bankPaidTransferArgs()
			mutateBankQuote(a, mutate)
			if _, _, err := bankTransferIntent(key, c, a, time.Now()); err == nil {
				t.Fatal("signed altered fee split")
			}
		})
	}
}
