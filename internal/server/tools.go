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

// Each surface is a raw upstream read; it does not claim the agent's normalized
// units, financial policies or evidence-admission guarantees.
var Surfaces = []Surface{
	{"reah_get_entity", "entity", true, "Current entity identity and point-in-time balance.", "id name type state totalBalance { usdAmount priceVersion }"},
	{"reah_list_balances", "instrumentBalances", false, "Balance page by instrument; preserve native and USD units and follow cursor.", "balances { nativeAmount usdAmount priceVersion aggregation { __typename ... on Instrument { id } } } cursor"},
	{"reah_list_wallets", "wallets", false, "Wallet roster and balances.", "id humanId name state totalBalance { usdAmount }"},
	{"reah_list_banking_accounts", "bankingAccounts", false, "Banking account roster and balances.", "id humanId name state provider totalBalance { usdAmount }"},
	{"reah_list_transactions", "transactions", false, "Transaction page, including lifecycle state. This is not a settled cashflow ledger.", "transactions { id humanId type state initiatedAt finishedAt notes counterpartyTexts } cursor"},
	{"reah_list_settled_movements", "analysisSettledMovements", false, "Settled posting facts for cashflow analysis. Requires settledFrom and settledTo RFC3339 timestamps in arguments. Preserve coverage and exclusions; follow nextPageToken.", "movements { movementId transactionId transactionType transactionState settledAt accountId instrumentId assetSymbol signedAmountSmallest instrumentDecimals role usdValue { amountMinor currency scale } valuationStatus } nextPageToken dataAsOf balanceEntryIdWatermark priceVersionWatermark coverage { firstObservedSettledAt lastObservedSettledAt ingestionCoverageProven coverageNote unpostedPartialTransactionCount unpostedPartialTransactionHumanIds } includesTreasury includesSpam transactionIds excludesInternalActivity excludesSmallBalances excludedSmallBalanceMovementCount unknownSmallBalanceParentCount excludedTreasuryMovementCount excludedInternalActivityMovementCount excludedSpamMovementCount"},
	{"reah_list_contacts", "contacts", false, "Contact roster; use fields to select details or transactions.", "contacts { id name type } cursor"},
	{"reah_list_invoices", "invoices", false, "Invoice page with status and dates; unpaid invoices do not prove cash received.", "invoices { id invoiceNumber status customerName issueDate dueDate } cursor"},
	{"reah_list_approvals", "approvalRecords", false, "Approval records and current state. No approval decisions or execution.", "records { summary { id humanId status type initiatedAt completedAt } } cursor"},
	{"reah_list_card_accounts", "cardAccounts", false, "Card accounts, balances and a first page of masked cards. Follow each nested cards.cursor using a custom fields selection. PAN/CVV reads are excluded.", "id totalBalance { usdAmount } cards(first:100) { cards { id nickname last4 status limitAmountMinor limitFrequency } cursor }"},
	{"reah_list_card_transactions", "cardTransactions", true, "Card transaction page scoped to configured entity.", "transactions { transactionId merchantName amountMinor currency transactionType uiStatus initiatedAt } cursor"},
	{"reah_list_treasury_positions", "treausryPositionAccounts", false, "Treasury positions, values, yield and APY in upstream units.", "id entityId currentValueInAssets currentValueInUSD investedInUSD unrealizedProfitInUSD currentAPY"},
	{"reah_list_statements", "statementExports", true, "Existing statement export tasks and metadata only. Does not generate exports or download URLs.", "tasks { id status fileName rowCount createdAt completedAt } cursor"},
	{"reah_list_members", "userMemberships", false, "Entity member page.", "memberships { user { id name email } joinedAt } cursor"},
	{"reah_list_assets", "assets", false, "Available asset catalog page.", "assets { id } cursor"},
	{"reah_search", "globalSearch", true, "Search permitted accounts, transactions, approvals and contacts. Requires query in arguments; results are search matches, not complete rosters.", "query total groups { type items { __typename ... on Wallet { id name } ... on BankingAccount { id name } ... on Transaction { transactionId: id humanId type state } ... on Contact { id name } ... on ApprovalRecordDetail { summary { id humanId status } } ... on BlockchainAccount { id } ... on InstrumentAccount { id } } }"},
}

type ReadInput struct {
	Fields    string         `json:"fields,omitempty" jsonschema:"Optional GraphQL field selection inside the result, without outer braces. Defaults to a concise business selection. Use reah_describe_type first for additional fields and units."`
	Arguments map[string]any `json:"arguments,omitempty" jsonschema:"GraphQL arguments for this surface, e.g. first=100, after=cursor, filters, settledFrom and settledTo. Entity scope is server-controlled. Use reah_describe_type to discover argument types."`
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
	s := mcp.NewServer(&mcp.Implementation{Name: "reah-mcp", Version: "0.1.0"}, &mcp.ServerOptions{Instructions: "Read-only Reah financial data, bound to one configured user credential and entity. Start with reah_catalog and reah_describe_type. Follow every returned cursor before claiming complete coverage. Keep amounts as decimal strings and preserve raw units. Queries return raw GraphQL data and errors, not agent-normalized evidence. Never treat missing data as zero."})
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
		return nil, map[string]any{"entity_id": client.EntityID, "surfaces": entries}, nil
	})
	mcp.AddTool(s, tool("reah_describe_type", "Describe a local pinned read schema type, its fields, arguments and available unit documentation. Makes no upstream request."), func(_ context.Context, _ *mcp.CallToolRequest, in DescribeInput) (*mcp.CallToolResult, any, error) {
		text, err := readschema.Describe(client.Schema, in.Type)
		return nil, map[string]any{"definition": text}, err
	})
	mcp.AddTool(s, tool("reah_query", "Execute one validated, entity-bound query across the permitted read surfaces. Mutations, subscriptions and unrelated query roots are rejected."), func(ctx context.Context, _ *mcp.CallToolRequest, in QueryInput) (*mcp.CallToolResult, any, error) {
		return call(ctx, client, in.Query, in.Variables)
	})
	for _, surface := range Surfaces {
		// Validate each default selection against the pinned schema at startup.
		query, _, err := BuildQuery(client.Schema, surface, ReadInput{})
		if err != nil {
			return nil, err
		}
		if _, errs := gqlparser.LoadQuery(client.Schema, query); len(errs) > 0 {
			return nil, fmt.Errorf("%s default query: %s", surface.Name, errs.Error())
		}
		mcp.AddTool(s, tool(surface.Name, surface.Description), func(ctx context.Context, _ *mcp.CallToolRequest, in ReadInput) (*mcp.CallToolResult, any, error) {
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
