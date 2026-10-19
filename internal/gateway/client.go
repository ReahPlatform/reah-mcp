package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	readschema "github.com/ReahPlatform/reah-mcp/internal/schema"
	"github.com/vektah/gqlparser/v2"
	"github.com/vektah/gqlparser/v2/ast"
	"github.com/vektah/gqlparser/v2/formatter"
	"github.com/vektah/gqlparser/v2/validator"
)

const maxResponseBytes = 8 << 20

type Client struct {
	endpoint string
	token    string
	EntityID string
	Schema   *ast.Schema
	http     *http.Client
}

type Response struct {
	Data             json.RawMessage                       `json:"data,omitempty"`
	Errors           json.RawMessage                       `json:"errors,omitempty"`
	FieldSemantics   []readschema.SelectedField            `json:"field_semantics"`
	NormalizedValues map[string]readschema.NormalizedValue `json:"normalized_values"`
	ReadCoverage     map[string]readschema.ReadCoverage    `json:"read_coverage"`
}

func New(endpoint, token, entityID string) (*Client, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("REAH_GRAPHQL_URL must be an absolute HTTP(S) URL without credentials, query or fragment")
	}
	local := u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1" || u.Hostname() == "::1"
	if u.Scheme != "https" && !(u.Scheme == "http" && local) {
		return nil, errors.New("GraphQL requires HTTPS, except loopback development endpoints")
	}
	if strings.TrimSpace(token) == "" || strings.ContainsAny(token, "\r\n") || strings.TrimSpace(entityID) == "" {
		return nil, errors.New("REAH_ACCESS_TOKEN and REAH_ENTITY_ID are required")
	}
	s, err := readschema.Load()
	if err != nil {
		return nil, fmt.Errorf("load read schema: %w", err)
	}
	return &Client{endpoint: endpoint, token: strings.TrimPrefix(token, "Bearer "), EntityID: entityID, Schema: s, http: &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

// Prepare validates against a schema with no mutations, then binds every root
// entry point to the configured entity. Fragments and aliases use the same path.
func (c *Client) Prepare(query string, variables map[string]any) (string, map[string]any, error) {
	if len(query) > 64<<10 {
		return "", nil, errors.New("query exceeds 64 KiB")
	}
	doc, errs := gqlparser.LoadQuery(c.Schema, query)
	if len(errs) > 0 {
		return "", nil, fmt.Errorf("invalid read query: %s", errs.Error())
	}
	if len(doc.Operations) != 1 || doc.Operations[0].Operation != ast.Query {
		return "", nil, errors.New("exactly one query operation is required")
	}
	op := doc.Operations[0]
	vars := make(map[string]any, len(variables)+1)
	for k, v := range variables {
		vars[k] = v
	}
	if v, ok := vars["entityId"]; ok && v != c.EntityID {
		return "", nil, errors.New("entityId conflicts with configured entity")
	}
	if op.VariableDefinitions.ForName("entityId") != nil {
		vars["entityId"] = c.EntityID
	}
	coerced, err := validator.VariableValues(c.Schema, op, vars)
	if err != nil {
		return "", nil, fmt.Errorf("invalid variables: %w", err)
	}
	nodes := 0
	var walk func(ast.SelectionSet, int, bool) error
	walk = func(set ast.SelectionSet, depth int, root bool) error {
		if depth > 16 {
			return errors.New("query depth exceeds 16")
		}
		for _, selection := range set {
			nodes++
			if nodes > 2000 {
				return errors.New("query expands beyond 2000 selections")
			}
			switch field := selection.(type) {
			case *ast.Field:
				if strings.HasPrefix(field.Name, "__") && field.Name != "__typename" {
					return errors.New("use reah_describe_type for schema discovery")
				}
				if root {
					argName := readschema.RootScopes[field.Name]
					if argName == "" {
						return fmt.Errorf("root %s is not a permitted read", field.Name)
					}
					arg := field.Arguments.ForName(argName)
					if arg == nil {
						return fmt.Errorf("%s requires configured entity scope", field.Name)
					}
					value, err := arg.Value.Value(coerced)
					if err != nil || value != c.EntityID {
						return errors.New("query must use the configured entity; prefer $entityId: ID!")
					}
				}
				for _, arg := range field.Arguments {
					if arg.Name == "first" || arg.Name == "pageSize" || arg.Name == "limit" {
						value, err := arg.Value.Value(coerced)
						if err != nil {
							return err
						}
						if value != nil {
							n, ok := value.(int64)
							if !ok {
								if i, okInt := value.(int); okInt {
									n = int64(i)
									ok = true
								}
							}
							if f, isFloat := value.(float64); isFloat && f >= 1 && f <= 200 && f == float64(int64(f)) {
								n = int64(f)
								ok = true
							}
							if !ok || n < 1 || n > 200 {
								return errors.New("page size must be between 1 and 200")
							}
						}
					}
				}
				if err := walk(field.SelectionSet, depth+1, false); err != nil {
					return err
				}
			case *ast.InlineFragment:
				if err := walk(field.SelectionSet, depth, root); err != nil {
					return err
				}
			case *ast.FragmentSpread:
				if err := walk(field.Definition.SelectionSet, depth, root); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := walk(op.SelectionSet, 1, true); err != nil {
		return "", nil, err
	}
	var b bytes.Buffer
	formatter.NewFormatter(&b).FormatQueryDocument(doc)
	return b.String(), coerced, nil
}

func (c *Client) Query(ctx context.Context, query string, variables map[string]any) (*Response, error) {
	query, variables, err := c.Prepare(query, variables)
	if err != nil {
		return nil, err
	}
	doc, errs := gqlparser.LoadQuery(c.Schema, query)
	if len(errs) > 0 {
		return nil, errors.New("cannot interpret validated query")
	}
	plan, err := readschema.Plan(c.Schema, doc, variables)
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(map[string]any{"query": query, "variables": variables})
	if err != nil {
		return nil, errors.New("variables must be JSON values")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, errors.New("cannot create upstream request")
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/graphql-response+json, application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errors.New("Reah GraphQL unavailable or request timed out")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Reah GraphQL returned HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return nil, errors.New("cannot read GraphQL response")
	}
	if len(data) > maxResponseBytes {
		return nil, errors.New("GraphQL response exceeds 8 MiB; request a smaller page or fewer fields")
	}
	var result Response
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, errors.New("invalid GraphQL JSON response")
	}
	if len(result.Data) == 0 && len(result.Errors) == 0 {
		return nil, errors.New("GraphQL response contains neither data nor errors")
	}
	values, coverage, err := plan.Normalize(result.Data)
	if err != nil {
		return nil, err
	}
	result.FieldSemantics = plan.Metadata
	result.NormalizedValues = values
	result.ReadCoverage = coverage
	if len(result.Errors) > 0 && string(result.Errors) != "null" && string(result.Errors) != "[]" {
		for path, c := range result.ReadCoverage {
			if c.Status == "complete" {
				c.Status = "not_proven"
				c.Reason = "GraphQL returned errors; complete acquisition is not established."
				result.ReadCoverage[path] = c
			}
		}
	}
	return &result, nil
}
