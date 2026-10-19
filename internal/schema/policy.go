package schema

// RootScopes is the closed set of upstream query entry points and the argument
// that must always be bound to the configured entity by the server.
var RootScopes = map[string]string{
	"entity":          "id",
	"statementExport": "entityId", "statementExports": "entityId",
	"cardDetail": "entityId", "cardSpendingSeries": "entityId",
	"cardTransactions": "entityId", "cardTransaction": "entityId",
	"approvalRecord": "entityId", "invoice": "entityId", "invoiceTimeline": "entityId",
	"globalSearch": "entityId", "searchFilterValues": "entityId",
}

// EntityFields excludes agent state, identity lifecycle, and security settings.
var EntityFields = map[string]bool{}

func init() {
	for _, name := range []string{
		"id", "name", "type", "state", "createdAt", "logoURL",
		"totalBalance", "walletTotalBalance", "accountBalances", "assetBalances", "instrumentBalances", "walletInstrumentBalances",
		"transactions", "transactionStats", "settledMovements", "analysisSettledMovements",
		"wallets", "bankingAccounts", "bankingDetail", "bankingProvider", "cardAccounts",
		"contacts", "contactTransactions", "contactTags", "transactionTags", "gasStation",
		"assets", "networks", "instruments", "defaultInstruments", "historicalBalance",
		"treausryPositionAccounts", "treausryPositionAccount", "treausryPositionAccountSummary",
		"yieldHistories", "positionYieldHistories", "yieldHistoriesConnection", "positionHistories",
		"positionTransactions", "treasuryTransactions", "treasuryPendingRedemptionTransactions",
		"approvalRecords", "userMemberships", "myPermissions", "myUserGroupMemberships", "userGroups",
		"invoices", "entityInvoices", "invoiceSettings", "cards", "cardholders", "myCards", "myCardTransactions",
		"recurringSourceInvoices", "invoiceTags", "invoiceTemplates", "invoiceReminderSettings", "invoiceCompanyProfile", "invoiceDashboardCards",
	} {
		EntityFields[name] = true
	}
}
