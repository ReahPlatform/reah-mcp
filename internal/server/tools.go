package server

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/ReahPlatform/reah-mcp/internal/gateway"
	readschema "github.com/ReahPlatform/reah-mcp/internal/schema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/vektah/gqlparser/v2"
	"github.com/vektah/gqlparser/v2/ast"
)

type Surface struct {
	Name        string
	Field       string
	Root        bool
	Description string
	Selection   string
}

// Each surface uses only reviewed public fields. Exact unit conversions are
// attached separately from raw upstream data; they do not imply spendability.
var Surfaces = []Surface{
	{"reah_get_entity", "entity", true, "Current entity identity and point-in-time balance.", "id name type state totalBalance { usdAmount priceVersion }"},
	{"reah_list_balances", "instrumentBalances", false, "Balance page by instrument. USD raw values are cents; use normalized_values for major-unit amounts. Follow cursor.", "balances { nativeAmount usdAmount priceVersion aggregation { __typename ... on Asset { id symbol } ... on Instrument { id decimals asset { id symbol } } } } cursor"},
	{"reah_list_wallets", "wallets", false, "Wallet roster and balances.", "id humanId name state totalBalance { usdAmount }"},
	{"reah_list_banking_accounts", "bankingAccounts", false, "Banking account roster and balances.", "id humanId name state provider totalBalance { usdAmount }"},
	{"reah_list_transactions", "transactions", false, "Transaction page, including lifecycle state. This is not a settled cashflow ledger.", "transactions { id humanId type state initiatedAt finishedAt notes counterpartyTexts } cursor"},
	{"reah_list_settled_movements", "analysisSettledMovements", false, "Settled posting facts for cashflow analysis. Requires settledFrom and settledTo RFC3339 timestamps in arguments. Preserve coverage and exclusions; follow nextPageToken.", "movements { movementId transactionId transactionType transactionState settledAt accountId instrumentId assetSymbol signedAmountSmallest instrumentDecimals role usdValue { amountMinor currency scale } valuationStatus } nextPageToken dataAsOf balanceEntryIdWatermark priceVersionWatermark coverage { firstObservedSettledAt lastObservedSettledAt ingestionCoverageProven coverageNote unpostedPartialTransactionCount unpostedPartialTransactionHumanIds } includesTreasury includesSpam transactionIds excludesInternalActivity excludesSmallBalances excludedSmallBalanceMovementCount unknownSmallBalanceParentCount excludedTreasuryMovementCount excludedInternalActivityMovementCount excludedSpamMovementCount"},
	{"reah_list_contacts", "contacts", false, "Contact roster; use fields to select details or transactions.", "contacts { id name type } cursor"},
	{"reah_list_invoices", "invoices", false, "Invoice page with status and dates; unpaid invoices do not prove cash received.", "invoices { id invoiceNumber status customerName issueDate dueDate } cursor"},
	{"reah_list_approvals", "approvalRecords", false, "Approval records and current state. No approval decisions or execution.", "records { summary { id humanId status type initiatedAt completedAt } } cursor"},
	{"reah_list_card_accounts", "cardAccounts", false, "Card accounts, balances and a first page of masked cards. Follow each nested cards.cursor using a custom fields selection. PAN/CVV reads are excluded.", "id totalBalance { usdAmount } cards(first:100) { cards { id nickname last4 status limitAmountMinor limitFrequency } cursor }"},
	{"reah_list_card_transactions", "cardTransactions", true, "Card activity metadata scoped to the configured entity, including USD spending and USDC funding activity. Transaction amounts are not exposed because the upstream field mixes unit sources without an explicit scale.", "transactions { transactionId merchantName currency transactionType uiStatus initiatedAt } cursor"},
	{"reah_list_treasury_positions", "treausryPositionAccounts", false, "At most the latest 200 matching Treasury positions, ordered by creation time and ID descending, without continuation. Check read_coverage before claiming completeness. USD raw values are cents. vault.apy is current product APY; use normalized_values.percent. Position currentAPY is not exposed because upstream zero values conflate unavailable and nonpositive returns.", "id entityId currentValueInAssets currentValueInUSD investedInUSD unrealizedProfitInUSD vault { id name apy underlyingInstrument { id decimals asset { id symbol } } }"},
	{"reah_list_statements", "statementExports", true, "Existing statement export tasks and metadata only. Does not generate exports or download URLs.", "tasks { id status fileName rowCount createdAt completedAt } cursor"},
	{"reah_list_members", "userMemberships", false, "Entity member page.", "memberships { user { id name email } joinedAt } cursor"},
	{"reah_list_assets", "assets", false, "Full entity asset catalog, without pagination arguments or a cursor. Specific instrument decimals take precedence over asset defaults.", "assets { id name symbol fiat defaultDecimals }"},
	{"reah_search", "globalSearch", true, "Search permitted accounts, transactions, approvals and contacts. Requires query in arguments; results are search matches, not complete rosters.", "query total groups { type items { __typename ... on Wallet { id name } ... on BankingAccount { id name } ... on Transaction { transactionId: id humanId type state } ... on Contact { id name } ... on ApprovalRecordDetail { summary { id humanId status } } ... on BlockchainAccount { id } ... on InstrumentAccount { id } } }"},
}

type ReadInput struct {
	Fields    string         `json:"fields,omitempty" jsonschema:"Optional GraphQL field selection inside the result, without outer braces. Defaults to a concise business selection. Use reah_describe_type first for additional fields and units."`
	Arguments map[string]any `json:"arguments,omitempty" jsonschema:"GraphQL arguments for this surface, e.g. first=100, after=cursor, settledFrom and settledTo. Entity scope is server-controlled. Use reah_describe_type to discover argument types."`
}

type QueryInput struct {
	Query     string         `json:"query" jsonschema:"Exactly one GraphQL query against the read schema. Declare $entityId: ID! and use it for every root entity argument; its value is supplied by the server."`
	Variables map[string]any `json:"variables,omitempty" jsonschema:"JSON variables. The server supplies entityId; it cannot be overridden."`
}

type DescribeInput struct {
	Type string `json:"type" jsonschema:"Exact GraphQL type name. Start with Query, Entity, or a return type from the catalog."`
}

func tool(name, description string) *mcp.Tool {
	return &mcp.Tool{Name: name, Description: description, Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true}}
}

func New(client *gateway.Client) (*mcp.Server, error) {
	s := mcp.NewServer(&mcp.Implementation{Name: "reah-mcp", Version: "0.1.0"}, &mcp.ServerOptions{Instructions: "Read-only Reah financial data, bound to one configured user credential and entity. Only reviewed fields are exposed. Card transaction amounts and position currentAPY are absent because their units or availability cannot be unambiguously established. Every read includes field_semantics and exact normalized_values keyed by JSON Pointer. Use normalized_values.amount and currency for display/calculation; raw data stays in upstream units. USD balance and Treasury raw amounts are cents; invoice amountDueUsd is already dollars. APYs are decimal fractions; normalized_values.percent is already multiplied by 100. Never convert normalized amounts again. Start with reah_catalog and reah_describe_type; select all required currency/scale companions for custom queries. Follow every available cursor and check read_coverage before claiming complete coverage. Treasury positions and summaries cover at most the latest 200 matching positions; a full list batch or any summary alone cannot prove completeness. Assets return a full catalog without pagination. Do not sum overlapping balance views or substitute transaction lists for settled cashflow. Missing/null/legacy unavailable data is never zero. Unit conversion does not establish spendability or evidence completeness."})
	mcp.AddTool(s, tool("reah_catalog", "List available read surfaces, GraphQL return types and configured entity."), func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, any, error) {
		var entries []map[string]any
		for _, surface := range Surfaces {
			parent := client.Schema.Types["Entity"]
			if surface.Root {
				parent = client.Schema.Query
			}
			f := parent.Fields.ForName(surface.Field)
			entries = append(entries, map[string]any{"tool": surface.Name, "field": surface.Field, "parent_type": parent.Name, "return_type": f.Type.String(), "description": surface.Description})
		}
		return nil, map[string]any{"entity_id": client.EntityID, "surfaces": entries, "value_contract": "Raw data preserves upstream units. field_semantics documents every selected field. normalized_values uses JSON Pointer paths, exact decimal strings, explicit units/currency/scale and APY percentages. Never apply a second conversion. Only reviewed fields are discoverable and queryable. read_coverage describes bounded source completeness separately from normalized_values; source limits also appear in field_semantics."}, nil
	})
	mcp.AddTool(s, tool("reah_describe_type", "Describe a local pinned read schema type, its fields, arguments and available unit documentation. Makes no upstream request."), func(_ context.Context, _ *mcp.CallToolRequest, in DescribeInput) (*mcp.CallToolResult, any, error) {
		text, err := readschema.Describe(client.Schema, in.Type)
		if err != nil {
			return nil, nil, err
		}
		contracts := map[string]readschema.FieldContract{}
		for _, field := range client.Schema.Types[in.Type].Fields {
			key := in.Type + "." + field.Name
			if contract, ok := readschema.PublicFields[key]; ok {
				contracts[key] = contract
			}
		}
		return nil, map[string]any{"definition": text, "field_semantics": contracts}, err
	})
	mcp.AddTool(s, tool("reah_query", "Execute one validated, entity-bound query across the permitted read surfaces. Returns raw data, field_semantics, exact normalized_values and read_coverage for source-bounded fields. Mutations, subscriptions and fields without reviewed contracts are unavailable."), func(ctx context.Context, _ *mcp.CallToolRequest, in QueryInput) (*mcp.CallToolResult, any, error) {
		return call(ctx, client, in.Query, in.Variables)
	})
	for _, surface := range Surfaces {
		// Validate each default selection against the pinned schema at startup.
		query, _, err := BuildQuery(client.Schema, surface, ReadInput{})
		if err != nil {
			return nil, err
		}
		doc, errs := gqlparser.LoadQuery(client.Schema, query)
		if len(errs) > 0 {
			return nil, fmt.Errorf("%s default query: %s", surface.Name, errs.Error())
		}
		if _, err := readschema.Plan(client.Schema, doc, nil); err != nil {
			return nil, fmt.Errorf("%s default contract: %w", surface.Name, err)
		}
		mcp.AddTool(s, tool(surface.Name, surface.Description+" Results include field_semantics, exact normalized_values and read_coverage for source-bounded fields; use normalized_values.amount and currency, and do not convert twice."), func(ctx context.Context, _ *mcp.CallToolRequest, in ReadInput) (*mcp.CallToolResult, any, error) {
			query, vars, err := BuildQuery(client.Schema, surface, in)
			if err != nil {
				return nil, nil, err
			}
			return call(ctx, client, query, vars)
		})
	}
	return s, nil
}

func call(ctx context.Context, client *gateway.Client, query string, vars map[string]any) (*mcp.CallToolResult, any, error) {
	response, err := client.Query(ctx, query, vars)
	if err != nil {
		return nil, nil, err
	}
	data, err := json.Marshal(response)
	if err != nil {
		return nil, nil, err
	}
	result := &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(data)}}}
	if len(response.Errors) > 0 && string(response.Errors) != "null" && string(response.Errors) != "[]" {
		result.IsError = true
	}
	return result, nil, nil
}

// BuildQuery derives argument variable types from the pinned GraphQL schema.
// Values never enter query text. The complete result still passes Prepare.
func BuildQuery(s *ast.Schema, surface Surface, in ReadInput) (string, map[string]any, error) {
	parent := s.Types["Entity"]
	if surface.Root {
		parent = s.Query
	}
	f := parent.Fields.ForName(surface.Field)
	if f == nil {
		return "", nil, fmt.Errorf("unknown read surface %s", surface.Field)
	}
	vars := map[string]any{}
	for name, value := range in.Arguments {
		vars[name] = value
	}
	scope := ""
	if surface.Root {
		scope = readschema.RootScopes[surface.Field]
	}
	if _, ok := vars[scope]; ok && scope != "" {
		return "", nil, fmt.Errorf("%s is server-controlled", scope)
	}
	if _, ok := vars["entityId"]; ok {
		return "", nil, fmt.Errorf("entityId is server-controlled")
	}
	if f.Arguments.ForName("first") != nil {
		if _, ok := vars["first"]; !ok {
			vars["first"] = 100
		}
	}
	var names []string
	for k := range vars {
		names = append(names, k)
	}
	sort.Strings(names)
	defs := []string{"$entityId: ID!"}
	var args []string
	if scope != "" {
		args = append(args, scope+": $entityId")
	}
	for _, name := range names {
		a := f.Arguments.ForName(name)
		if a == nil {
			return "", nil, fmt.Errorf("unknown argument %q on %s", name, surface.Field)
		}
		defs = append(defs, "$"+name+": "+a.Type.String())
		args = append(args, name+": $"+name)
	}
	// Declare omitted required arguments so startup can validate the selection;
	// VariableValues reports missing values when the actual tool is called.
	for _, a := range f.Arguments {
		if a.Name != scope && a.Type.NonNull && a.DefaultValue == nil {
			if _, ok := vars[a.Name]; !ok {
				defs = append(defs, "$"+a.Name+": "+a.Type.String())
				args = append(args, a.Name+": $"+a.Name)
			}
		}
	}
	selection := in.Fields
	if strings.TrimSpace(selection) == "" {
		selection = surface.Selection
	}
	field := surface.Field
	if len(args) > 0 {
		field += "(" + strings.Join(args, ", ") + ")"
	}
	field += " { " + selection + " }"
	if !surface.Root {
		field = "entity(id: $entityId) { " + field + " }"
	}
	query := "query ReahRead(" + strings.Join(defs, ", ") + ") { " + field + " }"
	return query, vars, nil
}
