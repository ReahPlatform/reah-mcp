# Cards

Use `/<entityId>/cards`. Card detail opens from the list, commonly with
`tab=cards&card=<id>` in the URL; do not invent a `/cards/<id>` detail route.
Match cardholder, nickname, and masked card reference before changing anything.
Do not reveal PAN/CVV or copy full card details when the task needs only management.

## Freeze, unfreeze, or change nickname

- Inspect the current state first. Freeze/unfreeze switches between active and
  locked states; the UI can offer `Freeze` or `Unfreeze`. If the card already has
  the requested state, report that instead of blindly toggling it.
- Use the requested card's action or nickname editor. Follow any signing or
  approval prompt through normal UI; let the user complete signing.
- Verify the resulting state or nickname on that exact card. A requested status
  change or pending approval is not proof that the card is frozen/unfrozen.

## Spending limit

The editor includes `Limit` and `Spending interval`. Verify both; changing an
interval changes the effect of the same numeric limit. Enter the displayed USD
amount with the accepted two-decimal precision: a $50 limit is `50.00`, not
`5000`. The UI handles its own conversion to minor units.

Save the specifically requested limit change within existing authorization.
If an approval is created, load [approvals](approvals.md). Verify the applied
limit and interval rather than claiming completion from request submission.

## Prepare issuance or funding

The onboarding entry is `/cards/create`; the actual form is `/cards/create-form`
under the business route. It can ask for nickname, cardholder, names, card type,
spending interval, and limit. Do not invent cardholder identity or complete KYC
attestations for the user; `/cards/holder-kyc` is a user handoff when needed.

Inspect issuance fees before submission. A paid card can open an issuance review
showing the source, fee, balance after charge, cardholder, type, and limit, with
`Continue to approval`. Stop before that button or any submission that can charge
an issuance fee. A separate review is conditional, so do not assume every form
submission is safe because another confirmation will appear.

For a fee-free issuance, inspect the actual live flow and authorization before
submitting; do not assume a fee-free card based on the card name alone. If the
charge or effect cannot be established, leave the prepared form for the user.

Card funding moves money. Use [conversions](conversions.md) for bank-to-card
funding, or the applicable live funding review, and hand the commitment to the
user before submission. Verify issuance, funding, and card activation as separate
outcomes. Load [outcomes and recovery](outcomes-and-recovery.md) if interrupted.
