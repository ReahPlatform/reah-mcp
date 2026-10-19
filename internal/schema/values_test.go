package schema

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/vektah/gqlparser/v2"
	"github.com/vektah/gqlparser/v2/validator"
)

func readPlan(t *testing.T, query string, vars map[string]any) *ReadPlan {
	t.Helper()
	s, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	doc, errs := gqlparser.LoadQuery(s, query)
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	coerced, err := validator.VariableValues(s, doc.Operations[0], vars)
	if err != nil {
		t.Fatal(err)
	}
	p, err := Plan(s, doc, coerced)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestExactFinancialUnitsAndBusinessMeaning(t *testing.T) {
	query := `query($id:ID!){e:entity(id:$id){
   totalBalance{usdAmount}
   treausryPositionAccounts{currentValueInUSD currentValueInAssets vault{apy underlyingInstrument{asset{symbol}}}}
   treausryPositionAccountSummary{averageAPY}
   invoices{invoices{amountSummary{currency amountDue amountDueUsd}}}
   analysisSettledMovements(settledFrom:"2026-10-01T00:00:00Z",settledTo:"2026-11-01T00:00:00Z"){movements{signedAmountSmallest instrumentDecimals assetSymbol usdValue{amountMinor currency scale}}}
  }
 }`
	plan := readPlan(t, query, map[string]any{"id": "entity-a"})
	raw := json.RawMessage(`{"e":{
  "totalBalance":{"usdAmount":"9007199254740993.123456789"},
  "treausryPositionAccounts":[{"currentValueInUSD":"10000","currentValueInAssets":"1.234567890123456789","vault":{"apy":"-0.0125","underlyingInstrument":{"asset":{"symbol":"ETH"}}}}],
  "treausryPositionAccountSummary":{"averageAPY":"0.04"},
  "invoices":{"invoices":[{"amountSummary":{"currency":"EUR","amountDue":"100.00","amountDueUsd":"109.23"}},{"amountSummary":{"currency":"USD","amountDue":"0","amountDueUsd":""}}]},
  "analysisSettledMovements":{"movements":[{"signedAmountSmallest":"-1234567","instrumentDecimals":6,"assetSymbol":"USDC","usdValue":{"amountMinor":"-123","currency":"USD","scale":2}},{"signedAmountSmallest":"1","instrumentDecimals":18,"assetSymbol":"ETH","usdValue":null}]}
 }}`)
	before := string(raw)
	values, _, err := plan.Normalize(raw)
	if err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]string{
		"/e/totalBalance/usdAmount":                                    "90071992547409.93123456789",
		"/e/treausryPositionAccounts/0/currentValueInUSD":              "100",
		"/e/treausryPositionAccounts/0/currentValueInAssets":           "1.234567890123456789",
		"/e/treausryPositionAccounts/0/vault/apy":                      "-0.0125",
		"/e/treausryPositionAccountSummary/averageAPY":                 "0.04",
		"/e/invoices/invoices/0/amountSummary/amountDue":               "100",
		"/e/invoices/invoices/0/amountSummary/amountDueUsd":            "109.23",
		"/e/analysisSettledMovements/movements/0/signedAmountSmallest": "-1.234567",
		"/e/analysisSettledMovements/movements/0/usdValue/amountMinor": "-1.23",
		"/e/analysisSettledMovements/movements/1/signedAmountSmallest": "0.000000000000000001",
	} {
		v, ok := values[path]
		if !ok || v.Amount == nil || *v.Amount != want {
			t.Errorf("%s = %#v; want %s", path, v, want)
		}
	}
	for path, want := range map[string]string{"/e/treausryPositionAccounts/0/vault/apy": "-1.25", "/e/treausryPositionAccountSummary/averageAPY": "4"} {
		v := values[path]
		if v.Percent == nil || *v.Percent != want {
			t.Errorf("%s percent = %#v", path, v)
		}
	}
	if v := values["/e/invoices/invoices/1/amountSummary/amountDueUsd"]; v.Status != "unavailable" || v.Amount != nil {
		t.Fatalf("legacy empty value became money: %#v", v)
	}
	if _, ok := values["/e/analysisSettledMovements/movements/1/usdValue/amountMinor"]; ok {
		t.Fatal("missing valuation became money")
	}
	if string(raw) != before {
		t.Fatal("raw evidence was changed")
	}
	if values["/e/treausryPositionAccounts/0/currentValueInAssets"].Currency != "ETH" {
		t.Fatal("lost underlying asset")
	}
	if values["/e/invoices/invoices/0/amountSummary/amountDue"].Currency != "EUR" {
		t.Fatal("lost invoice currency")
	}
}

func TestAliasesFragmentsMergedFieldsAndDirectives(t *testing.T) {
	plan := readPlan(t, `query($yes:Boolean!){company:entity(id:"entity-a"){
  analysisSettledMovements(settledFrom:"2026-10-01T00:00:00Z",settledTo:"2026-11-01T00:00:00Z"){
   rows:movements{v:usdValue{...Money} v:usdValue{places:scale code:currency}}
  }
  totalBalance{keep:usdAmount skipped:usdAmount @include(if:$yes)}
 }} fragment Money on SettledMovementMoney{value:amountMinor}`, map[string]any{"yes": false})
	values, _, err := plan.Normalize(json.RawMessage(`{"company":{"analysisSettledMovements":{"rows":[{"v":{"value":"12345","places":3,"code":"USD"}}]},"totalBalance":{"keep":"10000"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := values["/company/analysisSettledMovements/rows/0/v/value"]; got.Amount == nil || *got.Amount != "12.345" {
		t.Fatalf("wrong aliased scaled amount: %#v", got)
	}
	for _, field := range plan.Metadata {
		if strings.Contains(field.Path, "skipped") {
			t.Fatal("skipped field was described")
		}
	}
}

func TestNativeBalanceIsAlreadyInMajorAssetUnits(t *testing.T) {
	plan := readPlan(t, `{entity(id:"entity-a"){instrumentBalances{balances{n:nativeAmount a:aggregation{kind:__typename ... on Asset{ticker:symbol} ... on Instrument{asset{ticker:symbol}}}}}}}`, nil)
	values, _, err := plan.Normalize(json.RawMessage(`{"entity":{"instrumentBalances":{"balances":[{"n":"1.234567890123456789","a":{"kind":"Instrument","asset":{"ticker":"ETH"}}},{"n":"2.5","a":{"kind":"Asset","ticker":"BTC"}},{"n":null,"a":{"kind":"Total"}}]}}}`))
	if err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]string{"/entity/instrumentBalances/balances/0/n": "1.234567890123456789", "/entity/instrumentBalances/balances/1/n": "2.5"} {
		v := values[path]
		if v.Amount == nil || *v.Amount != want {
			t.Fatalf("native balance rescaled: %#v", v)
		}
	}
	if values["/entity/instrumentBalances/balances/2/n"].Status != "null" {
		t.Fatal("null became zero")
	}
}

func TestUnitCompanionsCannotBeOmittedOrSkipped(t *testing.T) {
	s, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{
		`{entity(id:"entity-a"){analysisSettledMovements(settledFrom:"2026-10-01T00:00:00Z",settledTo:"2026-11-01T00:00:00Z"){movements{usdValue{amountMinor scale}}}}}`,
		`{entity(id:"entity-a"){analysisSettledMovements(settledFrom:"2026-10-01T00:00:00Z",settledTo:"2026-11-01T00:00:00Z"){movements{usdValue{amountMinor scale currency @skip(if:true)}}}}}`,
		`{entity(id:"entity-a"){instrumentBalances{balances{nativeAmount}}}}`,
		`{entity(id:"entity-a"){treausryPositionAccounts{currentValueInAssets}}}`,
		`{entity(id:"entity-a"){invoices{invoices{amountSummary{amountDue}}}}}`,
	} {
		doc, errs := gqlparser.LoadQuery(s, query)
		if len(errs) > 0 {
			t.Fatal(errs)
		}
		if _, err := Plan(s, doc, nil); err == nil {
			t.Errorf("accepted missing unit companion: %s", query)
		}
	}
}

func TestUpstreamDriftIsNotPublished(t *testing.T) {
	plan := readPlan(t, `{entity(id:"entity-a"){analysisSettledMovements(settledFrom:"2026-10-01T00:00:00Z",settledTo:"2026-11-01T00:00:00Z"){movements{usdValue{amountMinor currency scale}}}}}`, nil)
	for _, money := range []string{
		`{"amountMinor":"123","currency":null,"scale":2}`,
		`{"amountMinor":123,"currency":"USD","scale":2}`,
		`{"amountMinor":"1.23","currency":"USD","scale":2}`,
		`{"amountMinor":"NaN","currency":"USD","scale":2}`,
		`{"amountMinor":"123","currency":"USD","scale":null}`,
		`{"amountMinor":"123","currency":"USD","scale":-1}`,
		`{"amountMinor":"123","currency":"USD","scale":2,"secretAmount":"999"}`,
	} {
		data := `{"entity":{"analysisSettledMovements":{"movements":[{"usdValue":` + money + `}]}}}`
		if _, _, err := plan.Normalize(json.RawMessage(data)); err == nil {
			t.Errorf("published uninterpretable upstream data: %s", data)
		}
	}
	plan = readPlan(t, `{entity(id:"entity-a"){id state}}`, nil)
	for _, data := range []string{`{"entity":{"id":{"unreviewed":"data"}}}`, `{"entity":{"state":"FUTURE_STATE"}}`} {
		if _, _, err := plan.Normalize(json.RawMessage(data)); err == nil {
			t.Errorf("published unreviewed shape: %s", data)
		}
	}
	plan = readPlan(t, `{entity(id:"entity-a"){id @skip(if:true)}}`, nil)
	if _, _, err := plan.Normalize(json.RawMessage(`{"entity":{"unreviewedAmount":"999"}}`)); err == nil {
		t.Fatal("object with no active selections leaked an unreviewed field")
	}
	plan = readPlan(t, `{entity(id:"entity-a"){__typename}}`, nil)
	if _, _, err := plan.Normalize(json.RawMessage(`{"entity":{"__typename":"Vault"}}`)); err == nil {
		t.Fatal("inconsistent concrete type was published")
	}
}

func TestUnreviewedFieldsAreAbsentFromPublicSchemaAndDiscovery(t *testing.T) {
	s, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, pair := range [][2]string{{"Vault", "tvlUsd"}, {"TreasuryPositionAccount", "sharePrice"}, {"Card", "cardDetail"}, {"Entity", "transactionStats"}, {"Asset", "latestPrice"}, {"CardTransaction", "amountMinor"}, {"TreasuryPositionAccount", "currentAPY"}, {"AssetConnection", "cursor"}} {
		if s.Types[pair[0]].Fields.ForName(pair[1]) != nil {
			t.Errorf("unreviewed field is public: %v", pair)
		}
		description, err := Describe(s, pair[0])
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(description, pair[1]+":") {
			t.Errorf("unreviewed field is discoverable: %v", pair)
		}
	}
	if s.Types["AssetPrice"] != nil {
		t.Fatal("unreviewed type is packaged")
	}
	for _, query := range []string{`{entity(id:"entity-a"){treausryPositionAccounts{sharePrice}}}`, `{entity(id:"entity-a"){assets{assets{latestPrice{usdPrice}}}}}`, `{cardTransactions(entityId:"entity-a"){transactions{amountMinor currency}}}`, `{entity(id:"entity-a"){treausryPositionAccounts{currentAPY}}}`, `{entity(id:"entity-a"){assets(first:1){assets{id}}}}`, `{entity(id:"entity-a"){assets(after:"cursor"){assets{id}}}}`} {
		if _, errs := gqlparser.LoadQuery(s, query); len(errs) == 0 {
			t.Errorf("query accepted removed field: %s", query)
		}
	}
	definition, err := Describe(s, "Entity")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(definition, "first: Int = 100") || !strings.Contains(definition, "argument settledFrom:") {
		t.Fatal("argument documentation or defaults were lost")
	}
	definition, err = Describe(s, "Query")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(definition, "__schema") {
		t.Fatal("introspection field was advertised")
	}
}

func TestExactDecimalShift(t *testing.T) {
	for _, tc := range []struct {
		raw   string
		scale int
		want  string
	}{
		{"6172.8395061728392079", 2, "61.728395061728392079"}, {"10000", 2, "100"}, {"1", 18, "0.000000000000000001"}, {"-0.0125", -2, "-1.25"}, {"-000.000", 2, "0"}, {"00010.50", 0, "10.5"},
	} {
		got, err := shiftDecimal(tc.raw, tc.scale)
		if err != nil || got != tc.want {
			t.Errorf("%s / 10^%d = %s %v; want %s", tc.raw, tc.scale, got, err, tc.want)
		}
	}
	for _, raw := range []string{"NaN", "Infinity", "1e3", "", " 100 ", "1,000", "--1", ".5"} {
		if _, err := shiftDecimal(raw, 2); err == nil {
			t.Errorf("accepted malformed decimal %q", raw)
		}
	}
}

func TestTreasurySourceCoverageWithAliasesAndFragments(t *testing.T) {
	plan := readPlan(t, `query($include:Boolean!){company:entity(id:"entity-a"){
  ...Positions
  totals:treausryPositionAccountSummary{value:currentValueInUSD}
  skipped:treausryPositionAccounts @skip(if:$include){id}
 }} fragment Positions on Entity{positions:treausryPositionAccounts{id}}`, map[string]any{"include": true})
	for _, count := range []int{0, 1, 199, 200, 201} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			positions := make([]map[string]string, count)
			for i := range positions {
				positions[i] = map[string]string{"id": fmt.Sprintf("position-%d", i)}
			}
			raw, err := json.Marshal(map[string]any{"company": map[string]any{
				"positions": positions, "totals": map[string]string{"value": "2000000"},
			}})
			if err != nil {
				t.Fatal(err)
			}
			values, coverage, err := plan.Normalize(raw)
			if count > 200 {
				if err == nil {
					t.Fatal("published data beyond the reviewed source limit")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			want := "complete"
			if count == 200 {
				want = "not_proven"
			}
			list := coverage["/company/positions"]
			if list.Status != want || list.SourceLimit != 200 || list.ReturnedItems == nil || *list.ReturnedItems != count || list.Reason == "" {
				t.Fatalf("incorrect list coverage: %#v", list)
			}
			summary := coverage["/company/totals"]
			if summary.Status != "not_proven" || summary.SourceLimit != 200 || summary.ReturnedItems != nil || len(coverage) != 2 {
				t.Fatalf("summary or skipped coverage was invented: %#v", coverage)
			}
			if v := values["/company/totals/value"]; v.Amount == nil || *v.Amount != "20000" {
				t.Fatalf("coverage changed the raw aggregate conversion: %#v", v)
			}
		})
	}
	for _, raw := range []string{
		`{"company":{"positions":null,"totals":null}}`,
		`{"company":{"positions":[null],"totals":null}}`,
	} {
		_, coverage, err := plan.Normalize(json.RawMessage(raw))
		if err != nil {
			t.Fatal(err)
		}
		if coverage["/company/positions"].Status == "complete" || coverage["/company/totals"].Status != "unavailable" {
			t.Fatalf("null data was treated as complete: %#v", coverage)
		}
	}
	for _, f := range plan.Metadata {
		if (f.SourceField == "Entity.treausryPositionAccounts" || f.SourceField == "Entity.treausryPositionAccountSummary") && f.SourceLimit != 200 {
			t.Fatalf("source limit missing from field semantics: %#v", f)
		}
	}
}
