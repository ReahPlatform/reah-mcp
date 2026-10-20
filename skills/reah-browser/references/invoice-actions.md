# Invoice actions

Open the identified invoice from `/<entityId>/invoicing`. Match the business,
invoice number, customer, status, and amount before opening its action menu.
For draft creation or invoice edits, use [invoice editor](invoice-editor.md).

## Reminder or resend

A reminder is an external email action, not an analysis step. Verify the target
invoice and actual recipients against the user's sending request. Inspect the
confirmation (`Send reminder`, then `Send`) and submit only within that request.
An invoice edit can separately offer `Save & Resend`, with resending selected
by default; inspect that choice even when the user requested only an edit.
Distinguish a queued/accepted email from confirmed delivery.

## Mark paid or match a transaction

- Use actual evidence of payment and the specific invoice requested. `Mark as
  paid` records an invoice state; it does not establish that funds settled.
  Do not mark paid merely because an amount matches a balance or expected payment.
- Inspect `Auto-send receipt after marking as paid (sync with Invoice Settings)`.
  Its default can be enabled, and the choice can also update the business's
  receipt setting. An instruction to mark one invoice paid does not implicitly
  authorize emailing a receipt or changing future invoice settings. Resolve the
  intended effects before submitting; hand off if the UI cannot express that scope.
- For matching an existing transaction, compare the actual transaction identity,
  amount, currency, and invoice relationship. Do not invent a payment or unlink
  a prior match to get around a blocked action.
- Handle signing through the user. If approval is involved, read
  [approvals](approvals.md). Verify the invoice state and receipt outcome separately.

## Cancel one invoice

The reviewed cancellation flow allows sent or overdue invoices, and blocks
matched or partially matched invoices. Respect those guards.

1. Inspect the cancellation panel and choose the user's actual reason:
   customer request, duplicate, incorrect details, canceled order/service, or other.
   `Other` requires an internal note; do not fabricate a reason.
2. Inspect customer notification, which defaults on when an email is available.
   Ensure the user's instruction covers that email, or disable it when the
   intended action is cancellation without notification.
3. Confirm the cancellation within the requested scope. Verify the invoice state
   and notification result. A queued notice is not evidence of delivery.

**Canceling one recurring invoice does not stop its recurring schedule.** Do
not claim that future invoices or emails have stopped after canceling one item.

## Stop recurrence

Use the separate recurring-schedule action only when future recurrence should
end. Inspect whether the UI offers stopping or deleting the recurring schedule
and the effect described in its confirmation. Do not substitute deletion for a
request to stop. Verify the schedule's status separately from existing invoices;
stopping future automation does not establish that an existing invoice was canceled.

Reopening or other state changes require their own explicit task scope and live
confirmation. Use [outcomes and recovery](outcomes-and-recovery.md) for ambiguous
responses, and do not repeat an email action just because its panel disappeared.
