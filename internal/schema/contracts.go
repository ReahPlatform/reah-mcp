package schema

import (
	"encoding/json"
	"fmt"
	"strings"
)

// FieldContract is the reviewed public contract. A field absent from this table
// is not part of the MCP schema, even when the upstream API exposes it.
type FieldContract struct {
	Description   string            `json:"description"`
	Unit          string            `json:"unit,omitempty"`
	Scale         *int              `json:"scale,omitempty"`
	ScaleField    string            `json:"scale_field,omitempty"`
	Currency      string            `json:"currency,omitempty"`
	CurrencyField string            `json:"currency_field,omitempty"`
	Integer       bool              `json:"integer,omitempty"`
	Requires      map[string]string `json:"requires,omitempty"`
	Evidence      string            `json:"evidence"`
	Arguments     map[string]string `json:"arguments,omitempty"`
	SourceLimit   int               `json:"source_limit,omitempty"`
}

var PublicFields = ContractsFromSource(SourceSemantics)

// PublicTypeDescriptions correct upstream type-level documentation where the
// reviewed resolver behavior differs from the broader upstream schema.
var PublicTypeDescriptions = map[string]string{
	"AssetConnection":                "Full entity asset catalog. No pagination parameters or cursor are exposed.",
	"TreasuryPositionAccountSummary": "Aggregate of at most the latest 200 matching Treasury positions. Not a proven full portfolio total; inspect the enclosing field's read_coverage.",
}

// ContractsFromSource combines an explicit field allowlist with pinned unit
// evidence. New upstream registry entries are never admitted automatically.
func ContractsFromSource(sourceJSON []byte) map[string]FieldContract {
	result := map[string]FieldContract{}
	add := func(parent, field, description string) {
		result[parent+"."+field] = FieldContract{Description: description, Evidence: "pinned_schema:" + parent + "." + field}
	}
	// Structural fields and identifiers never carry an implicit financial unit.
	for parent, names := range map[string]string{
		"Entity": "id name type state", "Wallet": "id humanId name state", "BankingAccount": "id humanId name state provider",
		"CardAccount": "id", "Card": "id nickname last4 status", "Contact": "id name type", "User": "id name email",
		"Transaction": "id humanId type state notes counterpartyTexts", "ApprovalRecord": "id humanId status type",
		"Instrument": "id", "InstrumentAccount": "id", "BlockchainAccount": "id", "TreasuryAccount": "id",
		"TreasuryPositionAccount": "id entityId", "Vault": "id name", "Asset": "id name symbol fiat defaultDecimals",
		"Network": "id name", "Invoice": "id invoiceNumber status customerName",
		"StatementExportTask": "id status fileName rowCount", "CardTransaction": "transactionId merchantName transactionType uiStatus",
	} {
		for _, field := range strings.Fields(names) {
			description := map[string]string{
				"id":       "Stable upstream identifier. Treat as opaque; do not infer business meaning from its format.",
				"entityId": "Identifier of the entity that owns this record.", "humanId": "Human-readable reference; distinct from the stable API identifier.",
				"name": "Display name of this record.", "type": "Upstream record category. Preserve the exact returned value.",
				"state":    "Upstream lifecycle state; a record's presence does not establish settlement or spendability.",
				"status":   "Upstream lifecycle status; a record's presence does not establish settlement or payment.",
				"provider": "Raw banking provider identifier.", "nickname": "User-assigned card display name.", "last4": "Last four card-number digits only; never a full card number.",
				"email": "Member email address.", "notes": "User-entered transaction notes; not authoritative settlement evidence.",
				"counterpartyTexts": "Counterparty display/search text; not proof of external ownership or deduplication identity.",
				"symbol":            "Asset ticker/code. Symbols alone do not uniquely identify instruments across networks.",
				"fiat":              "Whether this asset is a fiat currency.", "defaultDecimals": "Asset default precision in decimal places. Use Instrument.decimals for a specific instrument.",
				"invoiceNumber": "Invoice display reference; distinct from its API identifier.", "customerName": "Customer display name recorded on the invoice.",
				"fileName":      "Name of an existing statement export file; does not provide download access.",
				"rowCount":      "Number of rows reported by the export task; not a monetary value.",
				"transactionId": "Stable identifier of the card transaction.", "merchantName": "Merchant display name reported for the card transaction.",
				"transactionType": "Card activity category; use together with uiStatus. Funding events and merchant spending are distinct.",
				"uiStatus":        "Card transaction display lifecycle status; pending activity is not settled cashflow.",
			}[field]
			add(parent, field, description)
		}
	}
	for parent, fields := range map[string]map[string]string{
		"Query": {
			"entity":           "Entity scoped to the server-configured identity and entity ID.",
			"cardTransactions": "Paginated card activity for the configured entity; not a settled cashflow ledger.",
			"statementExports": "Existing statement export tasks. Does not create exports or download links.",
			"globalSearch":     "Bounded search matches. Results and totals do not establish a complete financial population.",
			"invoice":          "An invoice within the configured entity. Invoice amounts are not evidence of cash receipt.",
		},
		"Entity": {
			"totalBalance":       "Point-in-time upstream total USD valuation. Not a spendable balance or the Agent's spam-filtered summary; do not add overlapping account totals to it.",
			"instrumentBalances": "Paginated instrument balance observations. Follow cursor and do not sum overlapping aggregations.",
			"wallets":            "Wallet roster for this entity.", "bankingAccounts": "Banking account roster for this entity.",
			"transactions":             "Paginated transaction lifecycle records; includes activity that may not have settled.",
			"analysisSettledMovements": "Settled posting facts in an explicit time window. Retain coverage, exclusions and valuation status for all aggregate claims.",
			"contacts":                 "Paginated contact roster.", "invoices": "Paginated invoice records. An invoice is not proof of received funds.",
			"approvalRecords":                "Paginated approval records. Approval status is distinct from transaction settlement.",
			"cardAccounts":                   "Card account roster; each nested cards connection has independent pagination.",
			"treausryPositionAccounts":       "At most the latest 200 matching Treasury positions, ordered by created_at DESC, id DESC. No continuation is available. A full batch cannot establish complete holdings; check read_coverage. USD valuations are not spendable cash and may overlap account totals.",
			"treausryPositionAccountSummary": "Aggregate of at most the latest 200 matching Treasury positions, not a guaranteed full portfolio total. No continuation or contributing count is available; read_coverage reports completeness as not_proven. Do not add to underlying position values.",
			"userMemberships":                "Paginated memberships of this entity.", "assets": "Full entity asset catalog. The upstream resolver does not implement pagination; no first, after or cursor is exposed.",
		},
		"Balance": {
			"aggregation":  "Aggregation identity for this balance. Asset, instrument, network and total views may overlap.",
			"priceVersion": "Timestamp of the price observation used for the USD valuation; not a guarantee of present market price.",
		},
		"Wallet":                  {"totalBalance": "Upstream wallet valuation; not spendable-source readiness. May overlap entity and instrument totals."},
		"BankingAccount":          {"totalBalance": "Upstream bank account valuation; not its available bank-transfer or conversion ceiling."},
		"CardAccount":             {"totalBalance": "Upstream card account balance valuation; not a spending limit.", "cards": "Paginated masked cards. Follow this account's cursor independently."},
		"Instrument":              {"asset": "Underlying asset identity.", "network": "Network identity for this instrument.", "decimals": "Number of base-10 decimal places for this instrument's smallest unit."},
		"TreasuryPositionAccount": {"vault": "Associated physical vault, including underlying instrument and current product APY."},
		"Vault":                   {"underlyingInstrument": "Underlying instrument used to identify asset-denominated Treasury amounts."},
		"Invoice":                 {"amountSummary": "Invoice amounts in major currency units; not a cash-receipt ledger."},
		"InvoiceAmountSummary":    {"currency": "Primary invoice currency code. Distinct from per-line currencies on mixed-currency invoices."},
		"EntityMembership":        {"user": "Member identity scoped to this entity."},
		"ApprovalRecordDetail":    {"summary": "Approval lifecycle summary; does not establish financial execution."},
		"GlobalSearchResult":      {"query": "Search text applied by the upstream service.", "total": "Count of search matches; not a complete financial dataset.", "groups": "Matches grouped by resource category."},
		"SearchGroup":             {"type": "Resource category of this search group.", "items": "Matching records. The GraphQL concrete type determines each item's fields."},
		"SettledMovement": {
			"movementId": "Stable posting/movement identifier; use for deduplication across pages.", "transactionId": "Identifier of the parent transaction; one transaction may have multiple movements.",
			"transactionType": "Parent transaction category; not by itself an inflow/outflow classification.", "transactionState": "Parent lifecycle state recorded for this posting.",
			"accountId": "Account whose instrument balance this movement changes.", "instrumentId": "Exact instrument identifier; symbols alone cannot identify it.",
			"assetSymbol": "Asset display symbol of the movement instrument.", "instrumentDecimals": "Decimal places used to convert signedAmountSmallest to major asset units.",
			"role":            "Posting role from the Ledger. Preserve the raw role and sign when grouping cashflow.",
			"usdValue":        "Posting-date USD valuation with explicit currency and scale; null means no valuation object, not zero.",
			"valuationStatus": "Availability/quality of posting valuation. Missing or unavailable valuations must not be counted as zero.",
		},
		"SettledMovementMoney": {"currency": "Currency code of amountMinor.", "scale": "Decimal places for amountMinor; major amount = amountMinor / 10^scale."},
		"SettledMovementCoverage": {
			"firstObservedSettledAt": "Earliest observed settled posting timestamp in the returned coverage.", "lastObservedSettledAt": "Latest observed settled posting timestamp in the returned coverage.",
			"ingestionCoverageProven": "Whether external ingestion coverage is proven; distinct from successfully fetching all Reah pages.",
			"coverageNote":            "Upstream explanation of ingestion/observation coverage.", "unpostedPartialTransactionCount": "Number of partial transactions lacking complete postings; limits completeness claims.",
			"unpostedPartialTransactionHumanIds": "Display references of partial transactions lacking complete postings.",
		},
		"AnalysisSettledMovementConnection": {
			"movements": "Posting facts in this page; follow nextPageToken before claiming complete acquisition.", "coverage": "Coverage and unposted-partial-transaction information.",
			"dataAsOf": "Timestamp of the analysis data observation.", "balanceEntryIdWatermark": "Opaque Ledger balance-entry watermark; preserve as a string.",
			"priceVersionWatermark": "Opaque price-version watermark for this observation.", "includesTreasury": "Whether the query includes Treasury activity.",
			"includesSpam": "Whether the query includes spam-classified activity.", "transactionIds": "Parent transaction identifiers selected for this analysis, not independent movement counts.",
			"excludesInternalActivity": "Whether internal activity was excluded.", "excludesSmallBalances": "Whether small-balance activity was excluded.",
			"excludedSmallBalanceMovementCount": "Count of movements excluded by the small-balance filter.", "unknownSmallBalanceParentCount": "Count of parents with unavailable small-balance classification; not a missing monetary value.",
			"excludedTreasuryMovementCount": "Count of Treasury movements excluded by the query.", "excludedInternalActivityMovementCount": "Count of internal movements excluded by the query.",
			"excludedSpamMovementCount": "Count of spam movements excluded by the query.",
		},
		"Card":            {"limitFrequency": "Period over which limitAmountMinor applies; not a percentage or amount."},
		"CardTransaction": {"currency": "Currency label reported for card activity, including USD or USDC. No transaction amount is exposed because the upstream field mixes sources without an explicit scale."},
	} {
		for field, description := range fields {
			add(parent, field, description)
		}
	}
	for parent, item := range map[string]string{
		"BalanceConnection": "balances", "TransactionConnection": "transactions", "ContactConnection": "contacts", "InvoiceConnection": "invoices",
		"ApprovalRecordConnection": "records", "CardConnection": "cards", "CardTransactionConnection": "transactions", "StatementExportConnection": "tasks",
		"EntityMembershipConnection": "memberships",
	} {
		add(parent, item, "Records in this page only. Fetch subsequent pages before claiming complete coverage.")
		add(parent, "cursor", "Opaque next-page cursor. Pass as after with unchanged filters; null or empty indicates no next page.")
	}
	add("AssetConnection", "assets", "Full entity asset catalog returned by the upstream service; not a paginated connection.")
	for _, field := range []string{"treausryPositionAccounts", "treausryPositionAccountSummary"} {
		key := "Entity." + field
		contract := result[key]
		contract.SourceLimit = 200
		contract.Evidence = "reah-graphql-gateway:9b2e65e0d4a6c92d99e02965b322dcd9bf279d94:internal/graph/resolver/entity_treasury_helpers.go#listTreasuryPositionAccounts;internal/graph/resolver/entity.resolvers.go#TreausryPositionAccountSummary;reah-ledger:c62e0e1124bdef649a3a4b28bbeba082c27bf93c:internal/repos/treasury_position_accounts/query.sql#ListTreasuryPositionsByEntity"
		result[key] = contract
	}
	for _, key := range []string{"Entity.assets", "AssetConnection.assets"} {
		contract := result[key]
		contract.Evidence = "reah-graphql-gateway:9b2e65e0d4a6c92d99e02965b322dcd9bf279d94:internal/graph/resolver/entity.resolvers.go#Assets"
		result[key] = contract
	}
	add("AnalysisSettledMovementConnection", "nextPageToken", "Opaque next-page cursor. Pass as after with the original time window and filters; null or empty indicates no next page.")
	add("Total", "_empty", "Structural placeholder for the total aggregation; carries no monetary value.")
	for parent, names := range map[string]string{
		"Transaction": "initiatedAt finishedAt", "CardTransaction": "initiatedAt", "Invoice": "issueDate dueDate", "ApprovalRecord": "initiatedAt completedAt",
		"StatementExportTask": "createdAt completedAt", "SettledMovement": "settledAt", "EntityMembership": "joinedAt",
	} {
		for _, field := range strings.Fields(names) {
			add(parent, field, "Upstream "+field+" timestamp (RFC3339). Preserve its UTC offset; null is unavailable or not yet recorded. Date-window reporting must use the user's timezone.")
		}
	}
	// Import reviewed unit contracts only for public fields, never by leaf name.
	var upstream map[string]struct {
		Unit        string `json:"unit"`
		Scale       int    `json:"scale"`
		Description string `json:"description"`
		Evidence    string `json:"evidence_ref"`
	}
	if err := json.Unmarshal(sourceJSON, &upstream); err != nil {
		panic(err)
	}
	for _, key := range []string{
		"TreasuryPositionAccount.currentValueInUSD", "TreasuryPositionAccount.investedInUSD", "TreasuryPositionAccount.unrealizedProfitInUSD",
		"TreasuryPositionAccountSummary.averageAPY", "TreasuryPositionAccountSummary.currentValueInUSD", "TreasuryPositionAccountSummary.investedInUSD", "TreasuryPositionAccountSummary.totalEarnedInUSD", "TreasuryPositionAccountSummary.unrealizedProfitInUSD", "Vault.apy",
	} {
		source, ok := upstream[key]
		if !ok {
			panic("missing pinned semantic contract: " + key)
		}
		c := FieldContract{Description: source.Description, Unit: source.Unit, Scale: scale(source.Scale), Evidence: "pinned_agent_registry:" + source.Evidence}
		if source.Unit == "usd_minor_units" {
			c.Currency = "USD"
		}
		if source.Unit == "decimal_fraction" {
			c.Description += " Decimal fraction: 0.05 means 5%; normalized_values.percent is already multiplied by 100."
		}
		result[key] = c
	}

	money := func(key, description string, c FieldContract) {
		c.Description = description
		if c.Evidence == "" {
			c.Evidence = "pinned_schema:" + key
		}
		result[key] = c
	}
	money("Balance.usdAmount", "USD valuation in cents, including fractional cents. 10000 means USD 100.00. Not spendability, cashflow or a spam-filtered summary.", FieldContract{Unit: "usd_minor_units", Scale: scale(2), Currency: "USD", Evidence: "reah-ledger:f4fa1d1d73502ed4fc4e5923f297dfb727d06663:internal/convert/balances.go#BalanceToProto"})
	money("Balance.nativeAmount", "Major asset units already scaled for display (1.5 means 1.5 tokens). Null for aggregations without a native amount. Do not divide by instrument decimals again or add different assets.", FieldContract{Unit: "asset_units", Scale: scale(0), CurrencyField: "aggregation", Evidence: "reah-ledger:f4fa1d1d73502ed4fc4e5923f297dfb727d06663:internal/convert/balances.go#BalanceToProto"})
	money("TreasuryPositionAccount.currentValueInAssets", "Current position value in major underlying asset units. Do not divide by instrument decimals again or add different assets.", FieldContract{Unit: "asset_units", Scale: scale(0), CurrencyField: "vault.underlyingInstrument.asset.symbol", Requires: map[string]string{"vault.underlyingInstrument.asset.symbol": "String"}, Evidence: "reah-ledger:f4fa1d1d73502ed4fc4e5923f297dfb727d06663:internal/svc/treasury_apis.go#toProtoTreasuryPosition"})
	money("Card.limitAmountMinor", "USD spending limit in integer cents, applied over limitFrequency. Not current balance or available cash.", FieldContract{Unit: "usd_minor_units", Scale: scale(2), Currency: "USD", Integer: true, Evidence: "reah-web:a06d0d5741f1abb2f6a6ca6bb58a070ce02ac7ca:apps/dashboard/src/pages/cards/utils/card-maps.ts#minorToDollars"})
	money("SettledMovement.signedAmountSmallest", "Signed integer balance change in the instrument's smallest unit. Positive increases and negative decreases this account's instrument balance; this is not by itself an external cashflow classification.", FieldContract{Unit: "instrument_smallest_units", ScaleField: "instrumentDecimals", CurrencyField: "assetSymbol", Integer: true, Requires: map[string]string{"instrumentDecimals": "Int", "assetSymbol": "String"}})
	money("SettledMovementMoney.amountMinor", "Posting-date valuation in integer minor units of currency. Use the explicit scale; null parent valuation means unavailable, not zero.", FieldContract{Unit: "currency_minor_units", ScaleField: "scale", CurrencyField: "currency", Integer: true, Requires: map[string]string{"scale": "Int", "currency": "String"}})
	money("InvoiceAmountSummary.amountDue", "Final invoice amount in major units of currency. For mixed-currency invoices this is the primary-currency total, not each line's currency. Not evidence of payment.", FieldContract{Unit: "currency_units", Scale: scale(0), CurrencyField: "currency", Requires: map[string]string{"currency": "String"}})
	money("InvoiceAmountSummary.amountDueUsd", "Frozen publish-time invoice USD amount in major dollar units, not cents. Legacy empty string means unavailable, not zero. Use for cross-currency invoice valuation, not cash receipts.", FieldContract{Unit: "usd_dollars", Scale: scale(0), Currency: "USD"})
	argument := func(key string, descriptions map[string]string) {
		c, ok := result[key]
		if !ok {
			panic(key)
		}
		c.Arguments = descriptions
		result[key] = c
	}
	scope := map[string]string{"id": "Entity ID; must equal the server-configured entity."}
	argument("Query.entity", scope)
	for _, key := range []string{"Query.cardTransactions", "Query.statementExports"} {
		argument(key, map[string]string{"entityId": "Entity ID; supplied by the server.", "first": "Page size from 1 to 200; defaults to 100 in business tools.", "after": "Opaque cursor from the preceding page; keep all other arguments unchanged."})
	}
	argument("Query.invoice", map[string]string{"entityId": "Entity ID; must match the configured entity.", "invoiceId": "Stable invoice API identifier."})
	argument("Query.globalSearch", map[string]string{"entityId": "Entity ID; supplied by the server.", "query": "Search text. Matches are not a complete financial population.", "limit": "Maximum returned matches, from 1 to 200."})
	for _, key := range []string{"Entity.instrumentBalances", "Entity.transactions", "Entity.contacts", "Entity.invoices", "Entity.approvalRecords", "Entity.userMemberships", "CardAccount.cards"} {
		argument(key, map[string]string{"first": "Page size from 1 to 200; defaults to 100 in business tools.", "after": "Opaque next-page cursor; retain the original filters and window."})
	}
	argument("Entity.analysisSettledMovements", map[string]string{
		"settledFrom": "Inclusive settlement-window start timestamp in RFC3339; resolve the user's timezone explicitly.", "settledTo": "Exclusive settlement-window end timestamp in RFC3339.",
		"first": "Page size from 1 to 200.", "after": "Opaque nextPageToken from the prior page; preserve all filters and the time window.",
		"includeTreasury":         "Whether to include Treasury movements. Retain the returned includesTreasury value when reporting.",
		"includeSpam":             "Whether to include spam movements. Retain the returned includesSpam value when reporting.",
		"excludeInternalActivity": "Excludes every posting of convert (including swaps), internal_transfer, treasury_deposit, treasury_withdraw, add_smart_balance and withdraw_smart_balance, including their fees and refunds. Retain returned exclusions and counts.",
		"excludeSmallBalances":    "Excludes every posting of parents classified below USD 1. Parents with unavailable classification remain included; preserve the returned count.",
		"transactionIds":          "Optional exact parent transaction IDs. Omitted/null leaves the window unrestricted; an empty array selects no transactions. One parent can contain multiple movements.",
	})
	return result
}

func scale(n int) *int { return &n }

// ValidateContracts fails startup if the generated schema and reviewed contract drift.
func ValidateContracts(sdlFields map[string]string) error {
	for key, typ := range sdlFields {
		c, ok := PublicFields[key]
		if !ok || strings.TrimSpace(c.Description) == "" || c.Evidence == "" {
			return fmt.Errorf("public field %s has no reviewed semantic contract", key)
		}
		if c.Unit != "" && typ != "String" {
			return fmt.Errorf("numeric contract %s requires a String scalar", key)
		}
		if c.Unit != "" {
			switch c.Unit {
			case "usd_minor_units", "usd_dollars", "currency_minor_units", "currency_units", "asset_units", "instrument_smallest_units", "decimal_fraction":
			default:
				return fmt.Errorf("unsupported unit contract for %s", key)
			}
			if (c.Scale == nil) == (c.ScaleField == "") {
				return fmt.Errorf("%s requires exactly one fixed or companion scale", key)
			}
		}
	}
	return nil
}
