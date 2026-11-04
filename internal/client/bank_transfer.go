package client

import (
	"encoding/json"
	"errors"
	"math/big"
	"regexp"
	"slices"
	"strings"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/dynamicpb"
)

var bankTransferFields = []string{"entityId", "quoteId", "requestId"}
var bankTransferContextFields = []string{"approvalDetailsJson", "expiresAt", "sourceBankingAccountId", "targetBankingAccountId", "fromAddress", "toAddress", "walletId", "networkId", "instrumentId", "asset", "decimals", "amountCents", "amountAtomic", "gasFeeCents", "totalDebitCents"}
var bankInteger = regexp.MustCompile(`^(0|[1-9][0-9]{0,77})$`)

func bankTransferSchema(schema document) bool {
	props, _ := schema["properties"].(map[string]any)
	required, _ := schema["required"].([]any)
	for _, name := range bankTransferFields {
		if !slices.Contains(required, any(name)) || props[name] == nil {
			return false
		}
	}
	for name := range props {
		if !slices.Contains(bankTransferFields, name) && !slices.Contains(signatureFields, name) {
			return false
		}
	}
	return true
}

func addBankTransferContext(schema document) {
	props := schema["properties"].(map[string]any)
	fields := document{}
	for _, name := range bankTransferContextFields {
		fields[name] = document{"type": "string", "description": "Exact frozen quote or independently verified Banking settlement metadata; do not infer missing values."}
	}
	fields["approvalDetailsJson"] = document{"type": "string", "maxLength": 32768, "description": "Exact approvalDetailsJson from the selected fresh internal Banking quote. Never supply a provider signing payload."}
	fields["walletId"] = document{"type": "string", "description": "Custody wallet ID from the frozen quote. Ledger independently verifies this binding against the source Banking account."}
	fields["asset"] = document{"type": "string", "description": "Independently verified local ERC20 settlement identity. This is not present in the existing signed quote and must never be inserted into it."}
	fields["decimals"] = document{"type": "integer", "minimum": 2, "maximum": 36}
	props["bankTransferContext"] = document{"type": "object", "additionalProperties": false, "properties": fields, "required": bankTransferContextFields}
	required, _ := schema["required"].([]any)
	schema["required"] = append(required, "bankTransferContext")
}

func bankTransferIntent(key DeviceKey, c SigningContext, args document, now time.Time) (document, document, error) {
	bad := errors.New("use the exact unexpired existing Send v1 Banking internal quote with independently verified metadata and Gas Station gas")
	if c.Binding.PolicyProfile != "financial-operations-v1" {
		return nil, nil, errors.New("Banking transfer requires a newly authorized financial-operations-v1 credential")
	}
	if !exactFields(args, append(append([]string{}, bankTransferFields...), "bankTransferContext")...) {
		return nil, nil, bad
	}
	for _, name := range bankTransferFields {
		if !validUUID(stringAt(args, name)) {
			return nil, nil, bad
		}
	}
	if args["entityId"] != c.Binding.EntityID {
		return nil, nil, bad
	}
	ctx, ok := args["bankTransferContext"].(map[string]any)
	if !ok || !exactFields(ctx, bankTransferContextFields...) {
		return nil, nil, bad
	}
	for _, name := range bankTransferContextFields {
		if name != "decimals" {
			if _, ok := ctx[name].(string); !ok {
				return nil, nil, bad
			}
		}
	}
	for _, name := range []string{"sourceBankingAccountId", "targetBankingAccountId", "walletId", "networkId", "instrumentId"} {
		if !validUUID(stringAt(ctx, name)) {
			return nil, nil, bad
		}
	}
	if ctx["sourceBankingAccountId"] == ctx["targetBankingAccountId"] {
		return nil, nil, bad
	}
	for _, name := range []string{"fromAddress", "toAddress"} {
		if !evmAddress.MatchString(stringAt(ctx, name)) || strings.EqualFold(stringAt(ctx, name), "0x0000000000000000000000000000000000000000") {
			return nil, nil, bad
		}
	}
	if strings.EqualFold(stringAt(ctx, "fromAddress"), stringAt(ctx, "toAddress")) {
		return nil, nil, bad
	}
	asset := stringAt(ctx, "asset")
	m := evmAsset.FindStringSubmatch(asset)
	if m == nil || !strings.HasPrefix(m[2], "erc20:") || convertAsset(asset, strings.TrimPrefix(m[2], "erc20:")) == "" {
		return nil, nil, bad
	}
	decimals, ok := contextDecimals(ctx, "decimals")
	if !ok || decimals < 2 {
		return nil, nil, bad
	}
	for _, name := range []string{"amountCents", "amountAtomic", "gasFeeCents", "totalDebitCents"} {
		if !bankInteger.MatchString(stringAt(ctx, name)) {
			return nil, nil, bad
		}
	}
	if stringAt(ctx, "amountCents") == "0" || stringAt(ctx, "amountAtomic") == "0" || ctx["amountCents"] != ctx["totalDebitCents"] {
		return nil, nil, bad
	}
	// The internal Banking quote contract is a 1:1 USD settlement transfer.
	// Check its two explicit unit representations without floating point or FX.
	cents, _ := new(big.Int).SetString(stringAt(ctx, "amountCents"), 10)
	scale := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals-2)), nil)
	if new(big.Int).Mul(cents, scale).String() != ctx["amountAtomic"] {
		return nil, nil, bad
	}
	expiry, e := time.Parse(time.RFC3339Nano, stringAt(ctx, "expiresAt"))
	if e != nil || !expiry.After(now) {
		return nil, nil, bad
	}
	raw := stringAt(ctx, "approvalDetailsJson")
	if len(raw) == 0 || len(raw) > 32768 || approvalFileError != nil {
		return nil, nil, bad
	}
	message := dynamicpb.NewMessage(approvalFile.Messages().ByName("ApprovalDetails"))
	// Strict protobuf JSON parsing rejects unknown fields and duplicate oneof/fields.
	if protojson.Unmarshal([]byte(raw), message) != nil {
		return nil, nil, bad
	}
	canonical, e := protojson.Marshal(message)
	var details document
	if e != nil || json.Unmarshal(canonical, &details) != nil || !exactFields(details, "sendTransaction") {
		return nil, nil, bad
	}
	send, ok := details["sendTransaction"].(map[string]any)
	if !ok || !allowedBankFields(send, "v", "walletId", "networkId", "instrumentId", "fromAddress", "toAddress", "amount", "notes", "gasSource", "gasUsdFee", "fromTo", "gas") || send["v"] != float64(1) {
		return nil, nil, bad
	}
	for _, name := range []string{"walletId", "networkId", "instrumentId", "fromAddress", "toAddress"} {
		if send[name] != ctx[name] {
			return nil, nil, bad
		}
	}
	if send["amount"] != ctx["amountAtomic"] || send["gasSource"] != "gas_station" || send["gasUsdFee"] != ctx["gasFeeCents"] {
		return nil, nil, bad
	}
	if notes := stringAt(send, "notes"); len(notes) > 4096 || strings.ContainsRune(notes, '\x00') || notes != strings.TrimSpace(notes) {
		return nil, nil, bad
	}
	gas, ok := send["gas"].(map[string]any)
	if !ok || !allowedBankFields(gas, "usd", "payerAccountType") || gas["usd"] != ctx["gasFeeCents"] || gas["payerAccountType"] != "gas_station" {
		return nil, nil, bad
	}
	parties, ok := send["fromTo"].(map[string]any)
	if !ok || !exactFields(parties, "from", "to") {
		return nil, nil, bad
	}
	for _, side := range []string{"from", "to"} {
		entries, ok := parties[side].([]any)
		if !ok || len(entries) != 1 {
			return nil, nil, bad
		}
		party, ok := entries[0].(map[string]any)
		if !ok || !exactFields(party, "partyType", "bankingId", "address", "instrumentId", "amount", "estimatedUsd") {
			return nil, nil, bad
		}
		id, address := ctx["sourceBankingAccountId"], ctx["fromAddress"]
		if side == "to" {
			id, address = ctx["targetBankingAccountId"], ctx["toAddress"]
		}
		if party["partyType"] != "PARTY_TYPE_BANKING" || party["bankingId"] != id || party["address"] != address || party["instrumentId"] != ctx["instrumentId"] || party["amount"] != ctx["amountAtomic"] || party["estimatedUsd"] != ctx["amountCents"] {
			return nil, nil, bad
		}
	}
	intent, e := approvalIntent(key, c, details, stringAt(args, "requestId"), now)
	return intent, document{"entityId": args["entityId"], "quoteId": args["quoteId"], "requestId": args["requestId"]}, e
}

func allowedBankFields(d document, fields ...string) bool {
	for name := range d {
		if !slices.Contains(fields, name) {
			return false
		}
	}
	return true
}
