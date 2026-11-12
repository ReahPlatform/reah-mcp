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

func TestFiatFXUsesIndependentCurrencyUnits(t *testing.T) {
	key := testKey(t)
	c := testContext(key, DefaultServer)
	c.Binding.PolicyProfile = "financial-operations-v1"
	for _, currency := range []string{"HKD", "JPY", "EUR"} {
		a := fiatFixture(t)
		a["receivedFiatCurrency"], a["minReceivedFiatAmountMinor"], a["estimatedReceivedFiatAmountMinor"] = currency, "70000", "71000"
		ctx := a["financialContext"].(document)
		var wire document
		_ = json.Unmarshal([]byte(stringAt(ctx, "approvalDetailsJson")), &wire)
		d := wire["bankingFiatWithdrawal"].(document)
		d["currency"], d["receivedFiatCurrency"], d["amountMinor"], d["minReceivedFiatAmountMinor"], d["estimatedReceivedFiatAmountMinor"] = currency, currency, "70000", "70000", "71000"
		if currency == "JPY" {
			d["currencyScale"], d["receivedFiatCurrencyScale"] = "0", "0"
		}
		raw, _ := json.Marshal(wire)
		ctx["approvalDetailsJson"] = string(raw)
		if _, _, err := fiatWithdrawalIntent(key, c, a, time.Now()); err != nil {
			t.Fatalf("%s receive units incorrectly compared to USD cents: %v", currency, err)
		}
		d["receivedFiatCurrencyScale"] = "3"
		raw, _ = json.Marshal(wire)
		ctx["approvalDetailsJson"] = string(raw)
		if _, _, err := fiatWithdrawalIntent(key, c, a, time.Now()); err == nil {
			t.Fatal("accepted currency scale mismatch")
		}
	}
}

func TestFiatExactOutputBindsCapTargetAndEstimates(t *testing.T) {
	key := testKey(t)
	c := testContext(key, DefaultServer)
	c.Binding.PolicyProfile = "financial-operations-v1"
	fixture := func() document {
		a := fiatFixture(t)
		a["receivedFiatCurrency"], a["amountMode"] = "HKD", "EXACT_OUT"
		for _, field := range []string{"usdAmountMinor", "minReceivedFiatAmountMinor", "estimatedReceivedFiatAmountMinor"} {
			delete(a, field)
		}
		a["exactReceivedFiatAmountMinor"], a["maxUsdAmountMinor"] = "70000", "10000"
		ctx := a["financialContext"].(document)
		var wire document
		if err := json.Unmarshal([]byte(stringAt(ctx, "approvalDetailsJson")), &wire); err != nil {
			t.Fatal(err)
		}
		d := wire["bankingFiatWithdrawal"].(document)
		for _, field := range []string{"usdAmountMinor", "minReceivedFiatAmountMinor", "estimatedReceivedFiatAmountMinor"} {
			delete(d, field)
		}
		d["currency"], d["receivedFiatCurrency"], d["amountMode"] = "HKD", "HKD", "exact_output"
		d["amountMinor"], d["exactReceivedFiatAmountMinor"], d["maxUsdAmountMinor"] = "70000", "70000", "10000"
		d["estimatedUsdAmountMinor"], d["estimatedInUsdAmountMinor"], d["railFeeAmountMinor"] = "9800", "9600", "100"
		sender := d["fromTo"].(document)["from"].([]any)[0].(document)
		sender["maxAmount"] = sender["amount"]
		delete(sender, "amount")
		raw, _ := json.Marshal(wire)
		ctx["approvalDetailsJson"] = string(raw)
		return a
	}
	if _, _, err := fiatWithdrawalIntent(key, c, fixture(), time.Now()); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(document){
		"raised cap":     func(a document) { a["maxUsdAmountMinor"] = "10001" },
		"changed target": func(a document) { a["exactReceivedFiatAmountMinor"] = "69999" },
		"mixed mode":     func(a document) { a["usdAmountMinor"] = "10000" },
		"zero estimate":  func(d document) { d["estimatedInUsdAmountMinor"] = "0" },
		"over cap":       func(d document) { d["estimatedUsdAmountMinor"] = "10001" },
		"fee overflow":   func(d document) { d["railFeeAmountMinor"] = "300" },
		"noncanonical":   func(d document) { d["estimatedUsdAmountMinor"] = "09800" },
	} {
		t.Run(name, func(t *testing.T) {
			a := fixture()
			if name == "raised cap" || name == "changed target" || name == "mixed mode" {
				mutate(a)
			} else {
				ctx := a["financialContext"].(document)
				var wire document
				_ = json.Unmarshal([]byte(stringAt(ctx, "approvalDetailsJson")), &wire)
				mutate(wire["bankingFiatWithdrawal"].(document))
				raw, _ := json.Marshal(wire)
				ctx["approvalDetailsJson"] = string(raw)
			}
			if _, _, err := fiatWithdrawalIntent(key, c, a, time.Now()); err == nil {
				t.Fatal("accepted altered exact-output terms")
			}
		})
	}
}
