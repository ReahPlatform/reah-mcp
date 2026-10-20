# Contact payment methods

Load this guide only when the task concerns a contact's bank or crypto details.
First establish the contact identity using [contacts](contacts.md).

## Bank account

Open the contact's bank-account controls. Reviewed routes include
`/<entityId>/contacts/<contactId>/bank-account/new` and
`/<entityId>/contacts/<contactId>/bank-account/<bankAccountId>/edit`.
The detail page may show the same editor in a side panel.

1. Inspect existing methods and choose the exact account to edit, or add a new
   one when requested. Preserve account-number formatting and leading zeros.
2. Select the intended currency. Required rails and fields depend on currency,
   country, account-holder type, and provider requirements in the live UI.
3. Complete the rail selection and bank/holder information. The contact wizard
   separates Currency, rails, and bank/holder details; another variant combines
   currency and rails into its first step. Follow the rendered steps.
4. Use the provided holder relationship and actual individual/business identity.
   Supply the required holder names, address, industry, and rail details from
   the user or an explicitly identified source. Do not invent missing values.
5. Inspect **every populated rail**, including collapsed sections. At least one
   rail must be complete, and partially filled additional rails can block saving.
   Collapsing a rail does not clear it. `Clear All` removes its values; changing
   currency can also reset incompatible rail details. Do either only within the
   requested change scope.
6. Select `Save` for creation or `Update` for editing when ready. If requirements
   are still loading or the button is disabled, resolve the visible condition;
   do not force submission. Handle signing and approvals through normal UI.
7. Verify currency, supported rail, holder, and masked destination on the saved
   method. Report pending approval separately from applied bank details.

## Crypto address

1. Select the requested network explicitly and enter the exact address and
   optional label/purpose. Do not infer the network solely from an `0x` prefix
   or manually normalize an address beyond the website's behavior.
2. The `EVM` option can add the address across multiple EVM networks. Use it
   only when that multi-network scope is intended; it does not mean Ethereum
   alone. Inspect the concrete networks shown by the UI.
3. The contact detail can group matching EVM addresses and labels into one row.
   Editing or deleting that row can affect several network-specific methods.
   Inspect that scope before confirming; do not interpret one row as one chain.
4. Submit the requested method change, hand signing to the user if prompted,
   and inspect approval state when present. Verify the address, label, and all
   affected networks after the change.

Use [approvals](approvals.md) if a request is pending, or
[outcomes and recovery](outcomes-and-recovery.md) if only part of the change
appears. Do not recreate the contact to repair an attachment failure.
