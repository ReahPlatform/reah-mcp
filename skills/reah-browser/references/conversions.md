# Prepare a conversion

Open `/<entityId>/operations/convert` through the website. The steps are
**From → To → Amount → Review**; this wizard does not include Transfer's Type step.

## Choose supported accounts

The reviewed account combinations are wallet-to-wallet, wallet-to-bank,
bank-to-wallet, and bank-to-card. Bank-to-bank and wallet-to-card are not
supported conversion combinations; a card is not a source account here. Respect
the live capability controls rather than routing around a disabled combination.

1. Verify the actual source and destination account identities, assets, and
   networks. Different routes have different quote and scheduling capabilities.
2. Enter the requested amount in displayed units and inspect whether the source
   spend or destination receive amount is fixed. Do not infer exchange rates
   from MCP balances or assume a rate remains valid across navigation.
3. Inspect the live rate, receive/spend amounts, fees, gas source, available
   balance, price protection/slippage, and timing. Bank-to-card funding does not
   necessarily use the same quote flow as a cross-asset conversion.
4. Verify the mode actually selected. A requested zero-fee or scheduled option
   can be unavailable and the UI can fall back to standard execution. Do not
   report the originally requested mode as the prepared result without checking.
5. On Review, refresh an expired quote. If `The rate updated while you reviewed`
   appears, inspect the changed amounts and terms before handing off the review.

## Hand off

Stop before `Initiate Approval` or any scheduling/submission that can commit the
conversion or funding. The UI can describe approval as preceding commitment,
but the approval initialization code permits immediate execution. Do not rely
on another confirmation or signature prompt appearing later.

Present the prepared account pair, assets, spend/receive amounts, actual fees
and timing, and any expiring quote. The user completes financial commitment and
signing. If terms change before they submit, review the new terms rather than
treating the previous review as current.

For observed results after user submission, load [approvals](approvals.md).
For an unclear or partial outcome, load
[outcomes and recovery](outcomes-and-recovery.md).
