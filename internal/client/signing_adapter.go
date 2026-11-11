package client

import "time"

// Each adapter declares discovery and signing together. Business validation
// remains local and versioned; server metadata cannot install a new signer.
// All adapters share approvalIntent, operationProof and the execution loop.
type signingAdapter struct {
	schema      func(document) bool
	context     func(document)
	sign        func(DeviceKey, SigningContext, document, time.Time) (document, document, error)
	description string
}

func localSigningAdapter(format string, bankTransfer bool) (signingAdapter, bool) {
	switch format {
	case "approval-send-v2":
		return signingAdapter{sign: func(key DeviceKey, c SigningContext, args document, now time.Time) (document, document, error) {
			intent, err := transferIntent(key, c, args, now)
			return intent, cloneDocument(args), err
		}}, true
	case fiatWithdrawalProtocol:
		return signingAdapter{schema: fiatWithdrawalSchema, context: addFiatWithdrawalContext,
			sign: fiatWithdrawalIntent, description: " Use input and financialContext from reah_prepare_bank_fiat_withdraw unchanged. This local adapter supports bank-funded USD exact-input ACH/Wire without Points, documents, direct wallet funding or contact updates. Gateway resolves the actual payout route and complete recipient; the client verifies and signs these exact terms without selecting providers or recomputing fees. The gross USD debit already includes applicable quoted fees; never add the rail fee again. A fresh provider quote after approval must honor the signed receive floor. Approval creation does not execute or prove bank settlement. Preserve requestId and reconcile unknown outcomes."}, true
	case "approval-smart-add-v1", "approval-smart-withdraw-v1", "approval-usd-withdraw-v1":
		return signingAdapter{
			schema:  func(schema document) bool { return newFinancialSchema(schema, format) },
			context: func(schema document) { addFinancialContext(schema, format) },
			sign: func(key DeviceKey, c SigningContext, args document, now time.Time) (document, document, error) {
				return newFinancialIntent(key, c, args, format, now)
			}, description: " Local signing requires a financial-operations-v1 credential, a stable requestId, exact owned account context and explicit terms. Approval creation never approves or proves execution or settlement. Unknown outcomes require reconciliation of the original approval, without automatic replacement."}, true
	case "approval-financial-v1":
		if bankTransfer {
			return signingAdapter{schema: bankTransferSchema, context: addBankTransferContext,
				sign: bankTransferIntent, description: " Local adapter supports existing Banking internal Send v1 quotes paid by Gas Station. Supply bankTransferContext from the exact fresh quote and independently verified owned ERC20 Banking settlement metadata. The signed quote retains its existing UUID identities without an asset field; local metadata is not added to the signature. Preserve requestId and frozen approvalDetailsJson. Requires explicit financial-operations-v1 registration. Check actual available Banking funds and Gas Station balance separately. Bank internal execution requires a compatible Gateway advertising that business and Hodor acknowledging its private exact-request binding. This Web-equivalent bank route trusts Ledger transaction semantics; wallet and convert construction checks remain separate. Approval creation never approves, executes or proves settlement."}, true
		}
		return signingAdapter{schema: convertSchema, context: addConvertContext,
			sign: convertIntent, description: " Local adapter supports EVM Wallet crypto conversion (same-chain swap or cross-chain convert), exact-input and minimum-output, without Points. Smart Balance supports exact-input ERC-20 funding from Ethereum/Base to Tempo only, with an owned synchronous ERC-4626/allocator vault; no Smart same-chain swap, minimum-output or exact-output. Supply convertContext from independently verified account/network/instrument reads, explicit destinationAddress, authoritative asset decimals and CAIP-19 identities. Context is local-only and is bound into a v4 approval intent. Preserve its stable requestId. For SMART_BALANCE retain the exact owned smartBalanceVaultId, require redeemEnabled and check smartBalance for principal, with Wallet/Gas Station gas funded separately. Obtain a fresh quote and check principal plus selected gas balance before submitting; use a positive minimumReceive at destination precision. This requires explicit financial-operations-v1 registration; wallet-only credentials cannot convert."}, true
	}
	return signingAdapter{}, false
}
