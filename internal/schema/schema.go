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

func Load() (*ast.Schema, error) {
	return gqlparser.LoadSchema(&ast.Source{Name: "readonly.graphql", Input: SDL})
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
		fmt.Fprintf(&b, "%s", f.Name)
		if len(f.Arguments) > 0 {
			var args []string
			for _, a := range f.Arguments {
				args = append(args, a.Name+": "+a.Type.String())
			}
			fmt.Fprintf(&b, "(%s)", strings.Join(args, ", "))
		}
		fmt.Fprintf(&b, ": %s\n", f.Type.String())
		if f.Description != "" {
			fmt.Fprintf(&b, "  %s\n", f.Description)
		}
	}
	for _, e := range d.EnumValues {
		fmt.Fprintf(&b, "%s\n", e.Name)
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
