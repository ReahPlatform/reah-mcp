package client

import (
	"errors"
	"math/big"
	"slices"
	"strings"
	"time"
)

var bankCryptoCommon = []string{"entityId", "bankingAccountId", "flowVersion", "executionConstraint", "gasSource", "quoteSource", "notes", "pointsPreference", "pointsQuoteToken", "idempotencyKey"}

func bankCryptoFields(withdraw bool) []string {
	fields := append([]string{}, bankCryptoCommon...)
	if withdraw {
		return append(fields, "amountCents", "targetInstrumentAccountId", "targetInstrumentId", "walletId")
	}
	return append(fields, "originInstrumentAccountId", "amount", "balanceSource", "smartBalanceVaultId")
}

func bankCryptoSchema(schema document) bool {
	props, _ := schema["properties"].(map[string]any)
	required, _ := schema["required"].([]any)
	withdraw := props["amountCents"] != nil
	want := []string{"entityId", "bankingAccountId", "flowVersion", "executionConstraint", "gasSource"}
	if withdraw {
		want = append(want, "amountCents")
	} else {
		want = append(want, "originInstrumentAccountId", "amount")
	}
	for _, name := range want {
		if props[name] == nil || !slices.Contains(required, any(name)) {
			return false
		}
	}
	for name := range props {
		if !slices.Contains(bankCryptoFields(withdraw), name) && !slices.Contains(signatureFields, name) {
			return false
		}
	}
	return true
}

func bankCryptoContextFields(withdraw bool) []string {
	fields := append(append([]string{}, convertContextFields...), "destinationInstrumentId", "destinationAddress")
	if withdraw {
		fields = append(fields, "totalBankingDebitUsdCents")
	}
	return fields
}

func addBankCryptoContext(schema document) {
	props := schema["properties"].(map[string]any)
	withdraw := props["amountCents"] != nil
	fields := document{}
	for _, field := range bankCryptoContextFields(withdraw) {
		fields[field] = document{"type": "string", "description": "Exact independently verified Banking source/target metadata and selected quote billing. Never use a provider signing payload or infer a settlement address."}
	}
	for _, field := range []string{"sourceDecimals", "destinationDecimals"} {
		fields[field] = document{"type": "integer", "minimum": 0, "maximum": 36}
	}
	props["bankCryptoContext"] = document{"type": "object", "additionalProperties": false,
		"properties": fields, "required": bankCryptoContextFields(withdraw)}
	props["flowVersion"] = document{"type": "string", "enum": []string{"V2"}}
	schema["required"] = append(schema["required"].([]any), "bankCryptoContext")
}

func bankCryptoIntent(key DeviceKey, c SigningContext, args document, now time.Time) (document, document, error) {
	bad := errors.New("use explicit V2 Banking crypto terms, verified source/target identities and exact quote billing; preserve the original request and Points quote")
	ctx, ok := args["bankCryptoContext"].(map[string]any)
	withdraw := args["amountCents"] != nil
	if !ok || !exactFields(ctx, bankCryptoContextFields(withdraw)...) ||
		!validUUID(stringAt(args, "bankingAccountId")) || args["flowVersion"] != "V2" || stringAt(args, "balanceSource") == "SMART_BALANCE" {
		return nil, nil, bad
	}
	body := cloneDocument(args)
	delete(body, "bankCryptoContext")
	for name, value := range body {
		if !slices.Contains(bankCryptoFields(withdraw), name) {
			return nil, nil, bad
		}
		if name != "executionConstraint" {
			s, valid := value.(string)
			if !valid || len(s) > 4096 || strings.ContainsRune(s, '\x00') {
				return nil, nil, bad
			}
		}
	}
	mapped := document{}
	for _, field := range []string{"entityId", "amount", "originInstrumentAccountId", "executionConstraint", "gasSource", "notes", "balanceSource", "smartBalanceVaultId", "pointsPreference", "pointsQuoteToken", "idempotencyKey", "quoteSource"} {
		if value, exists := body[field]; exists {
			mapped[field] = value
		}
	}
	convertCtx := document{}
	for _, field := range convertContextFields {
		convertCtx[field] = ctx[field]
	}
	mapped["convertContext"], mapped["amountMode"] = convertCtx, "EXACT_INPUT"
	mapped["destinationInstrumentId"], mapped["destinationAddress"] = ctx["destinationInstrumentId"], ctx["destinationAddress"]
	bankingID := ""
	if withdraw {
		account := stringAt(body, "targetInstrumentAccountId")
		instrument, wallet := stringAt(body, "targetInstrumentId"), stringAt(body, "walletId")
		if account != "" {
			if !validUUID(account) || instrument != "" || wallet != "" {
				return nil, nil, bad
			}
		} else if !validUUID(wallet) || !validUUID(instrument) || instrument != stringAt(ctx, "destinationInstrumentId") {
			return nil, nil, bad
		}
		principal, pOK := new(big.Int).SetString(stringAt(body, "amountCents"), 10)
		debit, dOK := new(big.Int).SetString(stringAt(ctx, "totalBankingDebitUsdCents"), 10)
		if !pOK || !dOK || !bankInteger.MatchString(stringAt(body, "amountCents")) ||
			!bankInteger.MatchString(stringAt(ctx, "totalBankingDebitUsdCents")) ||
			principal.Sign() <= 0 || debit.Cmp(principal) < 0 {
			return nil, nil, bad
		}
		payer := stringAt(body, "gasSource")
		if payer != "GAS_SOURCE_FROM_BANKING" && payer != "GAS_SOURCE_GAS_STATION" ||
			payer == "GAS_SOURCE_GAS_STATION" && debit.Cmp(principal) != 0 {
			return nil, nil, bad
		}
		// Existing Web withdraw intents bind the complete Banking USD budget,
		// while the source send leg always uses Gas Station bookkeeping.
		mapped["amount"] = new(big.Rat).SetFrac(debit, big.NewInt(100)).FloatString(2)
		mapped["gasSource"] = "GAS_SOURCE_GAS_STATION"
		bankingID = stringAt(body, "bankingAccountId")
	}
	intent, _, err := convertFundingIntent(key, c, mapped, now, bankingID, true)
	return intent, body, err
}
