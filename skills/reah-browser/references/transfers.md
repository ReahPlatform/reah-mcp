# Prepare a transfer

Use `/<entityId>/operations/transfer` through visible navigation. The old
`operations/send` and `operations/bulk-send` routes are not current entry points.
A recipient link or prefilled form helps locate a destination; it does not
authorize sending money.

## Follow the wizard

The reviewed steps are **Type → From → To → Amount → Review**.

1. Select the requested single/bulk mode from the available UI.
2. In From, verify the exact source account or wallet, asset/currency, and network
   where applicable. A similar balance or asset ticker is not account identity.
3. In To, match the requested contact and payment method or destination. Inspect
   bank rail and masked account, or the full crypto address and concrete network.
4. In Amount, use displayed major currency/asset units. Inspect which amount is
   fixed (source spend or destination receive), fees, minimums, gas, quote expiry,
   and actual execution/scheduling options. Do not silently substitute a network,
   rail, asset, or schedule to make the form pass validation.
5. In Review, compare the live source, destination, amount, fees, and timing to
   the user's instruction. A route may include conversion; inspect both assets
   and the resulting receive amount rather than assuming a same-asset transfer.

Wait for bank availability, provider requirements, quotes, and balance checks.
Do not force a disabled Review/submit button or acknowledge a risk on the user's
behalf merely to advance the form. Resolve missing business details explicitly.

For bulk transfers, inspect **every row** and its destination, amount, currency,
and network/rail. Check rejected rows and duplicates against the intended batch.
Validating a sample or accepting a CSV does not verify the batch.

## Hand off before commitment

Stop at the final review **before `Initiate Approval` or `Schedule Transfer`**.
The approval initialization path can execute immediately when policy permits;
a later signer or confirmation is not guaranteed. Give the user the prepared
business, source, destination, amount, fees/quote, and remaining button to click.
Do not submit the financial action through browser automation.

After the user completes it, observe the record if requested. Read
[approvals](approvals.md) for approval/execution state. For bulk operations,
inspect all row results and report pending or partial execution explicitly.
Use [outcomes and recovery](outcomes-and-recovery.md) if the submission result
is unclear; never recreate a transfer to recover from a UI timeout.
