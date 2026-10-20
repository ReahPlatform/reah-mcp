# Approvals and execution

Load this guide when the task explicitly concerns approvals, or another action
creates a pending request. The approval page is `/<entityId>/approvals`; the
website can open a specific record with its `id` query parameter. Use the
record the UI actually returned, not a guessed identifier.

## Inspect the request

1. Match business, approval ID, action type, initiating user, and affected record.
   For financial actions also verify source, destination, asset, amount, and fees.
2. Inspect its current revision, decision history, remaining approval nodes,
   and execution state. A revised/superseded request is not the current request.
3. Use current list scopes/filters such as `Pending Approval`, `Pending Execution`,
   and `Completed`; do not assume every item visible in a list needs your decision.
4. Treat a contact/invoice request as pending until its approved change actually
   appears on that record. Opening the approval drawer does not apply the change.

## Decision boundaries

Act on a nonfinancial approval or rejection only when the user's task expressly
covers that specific decision and effect. Reuse that authorization rather than
asking for it repeatedly. Rejection has its own reason confirmation; use the
user's reason and do not invent one.

For any decision that can release funds, prepare the detail/review and hand
control to the user **before the decision button**. This includes plain
`Approve` when it could be the final approval, as well as `Approve & Execute`,
`Approve & Start funding`, `Start funding`, and `Execute`. Execution can follow
approval automatically; an `Approve` label is not a non-execution guarantee.
The user also completes passkey/wallet signing when required.

Respect execution locks, in-progress operations, revision guards, and disabled
actions. Do not use bulk selection to bypass detail review. Some actions are
ineligible for bulk approval and require their verified detail drawer.

## Interpret the outcome

| Observed state | What it establishes |
| --- | --- |
| Pending Approval | Further approval is required |
| Approved / Pending Execution | Approval passed; execution/settlement is not established |
| Executing | Execution is underway |
| Completed | The displayed request completed; inspect the affected financial/business record for its resulting state |
| Partially settled | Only part of the request settled; inspect every affected item |
| Rejected / Canceled / Failed | That request ended in the displayed state; inspect any effects already applied |

The drawer can close while background execution continues. Do not equate a
closed drawer, successful decision toast, or an approval vote with settlement.
Check the record and execution history after the decision. For transaction
results, distinguish an accepted request from confirmed settlement.

If a dispatched decision errors or times out, the result may be unknown.
Read [outcomes and recovery](outcomes-and-recovery.md) and inspect history before
any retry. Repeating an approval or execution is not a recovery strategy.
