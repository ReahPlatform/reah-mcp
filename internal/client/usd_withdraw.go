package client

import "strings"

// A resolved saved recipient retains its contact IDs and full bank terms. Manual
// recipients bind every submitted field. Gateway repeats the comparison against
// its private canonical approval before exposing any execution challenge.
func validUSDIntentTerms(d, body, context document) bool {
	if !allowedBankFields(d, "v", "bankingAccountId", "targetInstrumentId", "amount", "settlementAddress", "rail", "destinationName", "abaRoutingNumber", "accountNumber", "accountType", "accountHolderName", "bankName", "bankAddress", "notes", "contactBankAccountId", "contactId", "recipientEmail", "accountHolderType", "firstName", "lastName", "accountHolderAddress") {
		return false
	}
	if d["v"] != float64(1) && d["v"] != float64(2) {
		return false
	}
	if !validUUID(stringAt(body, "bankingAccountId")) || !validUUID(stringAt(context, "targetInstrumentId")) || !evmAddress.MatchString(stringAt(context, "settlementAddress")) || !bankInteger.MatchString(stringAt(body, "amountCents")) || stringAt(body, "amountCents") == "0" {
		return false
	}
	if d["bankingAccountId"] != body["bankingAccountId"] || d["amount"] != body["amountCents"] || d["targetInstrumentId"] != context["targetInstrumentId"] || d["settlementAddress"] != context["settlementAddress"] || d["rail"] != strings.ToLower(stringAt(body, "rail")) {
		return false
	}
	if body["rail"] != "ACH" && body["rail"] != "FEDWIRE" {
		return false
	}
	for _, name := range []string{"destinationName", "abaRoutingNumber", "accountNumber", "accountType", "accountHolderName", "bankName"} {
		if stringAt(d, name) == "" {
			return false
		}
	}
	recipient, ok := body["recipient"].(map[string]any)
	if !ok || len(recipient) != 1 {
		return false
	}
	if saved, ok := recipient["saved"].(map[string]any); ok {
		return exactFields(saved, "contactId", "contactBankAccountId", "rail") && validUUID(stringAt(saved, "contactId")) && validUUID(stringAt(saved, "contactBankAccountId")) && saved["rail"] == body["rail"] && saved["contactId"] == d["contactId"] && saved["contactBankAccountId"] == d["contactBankAccountId"]
	}
	manual, ok := recipient["manual"].(map[string]any)
	if !ok || stringAt(d, "contactId") != "" || stringAt(d, "contactBankAccountId") != "" || !allowedBankFields(manual, "destinationName", "abaRoutingNumber", "accountNumber", "accountType", "accountHolderName", "bankName", "destinationBankAddress", "accountHolderAddress") {
		return false
	}
	for _, name := range []string{"destinationName", "abaRoutingNumber", "accountNumber", "accountType", "accountHolderName", "bankName"} {
		if manual[name] != d[name] {
			return false
		}
	}
	for _, pair := range [][2]string{{"destinationBankAddress", "bankAddress"}, {"accountHolderAddress", "accountHolderAddress"}} {
		a, ok := manual[pair[0]].(map[string]any)
		if !ok {
			return false
		}
		b, ok := d[pair[1]].(map[string]any)
		if !ok {
			return false
		}
		if !allowedBankFields(a, "street1", "street2", "street3", "city", "region", "postalCode", "country") || !allowedBankFields(b, "street1", "street2", "street3", "city", "region", "postalCode", "country") {
			return false
		}
		for _, name := range []string{"street1", "street2", "street3", "city", "region", "postalCode", "country"} {
			if stringAt(a, name) != stringAt(b, name) {
				return false
			}
		}
	}
	return true
}
