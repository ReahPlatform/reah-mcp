# Invoice editor

## Open the correct invoice

Use Invoicing at `/<entityId>/invoicing`. The current editor is
`/<entityId>/invoicing/edit/<invoiceId>`; a new invoice uses `edit/new`.
The older `invoicing/create` route redirects there. Invoice details normally
open from the list in a drawer. Match business, invoice number, customer, and
status before editing. Paid invoices cannot be edited through this flow.

## Complete the five steps

| Step | What to inspect |
| --- | --- |
| Your Company | Company name, valid email, and address |
| Details | Customer name, valid email, billing address, issue date, and one-off due date or recurring payment terms |
| Line Items | At least one item with description, positive quantity and price, and currency; requested tax/discount adjustments |
| Payment | At least one configured payment method: bank account with rail, crypto receiving wallet, or custom payment instructions |
| Notes | Requested notes and the final preview |

The UI validates current and intervening steps when navigating. Resolve the
actual validation rather than skipping ahead or changing unrelated fields.
Percentage adjustments cannot exceed 100 in the reviewed validation. Use the
user's intended tax/discount basis and inspect the resulting totals.

Enter amounts in the currency units displayed by the editor. Do not paste raw
minor-unit API values into a price field or convert normalized MCP values a
second time. Preserve the UI's supported precision. Inspect line currencies
and totals rather than inventing a common currency for mixed-currency items.

For recurring invoices, verify interval, payment terms, and end conditions in
the rendered editor. Do not choose a recurrence or end date merely because it
is preselected. AI-generated form content also needs the same field review.

## Save a draft

For a draft request, use `Save as Draft` and verify the resulting draft and
invoice number. Do not proceed through `Publish Invoice` as part of saving a
draft. If saving reports an error, the new invoice may already have an ID and
some persisted content: inspect it before creating a replacement.

## Change an already sent invoice

Sent and overdue invoices use `Save Changes`. Their confirmation defaults to
**resending the updated invoice**. If the user requested an edit without email,
uncheck resending and verify the final action reads `Save Changes` rather than
`Save & Resend`. If resending is requested, inspect actual recipients first.
Saving and email delivery are separate outcomes; a resend failure does not
mean the edits failed or should be submitted again.

## Publish and send

Only enter this flow when the task authorizes publishing/sending.

1. Finish the editor and select `Publish Invoice` to open `Review and send`.
2. Inspect invoice number, customer, amount due, currency, and recipients.
   Review CC tags and any email still typed in the input: the pending input can
   be included on submission. Prefilled company CC values are not authorization
   to send to additional recipients.
3. Resolve any missing recipient or sending authority before `Confirm & Publish`.
   Reuse an explicit instruction that already covers the actual invoice and
   recipients; do not add a redundant confirmation step.
4. Let the user complete signing if requested. If publication enters approval,
   read [approvals](approvals.md) and verify the resulting publication state.

Publication saves the draft before requesting publication. An interrupted or
failed publish can leave a valid draft. Inspect that invoice before retrying;
do not duplicate it. Report saved, pending publication/approval, published, and
email delivery only to the extent evidenced by the UI.

For reminders, marking paid, cancellation, or ending recurrence, load
[invoice actions](invoice-actions.md). For unclear results, load
[outcomes and recovery](outcomes-and-recovery.md).
