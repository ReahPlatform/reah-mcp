package client

import (
	"encoding/json"
	"errors"
	"math/big"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/dynamicpb"
)

var fiatCurrency = regexp.MustCompile(`^[A-Z]{3}$`)

const fiatWithdrawalProtocol = "approval-fiat-withdraw-v1"

var fiatWithdrawalRequired = []string{"entityId", "bankingAccountId", "requestId", "receivedFiatCurrency", "recipient", "amountMode", "pointsPreference"}
var fiatWithdrawalOptional = []string{"notes", "paymentReference", "recipientEmail", "paymentReason", "usdAmountMinor", "minReceivedFiatAmountMinor", "estimatedReceivedFiatAmountMinor", "exactReceivedFiatAmountMinor", "maxUsdAmountMinor"}

func fiatWithdrawalSchema(schema document) bool {
	props, _ := schema["properties"].(map[string]any)
	required, _ := schema["required"].([]any)
	for _, name := range fiatWithdrawalRequired {
		if props[name] == nil || !slices.Contains(required, any(name)) {
			return false
		}
	}
	for name := range props {
		if !slices.Contains(fiatWithdrawalRequired, name) && !slices.Contains(fiatWithdrawalOptional, name) && !slices.Contains(signatureFields, name) {
			return false
		}
	}
	return true
}

func addFiatWithdrawalContext(schema document) {
	props := schema["properties"].(map[string]any)
	settlement := document{}
	for _, name := range []string{"address", "instrumentId", "networkId", "asset"} {
		settlement[name] = document{"type": "string"}
	}
	settlement["decimals"] = document{"type": "integer", "minimum": 6, "maximum": 6}
	props["financialContext"] = document{
		"type": "object", "additionalProperties": false,
		"required": []string{"approvalDetailsJson", "settlement"},
		"properties": document{
			"approvalDetailsJson": document{"type": "string", "maxLength": 32768, "description": "Copy the exact prepared Banking fiat ApprovalDetails JSON; never supply a provider payload."},
			"settlement":          document{"type": "object", "additionalProperties": false, "properties": settlement, "required": []string{"address", "instrumentId", "networkId", "asset", "decimals"}},
		},
	}
	schema["required"] = append(schema["required"].([]any), "financialContext")
}

// Financial normalization and routing belong to Ledger/Gateway. This adapter
// verifies the closed prepared contract and signs its exact protobuf intent.
func fiatWithdrawalIntent(key DeviceKey, c SigningContext, args document, now time.Time) (document, document, error) {
	bad := errors.New("use the exact prepared bank-funded fiat terms, resolved recipient and settlement context; unsupported routes require the existing human flow")
	allowed := append(append(append([]string{}, fiatWithdrawalRequired...), fiatWithdrawalOptional...), "financialContext")
	if c.Binding.PolicyProfile != "financial-operations-v1" || !allowedBankFields(args, allowed...) || args["entityId"] != c.Binding.EntityID || approvalFileError != nil {
		return nil, nil, bad
	}
	for _, name := range fiatWithdrawalRequired {
		if name != "recipient" && stringAt(args, name) == "" {
			return nil, nil, bad
		}
	}
	for _, name := range []string{"entityId", "bankingAccountId", "requestId"} {
		if !validUUID(stringAt(args, name)) {
			return nil, nil, bad
		}
	}
	if !fiatCurrency.MatchString(stringAt(args, "receivedFiatCurrency")) || (args["amountMode"] != "EXACT_IN" && args["amountMode"] != "EXACT_OUT") || args["pointsPreference"] != "NONE" {
		return nil, nil, bad
	}
	exactOut := args["amountMode"] == "EXACT_OUT"
	amounts := []string{"usdAmountMinor", "minReceivedFiatAmountMinor", "estimatedReceivedFiatAmountMinor"}
	absent := []string{"exactReceivedFiatAmountMinor", "maxUsdAmountMinor"}
	if exactOut {
		amounts, absent = absent, amounts
		if args["receivedFiatCurrency"] == "USD" {
			return nil, nil, bad
		}
	}
	for _, name := range amounts {
		if !bankInteger.MatchString(stringAt(args, name)) || args[name] == "0" {
			return nil, nil, bad
		}
	}
	for _, name := range absent {
		if _, exists := args[name]; exists {
			return nil, nil, bad
		}
	}
	if !exactOut {
		minimum, _ := new(big.Int).SetString(stringAt(args, "minReceivedFiatAmountMinor"), 10)
		estimate, _ := new(big.Int).SetString(stringAt(args, "estimatedReceivedFiatAmountMinor"), 10)
		gross, _ := new(big.Int).SetString(stringAt(args, "usdAmountMinor"), 10)
		if minimum.Cmp(estimate) > 0 || args["receivedFiatCurrency"] == "USD" && estimate.Cmp(gross) > 0 {
			return nil, nil, bad
		}
	}
	ctx, ok := args["financialContext"].(map[string]any)
	if !ok || !exactFields(ctx, "approvalDetailsJson", "settlement") {
		return nil, nil, bad
	}
	settlement, ok := ctx["settlement"].(map[string]any)
	if !ok || !exactFields(settlement, "address", "instrumentId", "networkId", "asset", "decimals") {
		return nil, nil, bad
	}
	decimals, ok := contextDecimals(settlement, "decimals")
	asset := stringAt(settlement, "asset")
	caip := evmAsset.FindStringSubmatch(asset)
	if !ok || decimals != 6 || !validUUID(stringAt(settlement, "instrumentId")) || !validUUID(stringAt(settlement, "networkId")) || !evmAddress.MatchString(stringAt(settlement, "address")) || strings.EqualFold(stringAt(settlement, "address"), "0x0000000000000000000000000000000000000000") || caip == nil || caip[1] != "8453" || !strings.HasPrefix(caip[2], "erc20:") || convertAsset(asset, strings.TrimPrefix(caip[2], "erc20:")) == "" {
		return nil, nil, bad
	}
	raw := stringAt(ctx, "approvalDetailsJson")
	if raw == "" || len(raw) > 32768 {
		return nil, nil, bad
	}
	message := dynamicpb.NewMessage(approvalFile.Messages().ByName("ApprovalDetails"))
	if protojson.Unmarshal([]byte(raw), message) != nil {
		return nil, nil, bad
	}
	canonical, err := protojson.Marshal(message)
	var details document
	if err != nil || json.Unmarshal(canonical, &details) != nil || !exactFields(details, "bankingFiatWithdrawal") {
		return nil, nil, bad
	}
	d, ok := details["bankingFiatWithdrawal"].(map[string]any)
	if !ok || !validFiatWithdrawalTerms(d, args, settlement, decimals) {
		return nil, nil, bad
	}
	body := cloneDocument(args)
	delete(body, "financialContext")
	intent, err := approvalIntent(key, c, details, stringAt(args, "requestId"), now)
	return intent, body, err
}

func validFiatWithdrawalTerms(d, body, settlement document, decimals int) bool {
	if !allowedBankFields(d, "v", "bankingAccountId", "amountMinor", "currency", "currencyScale", "settlementAddress", "rail", "destinationName", "accountNumber", "routingNumber", "accountType", "accountHolderName", "bankName", "bankAddress", "notes", "contactId", "contactBankAccountId", "recipientEmail", "minReceivedFiatAmountMinor", "usdAmountMinor", "receivedFiatCurrency", "receivedFiatCurrencyScale", "accountHolderType", "firstName", "lastName", "accountHolderAddress", "estimatedReceivedFiatAmountMinor", "paymentReason", "seismicHolderAddressRequired", "seismicHolderAddressRequirementResolved", "amountMode", "railFeeAmountMinor", "taxNumber", "paymentReference", "fromTo", "naicsCode", "relationship", "partyType", "iban", "bic", "sortCode", "cnapsCode", "idNumber", "phone", "intermediaryBic", "intermediaryRoutingNumber", "swiftPaymentCode", "ifsc", "bsb", "institutionNumber", "transitNumber", "vpa", "exactReceivedFiatAmountMinor", "maxUsdAmountMinor", "estimatedUsdAmountMinor", "estimatedInUsdAmountMinor") || (d["v"] != float64(9) && d["v"] != float64(15)) {
		return false
	}
	if d["bankingAccountId"] != body["bankingAccountId"] || d["currency"] != body["receivedFiatCurrency"] || d["receivedFiatCurrency"] != body["receivedFiatCurrency"] || stringAt(d, "currencyScale") != stringAt(d, "receivedFiatCurrencyScale") || d["settlementAddress"] != settlement["address"] {
		return false
	}
	exactOut := body["amountMode"] == "EXACT_OUT"
	if exactOut {
		if d["amountMode"] != "exact_output" || d["amountMinor"] != body["exactReceivedFiatAmountMinor"] || d["exactReceivedFiatAmountMinor"] != body["exactReceivedFiatAmountMinor"] || d["maxUsdAmountMinor"] != body["maxUsdAmountMinor"] || stringAt(d, "usdAmountMinor") != "" || stringAt(d, "minReceivedFiatAmountMinor") != "" || stringAt(d, "estimatedReceivedFiatAmountMinor") != "" {
			return false
		}
		cap, _ := new(big.Int).SetString(stringAt(body, "maxUsdAmountMinor"), 10)
		amounts := make([]*big.Int, 0, 3)
		for i, name := range []string{"estimatedUsdAmountMinor", "estimatedInUsdAmountMinor", "railFeeAmountMinor"} {
			raw := stringAt(d, name)
			if !bankInteger.MatchString(raw) {
				return false
			}
			n, _ := new(big.Int).SetString(raw, 10)
			if i < 2 && n.Sign() <= 0 {
				return false
			}
			amounts = append(amounts, n)
		}
		if new(big.Int).Add(amounts[1], amounts[2]).Cmp(amounts[0]) > 0 || amounts[0].Cmp(cap) > 0 {
			return false
		}
	} else if (d["amountMinor"] != body["minReceivedFiatAmountMinor"] && (body["receivedFiatCurrency"] != "USD" || d["amountMinor"] != body["usdAmountMinor"])) || d["usdAmountMinor"] != body["usdAmountMinor"] || (stringAt(d, "amountMode") != "" && stringAt(d, "amountMode") != "exact_input") || stringAt(d, "exactReceivedFiatAmountMinor") != "" || stringAt(d, "maxUsdAmountMinor") != "" || stringAt(d, "estimatedUsdAmountMinor") != "" || stringAt(d, "estimatedInUsdAmountMinor") != "" {
		return false
	}
	scaleRaw := stringAt(d, "currencyScale")
	if scaleRaw == "" {
		scaleRaw = "0"
	}
	scale, err := strconv.Atoi(scaleRaw)
	if err != nil || scale < 0 || scale > 6 {
		return false
	}
	for _, name := range []string{"minReceivedFiatAmountMinor", "estimatedReceivedFiatAmountMinor", "notes", "paymentReference", "recipientEmail"} {
		if stringAt(d, name) != stringAt(body, name) {
			return false
		}
	}
	if stringAt(d, "paymentReason") != strings.ToLower(stringAt(body, "paymentReason")) {
		return false
	}
	for _, name := range []string{"notes", "paymentReference", "recipientEmail", "paymentReason"} {
		if v, exists := body[name]; exists {
			s, ok := v.(string)
			if !ok || len(s) > 4096 || strings.TrimSpace(s) != s || strings.ContainsRune(s, '\x00') {
				return false
			}
		}
	}
	if fee := stringAt(d, "railFeeAmountMinor"); fee != "" && !bankInteger.MatchString(fee) {
		return false
	}
	if d["v"] == float64(15) && d["seismicHolderAddressRequirementResolved"] != true {
		return false
	}
	parties, ok := d["fromTo"].(map[string]any)
	if !ok || !allowedBankFields(parties, "from", "to") {
		return false
	}
	from, ok := parties["from"].([]any)
	if !ok || len(from) != 1 {
		return false
	}
	if to, exists := parties["to"]; exists {
		list, ok := to.([]any)
		if !ok || len(list) != 0 {
			return false
		}
	}
	sender, ok := from[0].(map[string]any)
	if !ok || !allowedBankFields(sender, "partyType", "address", "instrumentId", "asset", "amount", "maxAmount") || sender["partyType"] != "PARTY_TYPE_ADDRESS" || sender["address"] != settlement["address"] || sender["asset"] != settlement["asset"] || sender["instrumentId"] != settlement["instrumentId"] {
		return false
	}
	amountField, rawAmount := "amount", stringAt(body, "usdAmountMinor")
	if exactOut {
		amountField, rawAmount = "maxAmount", stringAt(body, "maxUsdAmountMinor")
		if stringAt(sender, "amount") != "" {
			return false
		}
	} else if stringAt(sender, "maxAmount") != "" {
		return false
	}
	cents, _ := new(big.Int).SetString(rawAmount, 10)
	atomic := new(big.Int).Mul(cents, new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals-2)), nil))
	if sender[amountField] != atomic.String() {
		return false
	}
	return validFiatRecipient(d, body)
}

func validFiatRecipient(d, body document) bool {
	if !slices.Contains([]string{"ach", "fedwire", "swift", "sepa", "fps", "bacs", "npss", "cnaps", "fast", "meps", "chaps", "becs", "eft"}, stringAt(d, "rail")) {
		return false
	}
	for _, name := range []string{"destinationName", "accountHolderName", "accountHolderType"} {
		if stringAt(d, name) == "" {
			return false
		}
	}
	if stringAt(d, "accountNumber") == "" && stringAt(d, "iban") == "" {
		return false
	}
	recipient, ok := body["recipient"].(map[string]any)
	if !ok || len(recipient) != 1 {
		return false
	}
	if saved, ok := recipient["saved"].(map[string]any); ok {
		if !allowedBankFields(saved, "contactId", "contactBankAccountId", "rail", "accountHolderType", "firstName", "lastName", "naicsCode") || !validUUID(stringAt(saved, "contactId")) || !validUUID(stringAt(saved, "contactBankAccountId")) || saved["contactId"] != d["contactId"] || saved["contactBankAccountId"] != d["contactBankAccountId"] || fiatRail(stringAt(saved, "rail")) != d["rail"] {
			return false
		}
		for _, name := range []string{"accountHolderType", "firstName", "lastName", "naicsCode"} {
			if _, exists := saved[name]; exists && stringAt(saved, name) != stringAt(d, name) {
				return false
			}
		}
		return true
	}
	manual, ok := recipient["manual"].(map[string]any)
	if !ok || !allowedBankFields(manual, "rail", "destinationName", "accountNumber", "routingNumber", "accountType", "accountHolderName", "accountHolderType", "firstName", "lastName", "bankName", "destinationBankAddress", "accountHolderAddress", "accountHolderCountry", "naicsCode", "relationship", "partyType", "taxNumber", "iban", "bic", "sortCode", "cnapsCode", "idNumber", "phone", "intermediaryBic", "intermediaryRoutingNumber", "swiftPaymentCode", "ifsc", "bsb", "institutionNumber", "transitNumber", "vpa") || stringAt(d, "contactId") != "" || stringAt(d, "contactBankAccountId") != "" || fiatRail(stringAt(manual, "rail")) != d["rail"] {
		return false
	}
	for _, name := range []string{"destinationName", "accountNumber", "routingNumber", "accountType", "accountHolderName", "accountHolderType", "firstName", "lastName", "bankName", "naicsCode", "relationship", "partyType", "taxNumber", "iban", "bic", "sortCode", "cnapsCode", "idNumber", "phone", "intermediaryBic", "intermediaryRoutingNumber", "swiftPaymentCode", "ifsc", "bsb", "institutionNumber", "transitNumber", "vpa"} {
		if stringAt(manual, name) != stringAt(d, name) {
			return false
		}
	}
	for _, pair := range [][2]string{{"destinationBankAddress", "bankAddress"}, {"accountHolderAddress", "accountHolderAddress"}} {
		input, inputOK := manual[pair[0]].(map[string]any)
		signed, signedOK := d[pair[1]].(map[string]any)
		if !inputOK || !signedOK || !fiatAddressEqual(input, signed) {
			return false
		}
	}
	if country := stringAt(manual, "accountHolderCountry"); country != "" {
		holder := d["accountHolderAddress"].(map[string]any)
		if country != stringAt(holder, "country") {
			return false
		}
	}
	return true
}

func fiatRail(rail string) string {
	switch rail {
	case "ACH":
		return "ach"
	case "WIRE":
		return "fedwire"
	case "SWIFT", "SEPA", "FPS", "BACS", "NPSS", "CNAPS", "FAST", "MEPS", "CHAPS", "BECS", "EFT":
		return strings.ToLower(rail)
	default:
		return ""
	}
}

func fiatAddressEqual(a, b document) bool {
	fields := []string{"street1", "street2", "street3", "city", "region", "postalCode", "country"}
	if !allowedBankFields(a, fields...) || !allowedBankFields(b, fields...) {
		return false
	}
	for _, name := range fields {
		if stringAt(a, name) != stringAt(b, name) {
			return false
		}
	}
	return true
}
