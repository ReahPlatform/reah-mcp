package client

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/dynamicpb"
)

func isNewFinancialProtocol(protocol string) bool {
	return protocol == "approval-smart-add-v1" || protocol == "approval-smart-withdraw-v1" || protocol == "approval-usd-withdraw-v1"
}
func newFinancialFields(protocol string) []string {
	if protocol == "approval-usd-withdraw-v1" {
		return []string{"entityId", "bankingAccountId", "amountCents", "rail", "recipient", "notes"}
	}
	return []string{"entityId", "walletId", "vaultId", "instrumentAccountId", "assetAmount", "gasSource", "notes"}
}
func newFinancialSchema(schema document, protocol string) bool {
	props, _ := schema["properties"].(map[string]any)
	required, _ := schema["required"].([]any)
	for _, name := range newFinancialFields(protocol) {
		if name != "notes" && (props[name] == nil || !slices.Contains(required, any(name))) {
			return false
		}
	}
	for name := range props {
		if !slices.Contains(newFinancialFields(protocol), name) && !slices.Contains(signatureFields, name) {
			return false
		}
	}
	return true
}
func addFinancialContext(schema document, protocol string) {
	props := schema["properties"].(map[string]any)
	fields := document{"requestId": document{"type": "string", "format": "uuid"}, "approvalDetailsJson": document{"type": "string", "maxLength": 32768, "description": "Exact Smart Balance quote account hierarchy, or complete resolved USD recipient intent from verified account/contact metadata. Never a provider payload."}}
	if protocol == "approval-usd-withdraw-v1" {
		fields["settlementAddress"] = document{"type": "string", "description": "Exact Banking settlement destination from reah_get_bank_account_metadata; never infer the source signer."}
		fields["targetInstrumentId"] = document{"type": "string", "format": "uuid"}
	}
	required := []string{"requestId", "approvalDetailsJson"}
	if protocol == "approval-usd-withdraw-v1" {
		required = append(required, "settlementAddress", "targetInstrumentId")
	}
	props["financialContext"] = document{"type": "object", "additionalProperties": false, "properties": fields, "required": required}
	schema["required"] = append(schema["required"].([]any), "financialContext")
}
func newFinancialIntent(key DeviceKey, c SigningContext, args document, protocol string, now time.Time) (document, document, error) {
	bad := errors.New("use complete exact business terms and verified original account/recipient context with a stable requestId")
	if c.Binding.PolicyProfile != "financial-operations-v1" || args["entityId"] != c.Binding.EntityID || approvalFileError != nil {
		return nil, nil, bad
	}
	ctx, ok := args["financialContext"].(map[string]any)
	if !ok || !validUUID(stringAt(ctx, "requestId")) {
		return nil, nil, bad
	}
	allowedContext := []string{"requestId", "approvalDetailsJson"}
	if protocol == "approval-usd-withdraw-v1" {
		allowedContext = append(allowedContext, "settlementAddress", "targetInstrumentId")
	}
	if !exactFields(ctx, allowedContext...) {
		return nil, nil, bad
	}
	body := cloneDocument(args)
	delete(body, "financialContext")
	for name := range body {
		if !slices.Contains(newFinancialFields(protocol), name) {
			return nil, nil, bad
		}
	}
	raw := stringAt(ctx, "approvalDetailsJson")
	if len(raw) == 0 || len(raw) > 32768 {
		return nil, nil, bad
	}
	message := dynamicpb.NewMessage(approvalFile.Messages().ByName("ApprovalDetails"))
	if protojson.Unmarshal([]byte(raw), message) != nil {
		return nil, nil, bad
	}
	canonical, err := protojson.Marshal(message)
	var details document
	if err != nil || json.Unmarshal(canonical, &details) != nil {
		return nil, nil, bad
	}
	kind := "addSmartBalance"
	if protocol == "approval-smart-withdraw-v1" {
		kind = "withdrawSmartBalance"
	}
	if protocol == "approval-usd-withdraw-v1" {
		kind = "bankingUsdWithdrawal"
	}
	d, ok := details[kind].(map[string]any)
	if !ok || !exactFields(details, kind) {
		return nil, nil, bad
	}
	if protocol == "approval-usd-withdraw-v1" {
		if !validUSDIntentTerms(d, body, ctx) {
			return nil, nil, bad
		}
	} else {
		if !allowedBankFields(d, "v", "entityId", "walletId", "vaultId", "networkId", "instrumentId", "instrumentAccountId", "assetAmount", "gasSource", "treasuryAccountId", "treasuryBlockchainAccountId", "treasuryPositionAccountId", "notes") || d["v"] != float64(1) {
			return nil, nil, bad
		}
		for _, name := range []string{"entityId", "walletId", "vaultId", "networkId", "instrumentId", "instrumentAccountId", "treasuryAccountId", "treasuryBlockchainAccountId", "treasuryPositionAccountId"} {
			if !validUUID(stringAt(d, name)) {
				return nil, nil, bad
			}
		}
		for _, name := range []string{"entityId", "walletId", "vaultId", "instrumentAccountId", "assetAmount"} {
			if d[name] != body[name] {
				return nil, nil, bad
			}
		}
		if !positiveDecimal(stringAt(body, "assetAmount"), 36) {
			return nil, nil, bad
		}
		gas := "from_wallet"
		if body["gasSource"] == "GAS_SOURCE_GAS_STATION" {
			gas = "gas_station"
		} else if body["gasSource"] != "GAS_SOURCE_FROM_WALLET" {
			return nil, nil, bad
		}
		if d["gasSource"] != gas {
			return nil, nil, bad
		}
	}
	if value, exists := body["notes"]; exists {
		if _, ok := value.(string); !ok {
			return nil, nil, bad
		}
	}
	notes := stringAt(body, "notes")
	if len(notes) > 4096 || notes != strings.TrimSpace(notes) || strings.ContainsRune(notes, '\x00') {
		return nil, nil, bad
	}
	if notes != "" {
		d["notes"] = notes
	} else {
		delete(d, "notes")
	}
	intent, err := approvalIntent(key, c, details, stringAt(ctx, "requestId"), now)
	return intent, body, err
}
