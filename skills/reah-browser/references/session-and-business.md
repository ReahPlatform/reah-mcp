# Session and business

## Establish context

1. Find the user's existing Reah tab at `https://platform.reah.com`, or the
   environment the user explicitly selected. Use the host's documented browser
   connection flow. Do not silently switch between production and staging.
2. Inspect the signed-in identity through the visible account UI. If login or
   an access challenge is needed, let the user complete it. Do not create a
   separate Agent session or export the browser session to MCP.
3. Match the workspace name and business in the current URL to the user's task.
   Reah business routes start with `/<entityId>/`. Record the business identifier
   for this task without copying credentials or sensitive account details.
4. If the user's intent identifies one business, use it without another question.
   If several remain plausible, ask which business before changing records.
   Do not choose the first business merely because it appears first in a list.

## Switch through the website

Use the workspace selector. Its accessible label includes
`Switch workspace. Current workspace: <name>`. Selecting another business may
require email verification, a passkey, or another access check. Wait for the
user to complete that UI before continuing.

After switching, verify both the current workspace label and the destination
business route. A canceled switch can return to the previous business;
selecting a menu item alone does not prove that the switch succeeded.
Recheck context before submission and after navigation to another business.

## Permissions and record identity

- Respect disabled controls, missing permissions, and view-as/read-only mode.
  Do not change local storage, URL state, or requests to bypass an access check.
- Browser permissions belong to the signed-in web account. The MCP connection's
  read authorization and enterprise list are separate from those permissions.
- Locate the target through the live list/detail UI. Compare stable identifiers
  when available and task-specific details such as customer, invoice number,
  masked account, network, or cardholder. Names alone may not be unique.
- Prefer visible navigation. Route examples in the task guides identify reviewed
  pages; they are not permission to guess record identifiers or build action URLs
  with sensitive payment information.
- If context or permission changes midway, re-establish it before further edits.
  For expired sessions or interrupted submissions, use
  [outcomes and recovery](outcomes-and-recovery.md).
