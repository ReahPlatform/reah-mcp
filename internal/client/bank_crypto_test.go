package client

import (
	"encoding/base64"
	"encoding/json"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/dynamicpb"
	"testing"
	"time"
)

func bankCryptoArgs(withdraw bool) document {
	a := convertArgs()
	c := a["convertContext"].(document)
	c["sourceAsset"] = c["destinationAsset"]
	c["originTokenAddress"] = c["destinationTokenAddress"]
	c["sourceDecimals"] = 6
	c["destinationInstrumentId"], c["destinationAddress"] = a["destinationInstrumentId"], a["destinationAddress"]
	delete(a, "convertContext")
	delete(a, "destinationInstrumentId")
	delete(a, "destinationAddress")
	delete(a, "amountMode")
	a["bankCryptoContext"], a["bankingAccountId"], a["flowVersion"] = c, testID, "V2"
	if withdraw {
		delete(a, "originInstrumentAccountId")
		delete(a, "amount")
		a["amountCents"], a["targetInstrumentAccountId"] = "100", testOtherID
		c["totalBankingDebitUsdCents"] = "100"
	}
	return a
}

func TestBankCryptoV2FundingIntent(t *testing.T) {
	key := testKey(t)
	c := testContext(key, DefaultServer)
	c.Binding.PolicyProfile = "financial-operations-v1"
	for _, withdraw := range []bool{false, true} {
		for _, bankGas := range []bool{false, true} {
			if bankGas && !withdraw {
				continue
			}
			a := bankCryptoArgs(withdraw)
			if bankGas {
				a["gasSource"] = "GAS_SOURCE_FROM_BANKING"
				a["bankCryptoContext"].(document)["totalBankingDebitUsdCents"] = "184"
			}
			a["pointsPreference"], a["pointsQuoteToken"], a["idempotencyKey"] = "MAX", "original-quote", testOtherID
			intent, body, err := bankCryptoIntent(key, c, a, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			verifyText(t, key, stringAt(intent, "payloadBytes"), stringAt(intent, "signature"), false)
			wire, _ := base64.StdEncoding.DecodeString(stringAt(intent, "payloadBytes"))
			m := dynamicpb.NewMessage(approvalFile.Messages().ByName("SignedApprovalIntent"))
			if proto.Unmarshal(wire, m) != nil {
				t.Fatal("invalid intent")
			}
			raw, _ := protojson.Marshal(m)
			var signed document
			_ = json.Unmarshal(raw, &signed)
			d := signed["details"].(document)["cryptoCryptoConvert"].(document)
			if d["v"] != float64(4) || d["sourceAsset"] != d["destinationAsset"] || body["bankCryptoContext"] != nil || body["pointsQuoteToken"] != "original-quote" {
				t.Fatalf("lost original v4/Points contract: %s", raw)
			}
			if withdraw {
				want := "1.00"
				if bankGas {
					want = "1.84"
				}
				if d["amount"] != want || d["bankingAccountId"] != testID || d["gasSource"] != "gas_station" {
					t.Fatalf("bank budget not bound: %s", raw)
				}
			}
			if a["bankCryptoContext"] == nil {
				t.Fatal("mutated input")
			}
		}
	}
}

func TestBankCryptoRejectsMismatchedTerms(t *testing.T) {
	key := testKey(t)
	c := testContext(key, DefaultServer)
	c.Binding.PolicyProfile = "financial-operations-v1"
	for name, mutate := range map[string]func(document){
		"unfunded bank gas":    func(a document) { a["bankCryptoContext"].(document)["totalBankingDebitUsdCents"] = "99" },
		"station excess":       func(a document) { a["bankCryptoContext"].(document)["totalBankingDebitUsdCents"] = "184" },
		"ambiguous target":     func(a document) { a["walletId"], a["targetInstrumentId"] = testID, testOtherID },
		"missing Points quote": func(a document) { a["pointsPreference"] = "MAX" },
		"future terms":         func(a document) { a["scheduledAt"] = "tomorrow" },
		"smart banking":        func(a document) { a["balanceSource"] = "SMART_BALANCE" },
		"unknown context":      func(a document) { a["bankCryptoContext"].(document)["payload"] = "arbitrary" },
		"legacy":               func(a document) { a["flowVersion"] = "LEGACY" },
	} {
		t.Run(name, func(t *testing.T) {
			a := bankCryptoArgs(true)
			mutate(a)
			if _, _, err := bankCryptoIntent(key, c, a, time.Now()); err == nil {
				t.Fatal("signed unsafe terms")
			}
		})
	}
}
