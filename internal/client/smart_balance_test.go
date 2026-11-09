package client

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/dynamicpb"
)

func smartArgs(kind string) document {
	terms := document{"v": 1, "entityId": testID, "walletId": testID, "vaultId": testOtherID, "networkId": testID, "instrumentId": testID, "instrumentAccountId": testID, "treasuryAccountId": testID, "treasuryBlockchainAccountId": testID, "treasuryPositionAccountId": testOtherID, "assetAmount": "1.25", "gasSource": "gas_station"}
	raw, _ := json.Marshal(document{kind: terms})
	return document{"entityId": testID, "walletId": testID, "vaultId": testOtherID, "instrumentAccountId": testID, "assetAmount": "1.25", "gasSource": "GAS_SOURCE_GAS_STATION", "notes": "Fixture", "financialContext": document{"requestId": testOtherID, "approvalDetailsJson": string(raw)}}
}
func signedDetailsText(t *testing.T, intent document) string {
	t.Helper()
	raw, err := base64.StdEncoding.Strict().DecodeString(stringAt(intent, "payloadBytes"))
	if err != nil {
		t.Fatal(err)
	}
	m := dynamicpb.NewMessage(approvalFile.Messages().ByName("SignedApprovalIntent"))
	if err := proto.Unmarshal(raw, m); err != nil {
		t.Fatal(err)
	}
	encoded, err := protojson.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}
func TestSmartBalanceSigningKeepsDirectionAndExactTerms(t *testing.T) {
	key := testKey(t)
	c := testContext(key, DefaultServer)
	c.Binding.PolicyProfile = "financial-operations-v1"
	for _, direction := range []struct{ kind, protocol string }{{"addSmartBalance", "approval-smart-add-v1"}, {"withdrawSmartBalance", "approval-smart-withdraw-v1"}} {
		args := smartArgs(direction.kind)
		intent, body, err := newFinancialIntent(key, c, args, direction.protocol, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		text := signedDetailsText(t, intent)
		for _, term := range []string{direction.kind, `"assetAmount":"1.25"`, `"vaultId":"` + testOtherID + `"`, `"notes":"Fixture"`} {
			if !strings.Contains(text, term) {
				t.Fatalf("missing signed term: %s", term)
			}
		}
		if body["financialContext"] != nil || args["financialContext"] == nil {
			t.Fatal("local context leaked or input mutated")
		}
		verifyText(t, key, stringAt(intent, "payloadBytes"), stringAt(intent, "signature"), false)
		for _, mutate := range []func(document){func(a document) { a["assetAmount"] = "1.26" }, func(a document) { a["vaultId"] = testID }, func(a document) { a["gasSource"] = "GAS_SOURCE_FROM_WALLET" }, func(a document) { a["notes"] = 7 }, func(a document) {
			a["financialContext"].(document)["approvalDetailsJson"] = `{"sendTransaction":{"v":2}}`
		}} {
			bad := smartArgs(direction.kind)
			mutate(bad)
			if _, _, err := newFinancialIntent(key, c, bad, direction.protocol, time.Now()); err == nil {
				t.Fatal("signed changed Smart Balance terms")
			}
		}
	}
	c.Binding.PolicyProfile = "wallet-transfer-v1"
	if _, _, err := newFinancialIntent(key, c, smartArgs("addSmartBalance"), "approval-smart-add-v1", time.Now()); err == nil {
		t.Fatal("wallet credential signed Smart Balance")
	}
	c.Binding.PolicyProfile = "financial-operations-v1"
	if _, _, err := newFinancialIntent(key, c, smartArgs("addSmartBalance"), "approval-smart-withdraw-v1", time.Now()); err == nil {
		t.Fatal("direction substitution accepted")
	}
}
func TestSmartBalanceConversionBindsVault(t *testing.T) {
	key := testKey(t)
	c := testContext(key, DefaultServer)
	c.Binding.PolicyProfile = "financial-operations-v1"
	args := convertArgs()
	args["balanceSource"] = "SMART_BALANCE"
	args["smartBalanceVaultId"] = testOtherID
	intent, _, err := convertIntent(key, c, args, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	text := signedDetailsText(t, intent)
	if !strings.Contains(text, `"balanceSource":"BALANCE_SOURCE_SMART_BALANCE"`) || !strings.Contains(text, `"smartBalanceVaultId":"`+testOtherID+`"`) {
		t.Fatal("funding bucket not signed")
	}
	delete(args, "smartBalanceVaultId")
	if _, _, err := convertIntent(key, c, args, time.Now()); err == nil {
		t.Fatal("Smart Balance accepted without vault")
	}
}
func TestLocalWriteCatalogReflectsAvailableAdapters(t *testing.T) {
	result := &mcp.CallToolResult{StructuredContent: document{"write_operations": []any{document{"requestTool": "known", "executionTool": "unknown"}, document{"requestTool": "new-protocol"}}}}
	out, err := filterLocalWriteCatalog(result, []*mcp.Tool{{Name: "known"}})
	if err != nil {
		t.Fatal(err)
	}
	entries := out.StructuredContent.(document)["write_operations"].([]any)
	if len(entries) != 1 || entries[0].(document)["executionTool"] != nil {
		t.Fatal("advertised unsupported local protocol")
	}
}
func TestUSDManualRecipientBindsBankAndBothAddresses(t *testing.T) {
	addr := document{"street1": "Fixture", "city": "City", "region": "CA", "postalCode": "90001", "country": "US"}
	manual := document{"destinationName": "Fixture", "abaRoutingNumber": "123456789", "accountNumber": "1234567", "accountType": "checking", "accountHolderName": "Fixture", "bankName": "Fixture Bank", "destinationBankAddress": addr, "accountHolderAddress": addr}
	body := document{"bankingAccountId": testID, "amountCents": "1234", "rail": "ACH", "recipient": document{"manual": manual}}
	ctx := document{"targetInstrumentId": testOtherID, "settlementAddress": "0x1111111111111111111111111111111111111111"}
	d := cloneDocument(manual)
	delete(d, "destinationBankAddress")
	d["bankAddress"] = addr
	d["v"] = float64(2)
	d["amount"] = "1234"
	d["rail"] = "ach"
	d["bankingAccountId"] = testID
	d["targetInstrumentId"] = testOtherID
	d["settlementAddress"] = ctx["settlementAddress"]
	if !validUSDIntentTerms(d, body, ctx) {
		t.Fatal("valid USD terms rejected")
	}
	for _, field := range []string{"accountNumber", "bankingAccountId", "amount", "settlementAddress", "targetInstrumentId", "rail"} {
		bad := cloneDocument(d)
		bad[field] = "changed"
		if validUSDIntentTerms(bad, body, ctx) {
			t.Fatalf("accepted changed %s", field)
		}
	}
	changed := cloneDocument(d)
	changed["bankAddress"] = document{"street1": "Other"}
	if validUSDIntentTerms(changed, body, ctx) {
		t.Fatal("accepted changed bank address")
	}
}
