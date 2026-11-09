package client

import (
	"encoding/json"
	"errors"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// The remote catalog describes Gateway capabilities. Advertise only adapters
// that this binary can expose; a future protocol cannot become local authority.
func filterLocalWriteCatalog(result *mcp.CallToolResult, tools []*mcp.Tool) (*mcp.CallToolResult, error) {
	raw, err := json.Marshal(result.StructuredContent)
	var catalog document
	if err != nil || json.Unmarshal(raw, &catalog) != nil {
		return nil, errors.New("invalid Reah capability catalog")
	}
	operations, exists := catalog["write_operations"]
	if !exists {
		return result, nil
	}
	entries, ok := operations.([]any)
	if !ok {
		return nil, errors.New("invalid Reah write capability catalog")
	}
	available := map[string]bool{}
	for _, tool := range tools {
		available[tool.Name] = true
	}
	kept := []any{}
	for _, item := range entries {
		entry, ok := item.(map[string]any)
		if !ok || !available[stringAt(entry, "requestTool")] {
			continue
		}
		entry = cloneDocument(entry)
		if executor := stringAt(entry, "executionTool"); executor != "" && !available[executor] {
			delete(entry, "executionTool")
			entry["executionScope"] = "Approval creation only; this local client cannot execute the advertised protocol."
		}
		kept = append(kept, entry)
	}
	catalog["write_operations"] = kept
	encoded, err := json.Marshal(catalog)
	if err != nil {
		return nil, err
	}
	copy := *result
	copy.StructuredContent = catalog
	copy.Content = []mcp.Content{&mcp.TextContent{Text: string(encoded)}}
	return &copy, nil
}
