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
		Commit string            `json:"commit"`
		Files  map[string]string `json:"schema_files_sha256"`
	}
	if err := json.Unmarshal(data, &pin); err != nil {
		return err
	}
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
	for name, d := range defs {
		if d.Description == "" {
			d.Description = commentText(d.AfterDescriptionComment)
		}
		d.Directives = nil
		var fields ast.FieldList
		for _, f := range d.Fields {
			if name == "User" && f.Name != "id" && f.Name != "name" && f.Name != "email" && f.Name != "avatarImage" {
				continue
			}
			if f.Type.Name() == "CardSensitiveDataResult" || f.Type.Name() == "PrivateUserInfo" {
				continue
			}
			if name == "Query" && readschema.RootScopes[f.Name] == "" {
				continue
			}
			if name == "Entity" && !readschema.EntityFields[f.Name] {
				continue
			}
			f.Directives = nil
			if f.Description == "" {
				f.Description = commentText(f.AfterDescriptionComment)
			}
			for _, a := range f.Arguments {
				a.Directives = nil
				if a.Description == "" {
					a.Description = commentText(a.AfterDescriptionComment)
				}
			}
			fields = append(fields, f)
		}
		d.Fields = fields
		for _, v := range d.EnumValues {
			v.Directives = nil
			if v.Description == "" {
				v.Description = commentText(v.AfterDescriptionComment)
			}
		}
	}
	for name := range readschema.RootScopes {
		if defs["Query"].Fields.ForName(name) == nil {
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
