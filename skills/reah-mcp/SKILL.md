---
name: reah-mcp
description: Analyze Reah financial data for balance reviews, cashflow analysis, spending reviews, invoice follow-up, and treasury reporting.
metadata:
  tools: reah_catalog,reah_describe_type,reah_query,reah_get_entity,reah_list_balances,reah_list_wallets,reah_list_banking_accounts,reah_list_transactions,reah_list_settled_movements,reah_list_contacts,reah_list_invoices,reah_list_approvals,reah_list_card_accounts,reah_list_card_transactions,reah_list_treasury_positions,reah_list_statements,reah_list_members,reah_list_assets,reah_search
---

Use the connected Reah MCP server to answer financial questions within its
available entity scope. Establish the reporting period, timezone, and comparison
currency from the request and available context before aggregating data.

Choose the data source for the business question:

- For balance and allocation reviews, reconcile relevant wallet, bank, card,
  and treasury holdings. Avoid adding overlapping account and asset totals;
  distinguish total holdings from funds available to spend.
- For cashflow, burn, or period receipts, use settled movements. Transaction
  activity can include pending, failed, or internal activity and does not by
  itself establish settled cashflow.
- For spending reviews, use card transactions and the relevant account context.
- For invoice follow-up, distinguish outstanding obligations from actual cash
  received. For treasury reviews, distinguish position performance from current
  product yield using the measures documented by the tools.

Use search to locate relevant records, then read the underlying data for
analysis. Complete all relevant pages with the same filters and reporting window
before claiming full coverage. If data is partial, state which accounts, periods,
or records the analysis covers.

Use the MCP tools' published field descriptions and returned normalized values
for units and calculations. Consult `reah_catalog` and `reah_describe_type` when
additional fields or query options are needed.

Present conclusions with the reporting period, currency, data timestamp, and
material exclusions. Explain missing valuations or partial results instead of
treating unavailable data as zero. Keep observed facts distinct from derived
calculations, and state any assumptions that affect the conclusion.
