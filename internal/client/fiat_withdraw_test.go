package client

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

func fiatFixture(t *testing.T) document {
	t.Helper()
	raw, err := os.ReadFile("testdata/fiat-usd-input.json")
	var args document
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		t.Fatal(err)
	}
	return args
}

func TestFiatWithdrawalIndependentWebWireFixture(t *testing.T) {
	key := testKey(t)
	c := testContext(key, DefaultServer)
	c.Binding.PolicyProfile = "financial-operations-v1"
	now := time.Date(2026, 11, 12, 1, 2, 3, 123000000, time.UTC)
	c.Binding.ExpiresAt = now.Add(5 * time.Minute).Format(time.RFC3339Nano)
	args := fiatFixture(t)
	intent, body, err := fiatWithdrawalIntent(key, c, args, now)
	if err != nil {
		t.Fatal(err)
	}
	wire, err := os.ReadFile("testdata/fiat-usd-v15.base64")
	if err != nil || strings.TrimSpace(string(wire)) != stringAt(intent, "payloadBytes") {
		t.Fatalf("Go differs from Web wire fixture: %v", err)
	}
	verifyText(t, key, stringAt(intent, "payloadBytes"), stringAt(intent, "signature"), false)
	if body["financialContext"] != nil || args["financialContext"] == nil || body["requestId"] != testOtherID {
		t.Fatal("local context leaked, input changed or nonce replaced")
	}
}

func TestFiatWithdrawalRejectsChangedRecipientSourceAndAmount(t *testing.T) {
	key := testKey(t)
	c := testContext(key, DefaultServer)
	c.Binding.PolicyProfile = "financial-operations-v1"
	for name, mutate := range map[string]func(document){
		"gross":     func(a document) { a["usdAmountMinor"] = "10001" },
		"floor":     func(a document) { a["minReceivedFiatAmountMinor"] = "9899" },
		"rail":      func(a document) { a["recipient"].(document)["manual"].(document)["rail"] = "WIRE" },
		"recipient": func(a document) { a["recipient"].(document)["manual"].(document)["accountNumber"] = "987654321" },
		"notes":     func(a document) { a["notes"] = "changed" },
		"points":    func(a document) { a["pointsPreference"] = "MAX" },
		"exact out": func(a document) { a["amountMode"] = "EXACT_OUT" },
		"source": func(a document) {
			a["financialContext"].(document)["settlement"].(document)["address"] = "0x2222222222222222222222222222222222222222"
		},
		"precision":      func(a document) { a["financialContext"].(document)["settlement"].(document)["decimals"] = float64(18) },
		"unknown option": func(a document) { a["directFunding"] = document{} },
	} {
		t.Run(name, func(t *testing.T) {
			a := fiatFixture(t)
			mutate(a)
			if _, _, err := fiatWithdrawalIntent(key, c, a, time.Now()); err == nil {
				t.Fatal("signed altered or unsupported fiat terms")
			}
		})
	}
	c.Binding.PolicyProfile = "wallet-transfer-v1"
	if _, _, err := fiatWithdrawalIntent(key, c, fiatFixture(t), time.Now()); err == nil {
		t.Fatal("wallet credential signed fiat")
	}
}
