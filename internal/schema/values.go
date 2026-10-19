package schema

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/vektah/gqlparser/v2/ast"
)

// SelectedField documents a response path, including aliases and array wildcards.
type SelectedField struct {
	Path        string `json:"path"`
	SourceField string `json:"source_field"`
	Type        string `json:"type"`
	FieldContract
}

type NormalizedValue struct {
	Raw      any     `json:"raw"`
	Amount   *string `json:"amount,omitempty"`
	Unit     string  `json:"unit"`
	Currency string  `json:"currency,omitempty"`
	Scale    *int    `json:"source_scale,omitempty"`
	Percent  *string `json:"percent,omitempty"`
	Status   string  `json:"status"`
}

// ReadCoverage describes acquisition of a source-bounded collection or aggregate,
// independently of monetary normalization and external ingestion coverage.
type ReadCoverage struct {
	Status        string `json:"status"`
	SourceLimit   int    `json:"source_limit"`
	ReturnedItems *int   `json:"returned_items,omitempty"`
	Reason        string `json:"reason"`
}

type readField struct {
	field    *ast.Field
	children []*readField
	path     string
	contract FieldContract
}

type ReadPlan struct {
	schema   *ast.Schema
	fields   []*readField
	Metadata []SelectedField
}

// Plan is built from the validated AST. Directives, aliases and named/inline
// fragments participate in the same contract and dependency checks.
func Plan(s *ast.Schema, doc *ast.QueryDocument, vars map[string]any) (*ReadPlan, error) {
	p := &ReadPlan{schema: s}
	var build func(ast.SelectionSet, string) ([]*readField, error)
	build = func(set ast.SelectionSet, path string) ([]*readField, error) {
		var flattened []*ast.Field
		var expand func(ast.SelectionSet) error
		expand = func(selections ast.SelectionSet) error {
			for _, selection := range selections {
				var directives ast.DirectiveList
				switch f := selection.(type) {
				case *ast.Field:
					directives = f.Directives
				case *ast.InlineFragment:
					directives = f.Directives
				case *ast.FragmentSpread:
					directives = f.Directives
				}
				included := true
				for _, d := range directives {
					if d.Name != "skip" && d.Name != "include" {
						continue
					}
					v, err := d.Arguments.ForName("if").Value.Value(vars)
					if err != nil {
						return err
					}
					b, ok := v.(bool)
					if !ok {
						return fmt.Errorf("invalid conditional directive")
					}
					if (d.Name == "skip" && b) || (d.Name == "include" && !b) {
						included = false
					}
				}
				if !included {
					continue
				}
				switch f := selection.(type) {
				case *ast.Field:
					flattened = append(flattened, f)
				case *ast.InlineFragment:
					if err := expand(f.SelectionSet); err != nil {
						return err
					}
				case *ast.FragmentSpread:
					if err := expand(f.Definition.SelectionSet); err != nil {
						return err
					}
				}
			}
			return nil
		}
		if err := expand(set); err != nil {
			return nil, err
		}
		// GraphQL merges repeated selections, including fields split across
		// fragments. Merge before checking currency and scale companions.
		var merged []*ast.Field
		indices := map[string]int{}
		for _, f := range flattened {
			key := f.ObjectDefinition.Name + "." + f.Alias
			if i, ok := indices[key]; ok {
				merged[i].SelectionSet = append(merged[i].SelectionSet, f.SelectionSet...)
			} else {
				copy := *f
				copy.SelectionSet = append(ast.SelectionSet(nil), f.SelectionSet...)
				indices[key] = len(merged)
				merged = append(merged, &copy)
			}
		}
		var out []*readField
		for _, f := range merged {
			key := f.ObjectDefinition.Name + "." + f.Name
			c, ok := PublicFields[key]
			if f.Name == "__typename" {
				c = FieldContract{Description: "Concrete GraphQL type of this object; used to interpret union and interface fields.", Evidence: "graphql:__typename"}
				ok = true
			}
			if !ok {
				return nil, fmt.Errorf("field %s has no public semantic contract", key)
			}
			next := path + "/" + f.Alias
			node := &readField{field: f, path: next, contract: c}
			childPath := next
			if f.Definition.Type.Elem != nil {
				childPath += "/*"
			}
			children, err := build(f.SelectionSet, childPath)
			if err != nil {
				return nil, err
			}
			node.children = children
			resultType := s.Types[f.Definition.Type.Name()]
			if resultType != nil && resultType.IsAbstractType() && findField(children, "__typename") == nil {
				return nil, fmt.Errorf("%s requires __typename to identify its concrete type", next)
			}
			out = append(out, node)
		}
		for _, node := range out {
			for dependency, typ := range node.contract.Requires {
				dependencyNode := findField(out, dependency)
				if dependencyNode == nil || dependencyNode.field.Definition.Type.Name() != typ {
					return nil, fmt.Errorf("%s requires selecting sibling %s (%s)", node.path, dependency, typ)
				}
			}
			if node.contract.CurrencyField == "aggregation" {
				aggregation := findField(out, "aggregation")
				if aggregation == nil || findField(aggregation.children, "__typename") == nil || findField(aggregation.children, "symbol") == nil || findField(aggregation.children, "asset.symbol") == nil {
					return nil, fmt.Errorf("%s requires aggregation { __typename ... on Asset { symbol } ... on Instrument { asset { symbol } } }", node.path)
				}
			}
		}
		return out, nil
	}
	var err error
	p.fields, err = build(doc.Operations[0].SelectionSet, "")
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var collect func([]*readField)
	collect = func(fields []*readField) {
		for _, node := range fields {
			key := node.path + "@" + node.field.ObjectDefinition.Name + "." + node.field.Name
			if !seen[key] {
				seen[key] = true
				p.Metadata = append(p.Metadata, SelectedField{Path: node.path, SourceField: node.field.ObjectDefinition.Name + "." + node.field.Name, Type: node.field.Definition.Type.String(), FieldContract: node.contract})
			}
			collect(node.children)
		}
	}
	collect(p.fields)
	sort.Slice(p.Metadata, func(i, j int) bool {
		a, b := p.Metadata[i], p.Metadata[j]
		return a.Path+"@"+a.SourceField < b.Path+"@"+b.SourceField
	})
	return p, nil
}

func findField(fields []*readField, path string) *readField {
	name, rest, nested := strings.Cut(path, ".")
	for _, f := range fields {
		if f.field.Name == name {
			if !nested {
				return f
			}
			if child := findField(f.children, rest); child != nil {
				return child
			}
		}
	}
	return nil
}

func sourceValue(object map[string]json.RawMessage, fields []*readField, path string) (json.RawMessage, bool) {
	name, rest, nested := strings.Cut(path, ".")
	for _, f := range fields {
		if f.field.Name != name {
			continue
		}
		raw, ok := object[f.field.Alias]
		if !ok {
			continue
		}
		if !nested {
			return raw, true
		}
		var child map[string]json.RawMessage
		if json.Unmarshal(raw, &child) == nil {
			return sourceValue(child, f.children, rest)
		}
	}
	return nil, false
}

// Normalize never changes raw data. Unexpected fields, enum values, numeric
// encodings or missing unit dependencies fail the whole read before publication.
func (p *ReadPlan) Normalize(data json.RawMessage) (map[string]NormalizedValue, map[string]ReadCoverage, error) {
	values := map[string]NormalizedValue{}
	coverage := map[string]ReadCoverage{}
	if len(data) == 0 || string(data) == "null" {
		return values, coverage, nil
	}
	var object func(json.RawMessage, []*readField, string, string) error
	var value func(json.RawMessage, *readField, string, *ast.Type) error
	value = func(raw json.RawMessage, node *readField, path string, typ *ast.Type) error {
		bounded := node.contract.SourceLimit > 0 && typ == node.field.Definition.Type
		if string(raw) == "null" {
			if bounded {
				coverage[path] = ReadCoverage{Status: "unavailable", SourceLimit: node.contract.SourceLimit, Reason: "The bounded source returned null; no completeness claim is possible."}
			}
			if node.contract.Unit != "" {
				values[path] = NormalizedValue{Raw: nil, Unit: deliveredUnit(node.contract.Unit), Status: "null", Currency: node.contract.Currency, Scale: node.contract.Scale}
			}
			return nil
		}
		if typ.Elem != nil {
			var items []json.RawMessage
			if err := json.Unmarshal(raw, &items); err != nil {
				return fmt.Errorf("invalid list at %s", path)
			}
			if limit := node.contract.SourceLimit; bounded {
				if len(items) > limit {
					return fmt.Errorf("upstream exceeded the reviewed source limit at %s", path)
				}
				count := len(items)
				c := ReadCoverage{Status: "complete", SourceLimit: limit, ReturnedItems: &count, Reason: "All matching upstream positions fit below the fixed limit; this does not establish external ingestion coverage or spendability."}
				if count == limit {
					c.Status = "not_proven"
					c.Reason = "The source limit was reached; older matching positions may be omitted and no continuation is available."
				}
				for _, item := range items {
					if string(item) == "null" {
						c.Status = "not_proven"
						c.Reason = "The source returned an unavailable position; complete position data is not established."
						break
					}
				}
				coverage[path] = c
			}
			for i, item := range items {
				if err := value(item, node, path+"/"+strconv.Itoa(i), typ.Elem); err != nil {
					return err
				}
			}
			return nil
		}
		if bounded {
			coverage[path] = ReadCoverage{Status: "not_proven", SourceLimit: node.contract.SourceLimit, Reason: "This aggregate covers at most the latest matching positions up to the source limit; the contributing count and continuation are unavailable. It is not a proven full portfolio total."}
		}
		definition := p.schema.Types[typ.Name()]
		if definition != nil && (definition.Kind == ast.Object || definition.IsAbstractType()) {
			return object(raw, node.children, path, typ.Name())
		}
		if definition != nil && definition.Kind == ast.Enum {
			var enum string
			if json.Unmarshal(raw, &enum) != nil || definition.EnumValues.ForName(enum) == nil {
				return fmt.Errorf("unrecognized enum value at %s", path)
			}
		}
		switch typ.Name() {
		case "String", "ID", "Time":
			var text string
			if json.Unmarshal(raw, &text) != nil {
				return fmt.Errorf("invalid string at %s", path)
			}
		case "Int":
			var integer int64
			if json.Unmarshal(raw, &integer) != nil {
				return fmt.Errorf("invalid integer at %s", path)
			}
		case "Float":
			var number float64
			if json.Unmarshal(raw, &number) != nil {
				return fmt.Errorf("invalid number at %s", path)
			}
		case "Boolean":
			var flag bool
			if json.Unmarshal(raw, &flag) != nil {
				return fmt.Errorf("invalid boolean at %s", path)
			}
		}

		return nil
	}
	object = func(raw json.RawMessage, fields []*readField, path, parentType string) error {
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(raw, &obj); err != nil || obj == nil {
			return fmt.Errorf("invalid object at %s", path)
		}
		runtimeType := parentType
		if d := p.schema.Types[parentType]; d != nil && d.IsAbstractType() {
			name := findField(fields, "__typename")
			if name == nil || json.Unmarshal(obj[name.field.Alias], &runtimeType) != nil {
				return fmt.Errorf("missing concrete type at %s", path)
			}
			valid := false
			for _, possible := range p.schema.GetPossibleTypes(d) {
				if possible.Name == runtimeType {
					valid = true
				}
			}
			if !valid {
				return fmt.Errorf("unrecognized concrete type at %s", path)
			}
		}
		admitted := map[string]bool{}
		for _, node := range fields {
			owner := node.field.ObjectDefinition
			if owner.Name != runtimeType && !owner.IsAbstractType() {
				continue
			}
			raw, exists := obj[node.field.Alias]
			if !exists {
				continue
			}
			admitted[node.field.Alias] = true
			leafPath := path + "/" + node.field.Alias
			if node.field.Name == "__typename" {
				var name string
				if json.Unmarshal(raw, &name) != nil || name != runtimeType {
					return fmt.Errorf("inconsistent concrete type at %s", leafPath)
				}
			}
			if err := value(raw, node, leafPath, node.field.Definition.Type); err != nil {
				return err
			}
			if node.contract.Unit != "" && string(raw) != "null" {
				converted, err := normalizeValue(raw, node.contract, obj, fields)
				if err != nil {
					return fmt.Errorf("cannot safely interpret %s: %w", leafPath, err)
				}
				values[leafPath] = converted
			}
		}
		for key := range obj {
			if !admitted[key] {
				return fmt.Errorf("upstream returned an unselected or unreviewed field at %s/%s", path, key)
			}
		}
		return nil
	}
	if err := object(data, p.fields, "", p.schema.Query.Name); err != nil {
		return nil, nil, err
	}
	return values, coverage, nil
}

func deliveredUnit(unit string) string {
	switch unit {
	case "usd_minor_units":
		return "usd_dollars"
	case "currency_minor_units":
		return "currency_units"
	case "instrument_smallest_units":
		return "asset_units"
	default:
		return unit
	}
}

func normalizeValue(raw json.RawMessage, c FieldContract, obj map[string]json.RawMessage, fields []*readField) (NormalizedValue, error) {
	n := NormalizedValue{Unit: deliveredUnit(c.Unit), Currency: c.Currency, Scale: c.Scale, Status: "available"}
	var text string
	if json.Unmarshal(raw, &text) != nil {
		return n, fmt.Errorf("numeric value must be a decimal string")
	}
	n.Raw = text
	// A legacy invoice explicitly uses an empty string for unavailable valuation.
	if c.Unit == "usd_dollars" && text == "" {
		n.Status = "unavailable"
		return n, nil
	}
	shift := 0
	if c.Scale != nil {
		shift = *c.Scale
	}
	if c.ScaleField != "" {
		rawScale, ok := sourceValue(obj, fields, c.ScaleField)
		var places int
		if !ok || json.Unmarshal(rawScale, &places) != nil || string(rawScale) == "null" || places < 0 || places > 255 {
			return n, fmt.Errorf("missing or invalid %s", c.ScaleField)
		}
		shift = places
		n.Scale = scale(places)
	}
	if c.CurrencyField != "" {
		currencyPath := c.CurrencyField
		if currencyPath == "aggregation" {
			aggregation := findField(fields, "aggregation")
			rawType, ok := sourceValue(obj, fields, "aggregation.__typename")
			var name string
			if !ok || json.Unmarshal(rawType, &name) != nil || aggregation == nil {
				return n, fmt.Errorf("missing balance aggregation identity")
			}
			switch name {
			case "Asset":
				currencyPath = "aggregation.symbol"
			case "Instrument":
				currencyPath = "aggregation.asset.symbol"
			default:
				return n, fmt.Errorf("native amount has no asset-denominated aggregation")
			}
		}
		currency, ok := sourceValue(obj, fields, currencyPath)
		if !ok || json.Unmarshal(currency, &n.Currency) != nil || strings.TrimSpace(n.Currency) == "" {
			return n, fmt.Errorf("missing or invalid %s", currencyPath)
		}
	}
	if c.Integer && (!integerPattern.MatchString(text)) {
		return n, fmt.Errorf("expected an integer smallest-unit value")
	}
	amount, err := shiftDecimal(text, shift)
	if err != nil {
		return n, err
	}
	n.Amount = &amount
	if c.Unit == "decimal_fraction" {
		percent, err := shiftDecimal(text, -2)
		if err != nil {
			return n, err
		}
		n.Percent = &percent
	}
	return n, nil
}

var decimalPattern = regexp.MustCompile(`^-?[0-9]+(?:\.[0-9]+)?$`)
var integerPattern = regexp.MustCompile(`^-?[0-9]+$`)

// shiftDecimal divides a decimal string by 10^places without floating point,
// rounding or a limit on integer magnitude. Fractional cents stay exact.
func shiftDecimal(text string, places int) (string, error) {
	if !decimalPattern.MatchString(text) {
		return "", fmt.Errorf("invalid decimal string")
	}
	negative := strings.HasPrefix(text, "-")
	text = strings.TrimPrefix(text, "-")
	whole, fraction, _ := strings.Cut(text, ".")
	digits := whole + fraction
	point := len(whole) - places
	if point <= 0 {
		digits = strings.Repeat("0", 1-point) + digits
		point = 1
	}
	if point >= len(digits) {
		digits += strings.Repeat("0", point-len(digits))
	}
	whole = strings.TrimLeft(digits[:point], "0")
	if whole == "" {
		whole = "0"
	}
	fraction = strings.TrimRight(digits[point:], "0")
	result := whole
	if fraction != "" {
		result += "." + fraction
	}
	if negative && result != "0" {
		result = "-" + result
	}
	return result, nil
}
