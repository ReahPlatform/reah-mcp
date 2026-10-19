// Command schema extracts a reachable, read-only schema from the pinned agent.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	readschema "github.com/ReahPlatform/reah-mcp/internal/schema"
	"github.com/vektah/gqlparser/v2/ast"
	"github.com/vektah/gqlparser/v2/formatter"
	"github.com/vektah/gqlparser/v2/parser"
)

func main() {
	repo := flag.String("repo", "../reah-agent", "upstream local git repository")
	flag.Parse()
	if err := run(*repo); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(repo string) error {
	data, err := os.ReadFile("docs/upstream.json")
	if err != nil {
		return err
	}
	var pin struct {
		Commit           string            `json:"commit"`
		Files            map[string]string `json:"schema_files_sha256"`
		SemanticRegistry struct {
			Path   string `json:"path"`
			SHA256 string `json:"sha256"`
		} `json:"semantic_registry"`
	}
	if err := json.Unmarshal(data, &pin); err != nil {
		return err
	}
	registry, err := exec.Command("git", "-C", repo, "show", pin.Commit+":"+pin.SemanticRegistry.Path).Output()
	if err != nil {
		return fmt.Errorf("read semantic registry: %w", err)
	}
	digest := sha256.Sum256(registry)
	if hex.EncodeToString(digest[:]) != pin.SemanticRegistry.SHA256 {
		return fmt.Errorf("upstream semantic registry hash mismatch")
	}
	var contracts struct {
		Fields map[string]json.RawMessage `json:"schema_field_semantics"`
	}
	if err := json.Unmarshal(registry, &contracts); err != nil {
		return err
	}
	contractJSON, err := json.Marshal(contracts.Fields)
	if err != nil {
		return err
	}
	publicFields := readschema.ContractsFromSource(contractJSON)
	paths := make([]string, 0, len(pin.Files))
	for p := range pin.Files {
		if strings.HasSuffix(p, ".graphql") {
			paths = append(paths, p)
		}
	}
	sort.Strings(paths)
	var sources []*ast.Source
	for _, p := range paths {
		out, err := exec.Command("git", "-C", repo, "show", pin.Commit+":"+p).Output()
		if err != nil {
			return fmt.Errorf("read %s: %w", p, err)
		}
		digest := sha256.Sum256(out)
		if hex.EncodeToString(digest[:]) != pin.Files[p] {
			return fmt.Errorf("upstream hash mismatch: %s", p)
		}
		sources = append(sources, &ast.Source{Name: filepath.Base(p), Input: string(out)})
	}
	doc, err := parser.ParseSchemas(sources...)
	if err != nil {
		return err
	}
	defs := map[string]*ast.Definition{}
	for _, d := range doc.Definitions {
		defs[d.Name] = d
	}
	for _, ext := range doc.Extensions {
		if d := defs[ext.Name]; d != nil {
			d.Fields = append(d.Fields, ext.Fields...)
		} else {
			defs[ext.Name] = ext
		}
	}
	admittedFields := map[string]bool{}
	for name, d := range defs {
		if d.Description == "" {
			d.Description = commentText(d.AfterDescriptionComment)
		}
		d.Directives = nil
		var fields ast.FieldList
		for _, f := range d.Fields {
			contract, admitted := publicFields[name+"."+f.Name]
			if !admitted {
				continue
			}
			if name == "User" && f.Name != "id" && f.Name != "name" && f.Name != "email" && f.Name != "avatarImage" {
				continue
			}
			if f.Type.Name() == "CardSensitiveDataResult" || f.Type.Name() == "PrivateUserInfo" {
				continue
			}
			if name == "Query" && readschema.RootScopes[f.Name] == "" {
				continue
			}
			admittedFields[name+"."+f.Name] = true
			for arg := range contract.Arguments {
				if f.Arguments.ForName(arg) == nil {
					return fmt.Errorf("contract names nonexistent argument %s.%s(%s)", name, f.Name, arg)
				}
			}
			f.Directives = nil
			f.Description = contract.Description
			var arguments ast.ArgumentDefinitionList
			for _, a := range f.Arguments {
				description, admitted := contract.Arguments[a.Name]
				if !admitted {
					if a.Type.NonNull && a.DefaultValue == nil {
						return fmt.Errorf("required argument %s.%s(%s) has no contract", name, f.Name, a.Name)
					}
					continue
				}
				a.Directives = nil
				a.Description = description
				arguments = append(arguments, a)
			}
			f.Arguments = arguments
			fields = append(fields, f)
		}
		d.Fields = fields
		for _, v := range d.EnumValues {
			v.Directives = nil
			if v.Description == "" {
				v.Description = commentText(v.AfterDescriptionComment)
			}
		}
		var members []string
		for _, member := range d.Types {
			for key := range publicFields {
				if strings.HasPrefix(key, member+".") {
					members = append(members, member)
					break
				}
			}
		}
		d.Types = members
	}
	for key := range publicFields {
		if !admittedFields[key] {
			return fmt.Errorf("reviewed field %s is missing from pinned upstream schema", key)
		}
	}
	for name := range readschema.RootScopes {
		if _, admitted := publicFields["Query."+name]; admitted && defs["Query"].Fields.ForName(name) == nil {
			return fmt.Errorf("missing allowed root %s", name)
		}
	}
	seen := map[string]bool{}
	var visit func(string) error
	visit = func(name string) error {
		if seen[name] {
			return nil
		}
		seen[name] = true
		d := defs[name]
		if d == nil {
			if name == "String" || name == "Int" || name == "Float" || name == "Boolean" || name == "ID" {
				return nil
			}
			return fmt.Errorf("missing reachable type %s", name)
		}
		for _, f := range d.Fields {
			if err := visit(f.Type.Name()); err != nil {
				return err
			}
			for _, a := range f.Arguments {
				if err := visit(a.Type.Name()); err != nil {
					return err
				}
			}
		}
		for _, t := range append(d.Types, d.Interfaces...) {
			if err := visit(t); err != nil {
				return err
			}
		}
		// Include implementations to support inline fragments on interfaces.
		if d.Kind == ast.Interface {
			for _, candidate := range defs {
				for _, i := range candidate.Interfaces {
					if i == name {
						if err := visit(candidate.Name); err != nil {
							return err
						}
					}
				}
			}
		}
		return nil
	}
	if err := visit("Query"); err != nil {
		return err
	}
	var names []string
	for name := range seen {
		if defs[name] != nil {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	result := &ast.SchemaDocument{}
	for _, name := range names {
		result.Definitions = append(result.Definitions, defs[name])
	}
	var buf bytes.Buffer
	formatter.NewFormatter(&buf).FormatSchemaDocument(result)
	lines := strings.Split(buf.String(), "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " \t")
	}
	retained := map[string]json.RawMessage{}
	for key, value := range contracts.Fields {
		typeName, fieldName, ok := strings.Cut(key, ".")
		if ok && seen[typeName] && defs[typeName] != nil && defs[typeName].Fields.ForName(fieldName) != nil {
			retained[key] = value
		}
	}
	if len(retained) == 0 {
		return fmt.Errorf("no reachable upstream field semantics")
	}
	semantics, err := json.MarshalIndent(retained, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile("internal/schema/source_semantics.json", append(semantics, '\n'), 0o644); err != nil {
		return err
	}
	return os.WriteFile("internal/schema/readonly.graphql", []byte(strings.Join(lines, "\n")), 0o644)
}

func commentText(group *ast.CommentGroup) string {
	if group == nil {
		return ""
	}
	var lines []string
	for _, c := range group.List {
		lines = append(lines, strings.TrimSpace(c.Text()))
	}
	return strings.Join(lines, "\n")
}
