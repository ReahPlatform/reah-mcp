---
name: reah-mcp
description: Analyze Reah financial data for balance reviews, cashflow analysis, card activity reviews, invoice follow-up, and treasury reporting.
---

Use the connected Reah MCP server to answer financial questions. First get the
authorized businesses with `reah_list_entities`. If only one is available, use
its ID automatically. If several are available, match the user's intended
business by name; ask only when the choice is ambiguous. Pass the selected
`entityId` to each business tool, even for a single-business connection. Keep
businesses separate when comparing or aggregating their results.

Establish the reporting period, timezone, and comparison currency from the
request and available context before aggregating data.

Choose the data source for the business question:

- For balance and allocation reviews, reconcile relevant wallet, bank, card,
  and treasury holdings. Avoid adding overlapping account and asset totals;
  distinguish total holdings from funds available to spend.
- For cashflow, burn, or period receipts, use settled movements. Transaction
  activity can include pending, failed, or internal activity and does not by
  itself establish settled cashflow.
- For card activity reviews, use card transactions and the relevant account
  context. Draw monetary conclusions only from financial measures supplied by
  the tools.
- For invoice follow-up, distinguish outstanding obligations from actual cash
  received. For treasury reviews, distinguish position performance from current
  product yield using the measures documented by the tools.

Use search to locate relevant records, then read the underlying data for
analysis. Complete all relevant pages with the same filters and reporting window
before claiming full coverage. Read coverage metadata: some sources are capped
without continuation. If data is partial, state which accounts, periods, or
records the analysis covers.

Use the MCP tools' published field descriptions and returned normalized values
for units and calculations. Consult `reah_catalog` for the available fixed reads
and their contracts. Never convert normalized values a second time or invent
unsupported fields and query options.

Present conclusions with the reporting period, currency, data timestamp when
available, and material exclusions. Explain missing valuations or partial results
instead of treating unavailable data as zero. Keep observed facts distinct from
derived calculations, and state assumptions that affect the conclusion.
