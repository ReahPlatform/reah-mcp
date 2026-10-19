package schema

import (
	_ "embed"
	"fmt"
	"sort"
	"strings"

	"github.com/vektah/gqlparser/v2"
	"github.com/vektah/gqlparser/v2/ast"
)

//go:embed readonly.graphql
var SDL string

// SourceSemantics contains only contracts reachable through the read schema.
//
//go:embed source_semantics.json
var SourceSemantics []byte

func Load() (*ast.Schema, error) {
	s, err := gqlparser.LoadSchema(&ast.Source{Name: "readonly.graphql", Input: SDL})
	if err != nil {
		return nil, err
	}
	fields := map[string]string{}
	for _, d := range s.Types {
		if d.BuiltIn || strings.HasPrefix(d.Name, "__") {
			continue
		}
		for _, f := range d.Fields {
			if strings.HasPrefix(f.Name, "__") {
				continue
			}
			fields[d.Name+"."+f.Name] = f.Type.Name()
		}
	}
	if err := ValidateContracts(fields); err != nil {
		return nil, err
	}
	return s, nil
}

// Describe returns only requested type definitions, avoiding a full-schema dump.
func Describe(s *ast.Schema, name string) (string, error) {
	d := s.Types[name]
	if d == nil || strings.HasPrefix(name, "__") {
		return "", fmt.Errorf("unknown read type %q", name)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s\n%s\n", d.Kind, d.Name, d.Description)
	for _, f := range d.Fields {
		if strings.HasPrefix(f.Name, "__") {
			continue
		}
		fmt.Fprintf(&b, "%s", f.Name)
		if len(f.Arguments) > 0 {
			var args []string
			for _, a := range f.Arguments {
				argument := a.Name + ": " + a.Type.String()
				if a.DefaultValue != nil {
					argument += " = " + a.DefaultValue.String()
				}
				args = append(args, argument)
			}
			fmt.Fprintf(&b, "(%s)", strings.Join(args, ", "))
		}
		fmt.Fprintf(&b, ": %s\n", f.Type.String())
		if f.Description != "" {
			fmt.Fprintf(&b, "  %s\n", f.Description)
		}
		if f.DefaultValue != nil {
			fmt.Fprintf(&b, "  default: %s\n", f.DefaultValue.String())
		}
		for _, a := range f.Arguments {
			if a.Description != "" {
				fmt.Fprintf(&b, "  argument %s: %s\n", a.Name, a.Description)
			}
		}
	}
	for _, e := range d.EnumValues {
		fmt.Fprintf(&b, "%s\n", e.Name)
		if e.Description != "" {
			fmt.Fprintf(&b, "  %s\n", e.Description)
		}
	}
	if len(d.Types) > 0 {
		fmt.Fprintf(&b, "members: %s\n", strings.Join(d.Types, ", "))
	}
	if d.Kind == ast.Interface {
		var names []string
		for _, t := range s.GetPossibleTypes(d) {
			names = append(names, t.Name)
		}
		sort.Strings(names)
		fmt.Fprintf(&b, "implementations: %s\n", strings.Join(names, ", "))
	}
	return b.String(), nil
}
