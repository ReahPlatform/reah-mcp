package client

import (
	"encoding/json"
	"errors"
	"math/big"
	"regexp"
	"slices"
	"strings"
	"time"
)

// The financial protocol contains several intent families. Select the supported
// shape from the authenticated schema, never from a compiled business-tool name.
var convertRequired = []string{"entityId", "originInstrumentAccountId", "destinationInstrumentId", "amount", "executionConstraint", "gasSource", "amountMode"}
var convertFields = append(append([]string{}, convertRequired...), "destinationAddress", "notes", "quoteSource", "requestedTargetAmount", "initialQuoteId", "balanceSource", "smartBalanceVaultId", "pointsPreference", "pointsQuoteToken", "idempotencyKey")
var convertContextFields = []string{"requestId", "originNetworkId", "destinationNetworkId", "sourceAsset", "destinationAsset", "originTokenAddress", "destinationTokenAddress", "originAddress", "sourceDecimals", "destinationDecimals"}

func convertSchema(schema document) bool {
	props, _ := schema["properties"].(map[string]any)
	required, _ := schema["required"].([]any)
	for _, name := range convertRequired {
		if !slices.Contains(required, any(name)) || props[name] == nil {
			return false
		}
	}
	for name := range props {
		if !slices.Contains(convertFields, name) && !slices.Contains(signatureFields, name) {
			return false
		}
	}
	return true
}

func addConvertContext(schema document) {
	props := schema["properties"].(map[string]any)
	contextProps := document{}
	for _, name := range convertContextFields {
		contextProps[name] = document{"type": "string", "description": "Exact independently verified source/destination context; never infer an asset from a missing contract or an address."}
	}
	for _, name := range []string{"sourceDecimals", "destinationDecimals"} {
		contextProps[name] = document{"type": "integer", "minimum": 0, "maximum": 36}
	}
	contextProps["requestId"] = document{"type": "string", "format": "uuid", "description": "Stable UUID for this intended conversion; retain it and reconcile the original approval after an unknown outcome."}
	props["convertContext"] = document{"type": "object", "additionalProperties": false, "properties": contextProps, "required": convertContextFields}
	props["amountMode"] = document{"type": "string", "enum": []string{"EXACT_INPUT", "MINIMUM_OUTPUT"}}
	if props["pointsPreference"] != nil {
		props["pointsPreference"] = document{"type": "string", "enum": []string{"NONE", "MAX"}}
	}
	required, _ := schema["required"].([]any)
	for _, name := range []string{"convertContext", "destinationAddress"} {
		if !slices.Contains(required, any(name)) {
			required = append(required, name)
		}
	}
	schema["required"] = required
}

var plainDecimal = regexp.MustCompile(`^(0|[1-9][0-9]{0,77})(\.[0-9]{1,36})?$`)
var evmAddress = regexp.MustCompile(`^0x[0-9a-fA-F]{40}$`)
var evmAsset = regexp.MustCompile(`^eip155:([1-9][0-9]{0,19})/(slip44:[0-9]{1,10}|erc20:0x[0-9a-fA-F]{40})$`)

func positiveDecimal(value string, decimals int) bool {
	if !plainDecimal.MatchString(value) {
		return false
	}
	parts := strings.Split(value, ".")
	if len(parts) == 2 && len(parts[1]) > decimals {
		return false
	}
	n, ok := new(big.Rat).SetString(value)
	return ok && n.Sign() > 0
}

func convertAsset(asset, token string) string {
	m := evmAsset.FindStringSubmatch(asset)
	if m == nil {
		return ""
	}
	if strings.HasPrefix(m[2], "slip44:") {
		if token != "" {
			return ""
		}
	} else if !evmAddress.MatchString(token) || !strings.EqualFold(strings.TrimPrefix(m[2], "erc20:"), token) || strings.EqualFold(token, "0x0000000000000000000000000000000000000000") {
		return ""
	}
	return m[1]
}

func contextDecimals(d document, name string) (int, bool) {
	b, e := json.Marshal(d[name])
	var n int
	if d[name] == nil || e != nil || json.Unmarshal(b, &n) != nil || n < 0 || n > 36 {
		return 0, false
	}
	return n, true
}

func convertIntent(key DeviceKey, c SigningContext, args document, now time.Time) (document, document, error) {
	return convertFundingIntent(key, c, args, now, "", false)
}

// Banking V2 uses the same v4 constraint intent as Wallet converts. Only the
// Banking adapter can admit same-asset funding or a Banking source identifier.
func convertFundingIntent(key DeviceKey, c SigningContext, args document, now time.Time, bankingID string, sameAsset bool) (document, document, error) {
	bad := errors.New("use complete explicit protected conversion terms, exact-input or minimum-output, and a positive destination floor; signing context is never a provider payload")
	if c.Binding.PolicyProfile != "financial-operations-v1" {
		return nil, nil, errors.New("conversion requires a newly authorized financial-operations-v1 credential; a wallet-transfer credential cannot be upgraded locally")
	}
	context, ok := args["convertContext"].(map[string]any)
	if !ok || !exactFields(context, convertContextFields...) {
		return nil, nil, bad
	}
	for _, name := range convertContextFields {
		if name != "sourceDecimals" && name != "destinationDecimals" {
			if _, ok := context[name].(string); !ok {
				return nil, nil, bad
			}
		}
	}
	body := cloneDocument(args)
	delete(body, "convertContext")
	for name, value := range body {
		if !slices.Contains(convertFields, name) {
			return nil, nil, bad
		}
		if name != "executionConstraint" {
			if s, ok := value.(string); !ok || len(s) > 4096 || strings.ContainsRune(s, '\x00') {
				return nil, nil, bad
			}
		}
	}
	ids := []string{"entityId", "destinationInstrumentId"}
	if bankingID == "" {
		ids = append(ids, "originInstrumentAccountId")
	} else if !validUUID(bankingID) || stringAt(body, "originInstrumentAccountId") != "" {
		return nil, nil, bad
	}
	for _, name := range ids {
		if !validUUID(stringAt(body, name)) {
			return nil, nil, bad
		}
	}
	for _, name := range []string{"requestId", "originNetworkId", "destinationNetworkId"} {
		if !validUUID(stringAt(context, name)) {
			return nil, nil, bad
		}
	}
	if body["entityId"] != c.Binding.EntityID {
		return nil, nil, bad
	}
	constraint, ok := body["executionConstraint"].(map[string]any)
	if !ok || !exactFields(constraint, "percentage", "minimumReceive") {
		return nil, nil, bad
	}
	percentage, floor := stringAt(constraint, "percentage"), stringAt(constraint, "minimumReceive")
	p, valid := new(big.Rat).SetString(percentage)
	if !plainDecimal.MatchString(percentage) || strings.Contains(percentage, ".") && len(strings.SplitN(percentage, ".", 2)[1]) > 2 || !valid || p.Sign() < 0 || p.Cmp(big.NewRat(100, 1)) > 0 {
		return nil, nil, bad
	}
	sourceDecimals, sourceOK := contextDecimals(context, "sourceDecimals")
	destinationDecimals, destinationOK := contextDecimals(context, "destinationDecimals")
	if !sourceOK || !destinationOK || !positiveDecimal(stringAt(body, "amount"), sourceDecimals) || !positiveDecimal(floor, destinationDecimals) {
		return nil, nil, bad
	}
	mode := stringAt(body, "amountMode")
	if mode != "EXACT_INPUT" && mode != "MINIMUM_OUTPUT" || mode == "EXACT_INPUT" && stringAt(body, "requestedTargetAmount") != "" || mode == "MINIMUM_OUTPUT" && stringAt(body, "requestedTargetAmount") != floor {
		return nil, nil, bad
	}
	source, vault := stringAt(body, "balanceSource"), stringAt(body, "smartBalanceVaultId")
	if source == "" {
		source = "WALLET"
	}
	if source != "WALLET" && source != "SMART_BALANCE" || source == "WALLET" && vault != "" || source == "SMART_BALANCE" && !validUUID(vault) || !validPointsSelection(body) {
		return nil, nil, bad
	}
	gas := "from_wallet"
	if body["gasSource"] == "GAS_SOURCE_GAS_STATION" {
		gas = "gas_station"
	} else if body["gasSource"] != "GAS_SOURCE_FROM_WALLET" {
		return nil, nil, bad
	}
	sourceChain := protectedConvertChain(stringAt(context, "sourceAsset"), stringAt(context, "originTokenAddress"))
	destinationChain := protectedConvertChain(stringAt(context, "destinationAsset"), stringAt(context, "destinationTokenAddress"))
	if sourceChain == "" || destinationChain == "" || !sameAsset && strings.EqualFold(stringAt(context, "sourceAsset"), stringAt(context, "destinationAsset")) || (sourceChain == destinationChain) != (context["originNetworkId"] == context["destinationNetworkId"]) {
		return nil, nil, bad
	}
	if source == "SMART_BALANCE" && (mode != "EXACT_INPUT" || (sourceChain != "eip155:1" && sourceChain != "eip155:8453") || destinationChain != "eip155:4217" || stringAt(context, "originTokenAddress") == "" || stringAt(context, "destinationTokenAddress") == "") {
		return nil, nil, errors.New("Smart Balance conversion requires exact-input ERC-20 funding from Ethereum/Base to Tempo; verify the owned synchronous ERC-4626/allocator route before quoting")
	}
	if !validProtectedConvertAddress(sourceChain, stringAt(context, "originAddress")) || !validProtectedConvertAddress(destinationChain, stringAt(body, "destinationAddress")) || sourceChain == convertBitcoinChain && gas != "from_wallet" {
		return nil, nil, bad
	}
	notes := stringAt(body, "notes")
	if len(notes) > 4096 || strings.ContainsRune(notes, '\x00') || notes != strings.TrimSpace(notes) {
		return nil, nil, bad
	}
	d := document{"v": 4, "balanceSource": "BALANCE_SOURCE_" + source, "amountMode": strings.ToLower(mode), "gasSource": gas, "executionConstraint": constraint}
	if bankingID != "" {
		d["bankingAccountId"] = bankingID
	}
	for _, name := range []string{"originInstrumentAccountId", "destinationInstrumentId", "amount", "destinationAddress", "requestedTargetAmount", "notes", "smartBalanceVaultId"} {
		if value, ok := body[name]; ok {
			d[name] = value
		}
	}
	for _, name := range []string{"originNetworkId", "destinationNetworkId", "sourceAsset", "destinationAsset", "originTokenAddress", "destinationTokenAddress", "originAddress"} {
		d[name] = context[name]
	}
	intent, e := approvalIntent(key, c, document{"cryptoCryptoConvert": d}, stringAt(context, "requestId"), now)
	return intent, body, e
}

// Points authorization remains in the existing server quote/reservation flow.
// The v4 transaction intent signs financial constraints; the operation proof
// separately binds this exact preference, original quote token and idempotency.
func validPointsSelection(body document) bool {
	preference, token := stringAt(body, "pointsPreference"), stringAt(body, "pointsQuoteToken")
	if preference == "" || preference == "NONE" {
		return token == ""
	}
	return preference == "MAX" && token != "" && len(token) <= 4096 &&
		!strings.ContainsAny(token, "\x00\r\n") && validUUID(stringAt(body, "idempotencyKey"))
}
