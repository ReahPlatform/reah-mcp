# Outcomes and recovery

Load only when a result is unclear, authentication interrupts, or an action has
several stages. Keep the task's business and record identity established.

## Establish what happened before doing more

1. Record the last observed action, visible record/request identifier, and state.
   Do not include credentials, full bank details, PAN/CVV, or signing material.
2. Inspect the existing record, list, activity/history, and approval/execution
   state through normal UI. Refresh or reopen a read view when appropriate;
   refreshing a read view is different from submitting the form again.
3. Separate stages: saved draft, accepted change request, pending approval,
   executing transaction, settled result, and email notification. One stage's
   success does not prove the others succeeded.
4. If the action dispatched but its outcome cannot be established, stop further
   submissions and report that uncertainty with the record reference. Do not
   label it failed and recreate it merely because the page timed out.

## Known multi-stage cases

| Flow | Check before retrying |
| --- | --- |
| Contact creation with an attached crypto address | Contact may exist while address attachment failed; inspect that contact's methods |
| New invoice save or publish | Draft/ID may already exist; inspect saved fields and publication/approval separately |
| Save and resend an invoice | Edits can be saved even when resend fails; retry only a separately authorized, demonstrably failed email action |
| Match a transaction as paid | The match can persist before marking paid fails; inspect the link, invoice status, and any automatically requested receipt separately |
| Approval decision | Dispatched decision may have an unknown outcome; inspect revision, decision history, and execution |
| Bulk transfer | Check each row's request and execution state; do not resubmit successful or unknown rows |
| Treasury withdrawal/deposit | Track all underlying approval IDs, including those not opened in the first drawer |

For financial actions, the user completes any needed resubmission after the
same review boundary; the agent does not retry a committing financial request.

## Interrupted session or signing

Let the user finish login, email verification, MFA, passkey/wallet signing, or
KYC through the website. Do not request secret codes in chat, extract session
tokens, or fabricate identity information. After resuming, reread
[session and business](session-and-business.md) and inspect existing records
before restoring or resubmitting a form.

## Completion report

State the business, exact record reference, requested change, observed outcome,
and remaining user action. Use the actual status, for example “draft saved,”
“change pending approval,” “execution pending,” or “partially settled.” For a
prepared financial action, state that the review is ready and name the button
left for the user; do not say the transfer or withdrawal completed.
