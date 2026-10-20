# Prepare treasury actions

Use `/<entityId>/treasury` and open the intended product/position. A reviewed
product route is `/<entityId>/treasury/product/<productId>`; `view=position` can
select the position view. Match the actual product, asset, network, and position,
not just its APY or display name.

## Deposit

1. Open the deposit panel and choose the requested source bank account or wallet.
2. Verify the deposit asset and destination product. A funding route can convert
   the source asset to the vault token; inspect spend, receive, quote, and fees.
3. Enter displayed asset/currency units and inspect minimums, available balance,
   gas source/balance, and quote readiness. Do not infer spendable funds from a
   portfolio valuation or a pending transaction.
4. Review the actual amount, accounts, product, conversion, fees, and timing.
   Current product APY does not guarantee a realized or future return.

## Withdraw

1. Open the withdrawal panel from the intended position. Inspect `To`, `Asset`,
   and `Withdraw amount`, then use `Review withdrawal` to inspect the summary.
2. Verify receiving wallet/network, available withdrawal amount, minimums,
   expected receive amount, fees, and any conversion or delay shown by the UI.
3. One visible product can route withdrawals through multiple underlying vaults.
   Respect per-vault minimums and availability checks. Do not split or change a
   requested amount to work around a disabled withdrawal without user direction.
4. If the UI cannot establish a minimum or valid route, stop at that condition;
   do not force submission using aggregate position value.

## Financial handoff and results

Stop before the confirmation's `Initiate approval` or another committing
submission. Approval creation may immediately execute the request. The user
completes commitment and signing; a review screen is the agent's endpoint.

After user submission, the action can create **multiple approval IDs** for
underlying vaults. Inspect all returned requests in Approvals. Opening the first
drawer does not establish that the whole product withdrawal completed.
If the request succeeds but opening its drawer fails, do not submit again:
locate the existing approval records instead.

Use [approvals](approvals.md) for state interpretation and
[outcomes and recovery](outcomes-and-recovery.md) for partial execution or an
uncertain result. Report amounts and outstanding requests separately; do not
describe a partially settled position action as fully withdrawn or deposited.
