# Reviewed frontend sources

Maintainer reference; do not load during ordinary browser tasks.

Reviewed on October 20, 2026 against fetched `ReahPlatform/reah-web`
`origin/main` at `8584ded5759c41fd9371c823fbc985f27b01257c`.
Links pin that revision. These are frontend workflow findings, not proof that
every deployed environment runs this revision or that a financial request settled.
Use the live UI for current controls, permissions, fees, and record state.

When updating a guide, review its route, form, submit handler, approval/execution
path, and result handling together. In particular, approval button copy alone
does not establish a safe stopping point. Keep API field contracts in the MCP
server rather than duplicating them here.

## Session and business

Login handoff, business switching and its verification/rollback, route access.

- [constants/routes.ts](https://github.com/ReahPlatform/reah-web/blob/8584ded5759c41fd9371c823fbc985f27b01257c/apps/dashboard/src/constants/routes.ts)
- [layouts/components/EntitySelector.tsx](https://github.com/ReahPlatform/reah-web/blob/8584ded5759c41fd9371c823fbc985f27b01257c/apps/dashboard/src/layouts/components/EntitySelector.tsx)
- [hooks/useHandleLoginSuccess.ts](https://github.com/ReahPlatform/reah-web/blob/8584ded5759c41fd9371c823fbc985f27b01257c/apps/dashboard/src/hooks/useHandleLoginSuccess.ts)
- [features/entities/hooks/useEnsureEntityAccess.ts](https://github.com/ReahPlatform/reah-web/blob/8584ded5759c41fd9371c823fbc985f27b01257c/apps/dashboard/src/features/entities/hooks/useEnsureEntityAccess.ts)
- [features/entities/hooks/useEnsureEntitySwitchCodeVerification.ts](https://github.com/ReahPlatform/reah-web/blob/8584ded5759c41fd9371c823fbc985f27b01257c/apps/dashboard/src/features/entities/hooks/useEnsureEntitySwitchCodeVerification.ts)
- [features/entities/components/EntitySwitchCodeVerificationModal.tsx](https://github.com/ReahPlatform/reah-web/blob/8584ded5759c41fd9371c823fbc985f27b01257c/apps/dashboard/src/features/entities/components/EntitySwitchCodeVerificationModal.tsx)
- [features/access/AccessGuard.tsx](https://github.com/ReahPlatform/reah-web/blob/8584ded5759c41fd9371c823fbc985f27b01257c/apps/dashboard/src/features/access/AccessGuard.tsx)

## Contacts

Creation panels, profile and email changes, signing/approval, and separately attached crypto addresses.

- [pages/contacts/routes.tsx](https://github.com/ReahPlatform/reah-web/blob/8584ded5759c41fd9371c823fbc985f27b01257c/apps/dashboard/src/pages/contacts/routes.tsx)
- [pages/contacts/components/NewContactModal.tsx](https://github.com/ReahPlatform/reah-web/blob/8584ded5759c41fd9371c823fbc985f27b01257c/apps/dashboard/src/pages/contacts/components/NewContactModal.tsx)
- [pages/contacts/components/EditContactModal.tsx](https://github.com/ReahPlatform/reah-web/blob/8584ded5759c41fd9371c823fbc985f27b01257c/apps/dashboard/src/pages/contacts/components/EditContactModal.tsx)
- [features/contacts/hooks/useCreateContact.ts](https://github.com/ReahPlatform/reah-web/blob/8584ded5759c41fd9371c823fbc985f27b01257c/apps/dashboard/src/features/contacts/hooks/useCreateContact.ts)
- [features/contacts/hooks/useContactEmails.ts](https://github.com/ReahPlatform/reah-web/blob/8584ded5759c41fd9371c823fbc985f27b01257c/apps/dashboard/src/features/contacts/hooks/useContactEmails.ts)
- [features/contacts/hooks/useDeleteContact.ts](https://github.com/ReahPlatform/reah-web/blob/8584ded5759c41fd9371c823fbc985f27b01257c/apps/dashboard/src/features/contacts/hooks/useDeleteContact.ts)
- [pages/contacts/detail.tsx](https://github.com/ReahPlatform/reah-web/blob/8584ded5759c41fd9371c823fbc985f27b01257c/apps/dashboard/src/pages/contacts/detail.tsx)

## Contact payment methods

Dynamic bank requirements, all populated rails, and grouped EVM destinations.

- [pages/contacts/bank-account/components/ContactBankAccountForm.tsx](https://github.com/ReahPlatform/reah-web/blob/8584ded5759c41fd9371c823fbc985f27b01257c/apps/dashboard/src/pages/contacts/bank-account/components/ContactBankAccountForm.tsx)
- [pages/contacts/bank-account/components/contactBankAccountRequirements.ts](https://github.com/ReahPlatform/reah-web/blob/8584ded5759c41fd9371c823fbc985f27b01257c/apps/dashboard/src/pages/contacts/bank-account/components/contactBankAccountRequirements.ts)
- [pages/contacts/bank-account/components/contactBankRailValidation.ts](https://github.com/ReahPlatform/reah-web/blob/8584ded5759c41fd9371c823fbc985f27b01257c/apps/dashboard/src/pages/contacts/bank-account/components/contactBankRailValidation.ts)
- [features/contacts/hooks/useContactBankAccounts.ts](https://github.com/ReahPlatform/reah-web/blob/8584ded5759c41fd9371c823fbc985f27b01257c/apps/dashboard/src/features/contacts/hooks/useContactBankAccounts.ts)
- [pages/contacts/components/AddChainAddressModal.tsx](https://github.com/ReahPlatform/reah-web/blob/8584ded5759c41fd9371c823fbc985f27b01257c/apps/dashboard/src/pages/contacts/components/AddChainAddressModal.tsx)
- [features/contacts/groupChainAddresses.ts](https://github.com/ReahPlatform/reah-web/blob/8584ded5759c41fd9371c823fbc985f27b01257c/apps/dashboard/src/features/contacts/groupChainAddresses.ts)

## Invoice editor

Current routes and five steps, publish validation, save-before-publish, resending defaults, and CC handling.

- [pages/invoicing/routes.tsx](https://github.com/ReahPlatform/reah-web/blob/8584ded5759c41fd9371c823fbc985f27b01257c/apps/dashboard/src/pages/invoicing/routes.tsx)
- [pages/invoicing/edit.tsx](https://github.com/ReahPlatform/reah-web/blob/8584ded5759c41fd9371c823fbc985f27b01257c/apps/dashboard/src/pages/invoicing/edit.tsx)
- [pages/invoicing/editor/publish/invoiceStepValidation.ts](https://github.com/ReahPlatform/reah-web/blob/8584ded5759c41fd9371c823fbc985f27b01257c/apps/dashboard/src/pages/invoicing/editor/publish/invoiceStepValidation.ts)
- [pages/invoicing/editor/publish/useInvoicePublish.ts](https://github.com/ReahPlatform/reah-web/blob/8584ded5759c41fd9371c823fbc985f27b01257c/apps/dashboard/src/pages/invoicing/editor/publish/useInvoicePublish.ts)
- [pages/invoicing/invoiceEditGuards.ts](https://github.com/ReahPlatform/reah-web/blob/8584ded5759c41fd9371c823fbc985f27b01257c/apps/dashboard/src/pages/invoicing/invoiceEditGuards.ts)
- [pages/invoicing/SendInvoiceConfirmPanel.tsx](https://github.com/ReahPlatform/reah-web/blob/8584ded5759c41fd9371c823fbc985f27b01257c/apps/dashboard/src/pages/invoicing/SendInvoiceConfirmPanel.tsx)
- [pages/invoicing/SaveSentInvoiceChangesConfirmModal.tsx](https://github.com/ReahPlatform/reah-web/blob/8584ded5759c41fd9371c823fbc985f27b01257c/apps/dashboard/src/pages/invoicing/SaveSentInvoiceChangesConfirmModal.tsx)

## Invoice actions

Reminders, receipt-setting side effects, matching, cancellation guards, and the separate recurring-schedule lifecycle.

- [pages/invoicing/detail.tsx](https://github.com/ReahPlatform/reah-web/blob/8584ded5759c41fd9371c823fbc985f27b01257c/apps/dashboard/src/pages/invoicing/detail.tsx)
- [pages/invoicing/SendReminderConfirmModal.tsx](https://github.com/ReahPlatform/reah-web/blob/8584ded5759c41fd9371c823fbc985f27b01257c/apps/dashboard/src/pages/invoicing/SendReminderConfirmModal.tsx)
- [pages/invoicing/MarkAsPaidConfirmModal.tsx](https://github.com/ReahPlatform/reah-web/blob/8584ded5759c41fd9371c823fbc985f27b01257c/apps/dashboard/src/pages/invoicing/MarkAsPaidConfirmModal.tsx)
- [pages/invoicing/ConfirmTransactionMatchModal.tsx](https://github.com/ReahPlatform/reah-web/blob/8584ded5759c41fd9371c823fbc985f27b01257c/apps/dashboard/src/pages/invoicing/ConfirmTransactionMatchModal.tsx)
- [pages/invoicing/CancelInvoicePanel.tsx](https://github.com/ReahPlatform/reah-web/blob/8584ded5759c41fd9371c823fbc985f27b01257c/apps/dashboard/src/pages/invoicing/CancelInvoicePanel.tsx)
- [pages/invoicing/invoiceCancellation.ts](https://github.com/ReahPlatform/reah-web/blob/8584ded5759c41fd9371c823fbc985f27b01257c/apps/dashboard/src/pages/invoicing/invoiceCancellation.ts)
- [pages/invoicing/StopRecurringConfirmModal.tsx](https://github.com/ReahPlatform/reah-web/blob/8584ded5759c41fd9371c823fbc985f27b01257c/apps/dashboard/src/pages/invoicing/StopRecurringConfirmModal.tsx)

## Transfers

Current transfer route, step order, bulk review, availability gates, and potentially committing submit labels.

- [pages/wallet-action/operations.routes.tsx](https://github.com/ReahPlatform/reah-web/blob/8584ded5759c41fd9371c823fbc985f27b01257c/apps/dashboard/src/pages/wallet-action/operations.routes.tsx)
- [pages/wallet-action/send/wizard/transferSteps.ts](https://github.com/ReahPlatform/reah-web/blob/8584ded5759c41fd9371c823fbc985f27b01257c/apps/dashboard/src/pages/wallet-action/send/wizard/transferSteps.ts)
- [pages/wallet-action/send/wizard/reviewSubmitState.ts](https://github.com/ReahPlatform/reah-web/blob/8584ded5759c41fd9371c823fbc985f27b01257c/apps/dashboard/src/pages/wallet-action/send/wizard/reviewSubmitState.ts)
- [pages/wallet-action/send/useSendForm.ts](https://github.com/ReahPlatform/reah-web/blob/8584ded5759c41fd9371c823fbc985f27b01257c/apps/dashboard/src/pages/wallet-action/send/useSendForm.ts)

## Conversions

Supported account pairs, four-step wizard, quote refresh and route-dependent capabilities.

- [pages/wallet-action/convert/adapter/deriveConvertKind.ts](https://github.com/ReahPlatform/reah-web/blob/8584ded5759c41fd9371c823fbc985f27b01257c/apps/dashboard/src/pages/wallet-action/convert/adapter/deriveConvertKind.ts)
- [pages/wallet-action/convert/adapter/capabilities.ts](https://github.com/ReahPlatform/reah-web/blob/8584ded5759c41fd9371c823fbc985f27b01257c/apps/dashboard/src/pages/wallet-action/convert/adapter/capabilities.ts)
- [pages/wallet-action/convert/page/ConvertPage.tsx](https://github.com/ReahPlatform/reah-web/blob/8584ded5759c41fd9371c823fbc985f27b01257c/apps/dashboard/src/pages/wallet-action/convert/page/ConvertPage.tsx)
- [pages/wallet-action/convert/page/controls/StepPanels.tsx](https://github.com/ReahPlatform/reah-web/blob/8584ded5759c41fd9371c823fbc985f27b01257c/apps/dashboard/src/pages/wallet-action/convert/page/controls/StepPanels.tsx)

## Approvals and recovery

Immediate execution after initialization/approval, decision ambiguity, background execution, statuses and bulk eligibility.

- [features/approvals/init-flow/useInitApprovalFlow.ts](https://github.com/ReahPlatform/reah-web/blob/8584ded5759c41fd9371c823fbc985f27b01257c/apps/dashboard/src/features/approvals/init-flow/useInitApprovalFlow.ts)
- [features/approvals/init-flow/autoExecution.ts](https://github.com/ReahPlatform/reah-web/blob/8584ded5759c41fd9371c823fbc985f27b01257c/apps/dashboard/src/features/approvals/init-flow/autoExecution.ts)
- [features/record-detail/approval/Footer.tsx](https://github.com/ReahPlatform/reah-web/blob/8584ded5759c41fd9371c823fbc985f27b01257c/apps/dashboard/src/features/record-detail/approval/Footer.tsx)
- [features/approvals/decision/useApprovalDecision.ts](https://github.com/ReahPlatform/reah-web/blob/8584ded5759c41fd9371c823fbc985f27b01257c/apps/dashboard/src/features/approvals/decision/useApprovalDecision.ts)
- [features/approvals/utils/statusDisplay.tsx](https://github.com/ReahPlatform/reah-web/blob/8584ded5759c41fd9371c823fbc985f27b01257c/apps/dashboard/src/features/approvals/utils/statusDisplay.tsx)
- [pages/approvals/routes.tsx](https://github.com/ReahPlatform/reah-web/blob/8584ded5759c41fd9371c823fbc985f27b01257c/apps/dashboard/src/pages/approvals/routes.tsx)
- [pages/approvals/list/scopeConfig.ts](https://github.com/ReahPlatform/reah-web/blob/8584ded5759c41fd9371c823fbc985f27b01257c/apps/dashboard/src/pages/approvals/list/scopeConfig.ts)
- [pages/approvals/components/finalizeApprovalAction.ts](https://github.com/ReahPlatform/reah-web/blob/8584ded5759c41fd9371c823fbc985f27b01257c/apps/dashboard/src/pages/approvals/components/finalizeApprovalAction.ts)
- [pages/approvals/bulk/eligibility.ts](https://github.com/ReahPlatform/reah-web/blob/8584ded5759c41fd9371c823fbc985f27b01257c/apps/dashboard/src/pages/approvals/bulk/eligibility.ts)

## Cards

Detail navigation, state-aware freezing, dollar-to-minor conversion, issuance fees and conditional review.

- [pages/cards/routes.tsx](https://github.com/ReahPlatform/reah-web/blob/8584ded5759c41fd9371c823fbc985f27b01257c/apps/dashboard/src/pages/cards/routes.tsx)
- [pages/cards/management/CardDetailDrawer.tsx](https://github.com/ReahPlatform/reah-web/blob/8584ded5759c41fd9371c823fbc985f27b01257c/apps/dashboard/src/pages/cards/management/CardDetailDrawer.tsx)
- [pages/cards/management/EditCardLimitPanel.tsx](https://github.com/ReahPlatform/reah-web/blob/8584ded5759c41fd9371c823fbc985f27b01257c/apps/dashboard/src/pages/cards/management/EditCardLimitPanel.tsx)
- [pages/cards/my-cards/CardFreezeToggle.tsx](https://github.com/ReahPlatform/reah-web/blob/8584ded5759c41fd9371c823fbc985f27b01257c/apps/dashboard/src/pages/cards/my-cards/CardFreezeToggle.tsx)
- [pages/cards/create/CreateCardForm.tsx](https://github.com/ReahPlatform/reah-web/blob/8584ded5759c41fd9371c823fbc985f27b01257c/apps/dashboard/src/pages/cards/create/CreateCardForm.tsx)
- [pages/cards/create/CardIssuanceReviewModal.tsx](https://github.com/ReahPlatform/reah-web/blob/8584ded5759c41fd9371c823fbc985f27b01257c/apps/dashboard/src/pages/cards/create/CardIssuanceReviewModal.tsx)

## Treasury

Product/position navigation, deposit/withdraw review, minimum and quote gates, and multiple underlying approval IDs.

- [pages/treasury/routes.tsx](https://github.com/ReahPlatform/reah-web/blob/8584ded5759c41fd9371c823fbc985f27b01257c/apps/dashboard/src/pages/treasury/routes.tsx)
- [features/treasury/components/DepositWithdrawPanel.tsx](https://github.com/ReahPlatform/reah-web/blob/8584ded5759c41fd9371c823fbc985f27b01257c/apps/dashboard/src/features/treasury/components/DepositWithdrawPanel.tsx)
- [features/treasury/components/DepositForm.tsx](https://github.com/ReahPlatform/reah-web/blob/8584ded5759c41fd9371c823fbc985f27b01257c/apps/dashboard/src/features/treasury/components/DepositForm.tsx)
- [features/treasury/components/WithdrawForm.tsx](https://github.com/ReahPlatform/reah-web/blob/8584ded5759c41fd9371c823fbc985f27b01257c/apps/dashboard/src/features/treasury/components/WithdrawForm.tsx)
- [features/treasury/components/ConfirmModal.tsx](https://github.com/ReahPlatform/reah-web/blob/8584ded5759c41fd9371c823fbc985f27b01257c/apps/dashboard/src/features/treasury/components/ConfirmModal.tsx)
- [features/treasury/confirmation.ts](https://github.com/ReahPlatform/reah-web/blob/8584ded5759c41fd9371c823fbc985f27b01257c/apps/dashboard/src/features/treasury/confirmation.ts)
- [features/treasury/withdraw-guards.ts](https://github.com/ReahPlatform/reah-web/blob/8584ded5759c41fd9371c823fbc985f27b01257c/apps/dashboard/src/features/treasury/withdraw-guards.ts)
