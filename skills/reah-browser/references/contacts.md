# Contacts

## Locate and create

Contacts live at `/<entityId>/contacts`; a detail page uses
`/<entityId>/contacts/<contactId>`. The new-contact route can open a panel over
the list rather than a standalone page. Use the visible `New contact` flow.

1. Check for an existing matching contact before creating one. Match more than
   a display name when the task includes an email, address, or account reference.
2. Fill `Name` and `Type` (`Business` or `Individual`). Use the user's actual
   party type rather than guessing from a name.
3. Fill `Email (Optional)` and comma-separated `Tags (Optional)` only as requested.
   The basic creation form is not the full bank or crypto payment-method editor.
4. Inspect the fields and select `Add` within the user's creation request.
5. If a signing challenge appears, let the user complete it. If Reah creates an
   approval, follow [approvals](approvals.md) to determine the actual outcome.
   A submitted request is not yet a created contact.

Creation initiated from another flow can attach a crypto address separately
after contact creation. Verify both the contact and the address. If only the
contact exists, resume that contact's payment-method flow instead of creating
another contact. Do not assume the newest contact with the same name is yours.

## Edit profile, email, or delete

- The profile editor exposes `Name` and `Tag`, with `Save`. Do not assume the
  contact type or email is editable in that same form.
- Manage email through the contact's email controls. Verify the exact address
  and whether the user asked to add or remove it. Profile saving alone does not
  establish that the email list changed.
- Profile changes, email changes, deletion, and payment-method changes can use
  signing and approval. Inspect their resulting state instead of treating panel
  closure as a completed write.
- For deletion, verify the exact contact and requested removal scope in the
  confirmation. Reuse explicit deletion authorization; do not infer it from a
  request to clean up or inspect duplicates.

## Payment methods: load only when needed

For bank details or crypto destinations, read
[contact payment methods](contact-payment-methods.md) before filling or editing
them. These forms have currency, rail, and network-specific behavior.

A payment method's `Send` action opens a transfer workflow. Saving a beneficiary
does not authorize a transfer. If sending is also requested, switch to
[transfers](transfers.md) and stop at the financial handoff boundary.

## Verify

Reopen the identified contact and inspect the changed profile, email, or payment
method. If an approval is pending, report its reference and pending state rather
than saying the contact was updated. Use
[outcomes and recovery](outcomes-and-recovery.md) for ambiguous or partial results.
