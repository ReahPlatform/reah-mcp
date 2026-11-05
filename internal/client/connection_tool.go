package client

import (
	"context"
	"encoding/json"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const connectionStatusToolName = "reah_local_connection_status"

func connectionStatusTool() *mcp.Tool {
	return &mcp.Tool{
		Name:        connectionStatusToolName,
		Description: "Diagnose this local Go client's OAuth and signing registration without moving funds. Always available, including before login or when the remote catalog fails. Set check=true to verify the live catalog and each enrolled business's signing binding; automatic OAuth refresh may occur. Without check, only inspect saved local state. Follow nextAction.command and nextAction.args with the same profile. Ask the owner to complete browser consent; do not handle their OTP, Passkey or private key. A ready signing binding does not override transaction permissions, approval policy, limits or balance checks.",
		InputSchema: document{"type": "object", "properties": document{"check": document{"type": "boolean", "default": false}}, "additionalProperties": false},
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}
}

func (r *Remote) connectionStatus(ctx context.Context, args document) *mcp.CallToolResult {
	check := false
	for k, v := range args {
		value, ok := v.(bool)
		if k != "check" || !ok {
			return errorResult("Use only the optional boolean check argument")
		}
		check = value
	}
	var out document
	var err error
	if check {
		out, err = r.CheckStatus(ctx)
	} else {
		out, err = r.OAuth.Status(ctx)
	}
	if out == nil {
		return errorResult("Connection state could not be read; check the OS credential store and retry status with the same server and profile")
	}
	if err != nil {
		out["checkError"] = safeConnectionError(err).Error()
	}
	b, _ := json.Marshal(out)
	return &mcp.CallToolResult{IsError: err != nil, StructuredContent: out, Content: []mcp.Content{&mcp.TextContent{Text: string(b)}}}
}
