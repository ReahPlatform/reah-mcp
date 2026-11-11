package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const localMeta = "reah.com/local"

var signatureFields = []string{"_proof", "requestSignature", "signatures"}

type protocol struct{ Mode, Signing string }

func localProtocol(t *mcp.Tool) (protocol, bool) {
	v, exists := t.Meta[localMeta]
	if !exists {
		return protocol{}, false
	}
	b, e := json.Marshal(v)
	var d document
	if e != nil || json.Unmarshal(b, &d) != nil || d["version"] != float64(1) {
		return protocol{}, true
	}
	p := protocol{stringAt(d, "mode"), stringAt(d, "signing")}
	if p.Mode == "internal" || p.Mode == "proxy" {
		return p, true
	}
	if p.Mode == "signed" && slices.Contains([]string{"approval-send-v2", "approval-financial-v1", "turnkey-execution-v1", "approval-smart-add-v1", "approval-smart-withdraw-v1", "approval-usd-withdraw-v1"}, p.Signing) {
		return p, true
	}
	return protocol{}, true
}

func publicTools(all []*mcp.Tool) ([]*mcp.Tool, error) {
	seen := map[string]bool{}
	out := []*mcp.Tool{}
	for _, t := range all {
		if t == nil || t.Name == "" || seen[t.Name] {
			return nil, errors.New("invalid or duplicate Reah tool definition")
		}
		seen[t.Name] = true
		p, hasMeta := localProtocol(t)
		if t.Name == "reah_signing_context" || p.Mode == "internal" || (hasMeta && p.Mode == "") {
			continue
		}
		b, e := json.Marshal(t.InputSchema)
		if e != nil {
			return nil, errConnection
		}
		var schema document
		if json.Unmarshal(b, &schema) != nil || schema["type"] != "object" {
			return nil, errors.New("invalid Reah tool input schema")
		}
		props, _ := schema["properties"].(map[string]any)
		hasSignature := false
		for _, k := range signatureFields {
			if _, ok := props[k]; ok {
				hasSignature = true
			}
		}
		if p.Mode != "signed" {
			if hasSignature || (!hasMeta && (t.Annotations == nil || !t.Annotations.ReadOnlyHint)) {
				continue
			}
			out = append(out, t)
			continue
		}
		if isNewFinancialProtocol(p.Signing) && !newFinancialSchema(schema, p.Signing) {
			continue
		}
		if p.Signing == "approval-financial-v1" && !convertSchema(schema) && !bankTransferSchema(schema) {
			continue
		}
		if t.Annotations == nil || t.Annotations.ReadOnlyHint {
			continue
		}
		for _, k := range signatureFields {
			delete(props, k)
		}
		if required, ok := schema["required"].([]any); ok {
			kept := []any{}
			for _, k := range required {
				str, _ := k.(string)
				if !slices.Contains(signatureFields, str) {
					kept = append(kept, k)
				}
			}
			schema["required"] = kept
		}
		copy := *t
		if isNewFinancialProtocol(p.Signing) {
			addFinancialContext(schema, p.Signing)
			copy.Description += " Local signing requires a financial-operations-v1 credential, a stable requestId, exact owned account context and explicit terms. Approval creation never approves or proves execution or settlement. Unknown outcomes require reconciliation of the original approval, without automatic replacement."
		} else if p.Signing == "approval-financial-v1" && bankTransferSchema(schema) {
			addBankTransferContext(schema)
			copy.Description += " Local adapter supports existing Banking internal Send v1 quotes paid by Gas Station. Supply bankTransferContext from the exact fresh quote and independently verified owned ERC20 Banking settlement metadata. The signed quote retains its existing UUID identities without an asset field; local metadata is not added to the signature. Preserve requestId and frozen approvalDetailsJson. Requires explicit financial-operations-v1 registration. Check actual available Banking funds and Gas Station balance separately. Bank internal execution requires a compatible Gateway advertising that business and Hodor acknowledging its private exact-request binding. This Web-equivalent bank route trusts Ledger transaction semantics; wallet and convert construction checks remain separate. Approval creation never approves, executes or proves settlement."
		} else if p.Signing == "approval-financial-v1" {
			addConvertContext(schema)
			copy.Description += " Local adapter supports EVM Wallet crypto conversion (same-chain swap or cross-chain convert), exact-input and minimum-output, without Points. Smart Balance supports exact-input ERC-20 funding from Ethereum/Base to Tempo only, with an owned synchronous ERC-4626/allocator vault; no Smart same-chain swap, minimum-output or exact-output. Supply convertContext from independently verified account/network/instrument reads, explicit destinationAddress, authoritative asset decimals and CAIP-19 identities. Context is local-only and is bound into a v4 approval intent. Preserve its stable requestId. For SMART_BALANCE retain the exact owned smartBalanceVaultId, require redeemEnabled and check smartBalance for principal, with Wallet/Gas Station gas funded separately. Obtain a fresh quote and check principal plus selected gas balance before submitting; use a positive minimumReceive at destination precision. This requires explicit financial-operations-v1 registration; wallet-only credentials cannot convert."
		}
		copy.InputSchema = schema
		out = append(out, &copy)
	}
	return out, nil
}

type bearerTransport struct {
	OAuth *OAuth
	Base  http.RoundTripper
}

func (t bearerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.String() != t.OAuth.Config.Server {
		return nil, errors.New("blocked unexpected MCP destination")
	}
	token, e := t.OAuth.AccessToken(r.Context())
	if e != nil {
		return nil, e
	}
	clone := r.Clone(r.Context())
	clone.Header = r.Header.Clone()
	clone.Header.Set("Authorization", "Bearer "+token)
	base := t.Base
	if base == nil {
		base = http.DefaultTransport
	}
	response, e := base.RoundTrip(clone)
	if e != nil {
		return nil, errConnection
	}
	if response.StatusCode >= 400 {
		defer response.Body.Close()
		return nil, oauthHTTPError(response, "/mcp")
	}
	return response, nil
}

type Remote struct {
	OAuth   *OAuth
	Version string
	mu      sync.Mutex
	session *mcp.ClientSession
}

func (r *Remote) connected(ctx context.Context) (*mcp.ClientSession, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.session != nil {
		return r.session, nil
	}
	h := httpClient()
	h.Transport = bearerTransport{r.OAuth, r.OAuth.HTTP.Transport}
	c := mcp.NewClient(&mcp.Implementation{Name: "reah-go-mcp", Version: r.Version}, &mcp.ClientOptions{Capabilities: &mcp.ClientCapabilities{}, MultiRoundTrip: &mcp.MultiRoundTripOptions{Disabled: true}})
	s, e := c.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: r.OAuth.Config.Server, HTTPClient: h, MaxRetries: -1, DisableStandaloneSSE: true, MaxEventSize: 2 * 1024 * 1024}, nil)
	if e != nil {
		return nil, safeConnectionError(e)
	}
	r.session = s
	return s, nil
}
func (r *Remote) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.session != nil {
		_ = r.session.Close()
		r.session = nil
	}
}
func (r *Remote) List(ctx context.Context) ([]*mcp.Tool, error) {
	if _, e := r.OAuth.AccessToken(ctx); e != nil {
		return nil, e
	}
	s, e := r.connected(ctx)
	if e != nil {
		return nil, e
	}
	var all []*mcp.Tool
	cursor := ""
	seen := map[string]bool{}
	for {
		p, e := s.ListTools(ctx, &mcp.ListToolsParams{Cursor: cursor})
		if e != nil {
			return nil, safeConnectionError(e)
		}
		all = append(all, p.Tools...)
		cursor = p.NextCursor
		if len(all) > 500 || len(seen) > 500 || (cursor != "" && seen[cursor]) {
			return nil, errors.New("invalid Reah tool catalog")
		}
		if cursor == "" {
			break
		}
		seen[cursor] = true
	}
	return publicTools(all)
}

type invoke func(context.Context, string, document) (*mcp.CallToolResult, error)

func (r *Remote) invoke(ctx context.Context, name string, args document) (*mcp.CallToolResult, error) {
	s, e := r.connected(ctx)
	if e != nil {
		return nil, e
	}
	result, e := s.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	if e != nil {
		return nil, safeConnectionError(e)
	}
	return result, nil
}
func (r *Remote) Call(ctx context.Context, name string, args document) (*mcp.CallToolResult, error) {
	for _, k := range signatureFields {
		if _, ok := args[k]; ok {
			return nil, errors.New("signing material is supplied by the local client, never tool arguments")
		}
	}
	tools, e := r.List(ctx)
	if e != nil {
		return nil, e
	}
	var tool *mcp.Tool
	for _, t := range tools {
		if t.Name == name {
			tool = t
			break
		}
	}
	if tool == nil {
		message := "tool is unavailable in the current authenticated Reah catalog"
		// A missing name does not identify a financial capability. Add only
		// the saved connection's explicit limitation, without inferring one.
		_ = r.OAuth.Store.Exclusive(ctx, func() error {
			s, err := r.OAuth.Store.Read()
			if err == nil && s != nil && s.Tokens != nil && s.Enrollment != nil && s.Enrollment.State == "read_only" {
				message += ". The saved connection is read-only; financial signing is unavailable for its selected businesses. Ask a business Owner to check signing access or registration."
			}
			return err
		})
		return nil, errors.New(message)
	}
	p, _ := localProtocol(tool)
	if p.Mode != "signed" {
		result, err := r.invoke(ctx, name, args)
		if err == nil && name == "reah_catalog" && result != nil && !result.IsError {
			return filterLocalWriteCatalog(result, tools)
		}
		return result, err
	}
	if p.Signing == "turnkey-execution-v1" && args["business"] == "bank_internal_transfer" && !advertisesBankExecution(tool) {
		return nil, errors.New("Banking execution is unavailable in the authenticated Gateway catalog; use the existing human flow")
	}
	key, e := r.OAuth.signingKey(ctx)
	if e != nil {
		return nil, e
	}
	return callSigned(ctx, name, p.Signing, args, key, r.OAuth.Config.Server, r.invoke, "")
}

// Older catalogs cannot opt a Banking approval into an unbound custody route.
// The compatible Gateway additionally requires Hodor's private binding acknowledgment.
func advertisesBankExecution(tool *mcp.Tool) bool {
	b, err := json.Marshal(tool.InputSchema)
	var schema struct {
		Properties struct {
			Business struct {
				Enum []string `json:"enum"`
			} `json:"business"`
		} `json:"properties"`
	}
	return err == nil && json.Unmarshal(b, &schema) == nil && slices.Contains(schema.Properties.Business.Enum, "bank_internal_transfer")
}

func callSigned(ctx context.Context, name, format string, args document, key DeviceKey, server string, call invoke, verificationKey string) (*mcp.CallToolResult, error) {
	if !validUUID(stringAt(args, "entityId")) {
		return nil, errors.New("an authorized entityId is required")
	}
	if format == "turnkey-execution-v1" {
		for k := range args {
			if k != "entityId" && k != "approvalId" && k != "business" {
				return nil, errors.New("execution requires the original approvalId and no signing material")
			}
		}
		if !validUUID(stringAt(args, "approvalId")) {
			return nil, errors.New("execution requires the original approvalId")
		}
		if b, ok := args["business"]; ok && !slices.Contains([]any{"crypto_convert", "bank_crypto_deposit", "bank_crypto_withdraw", "banking_fiat_withdrawal", "bank_internal_transfer", "banking_usd_withdrawal", "add_smart_balance", "withdraw_smart_balance"}, b) {
			return nil, errors.New("unsupported financial execution business")
		}
	} else if format != "approval-send-v2" && format != "approval-financial-v1" && !isNewFinancialProtocol(format) {
		return nil, errors.New("unsupported local signing protocol")
	}
	contextResult, e := call(ctx, "reah_signing_context", document{"entityId": args["entityId"]})
	if e != nil {
		return nil, e
	}
	if contextResult.IsError {
		return contextResult, nil
	}
	c, e := signingContext(contextResult.StructuredContent, server, stringAt(args, "entityId"), key)
	if e != nil {
		return nil, e
	}
	submit := func(input document) (*mcp.CallToolResult, error) {
		proof, e := operationProof(key, c, name, input)
		if e != nil {
			return nil, e
		}
		body := cloneDocument(input)
		body["_proof"] = proof
		out, e := call(ctx, name, body)
		if e != nil {
			return nil, errors.New("operation outcome is unconfirmed; inspect the original requestId or approvalId before retrying; do not create a replacement")
		}
		return out, nil
	}
	if isNewFinancialProtocol(format) {
		intent, body, err := newFinancialIntent(key, c, args, format, time.Now())
		if err != nil {
			return nil, err
		}
		body["requestSignature"] = intent
		return submit(body)
	}
	if format == "approval-financial-v1" {
		var intent, body document
		var e error
		if args["bankTransferContext"] != nil {
			intent, body, e = bankTransferIntent(key, c, args, time.Now())
		} else {
			intent, body, e = convertIntent(key, c, args, time.Now())
		}
		if e != nil {
			return nil, e
		}
		body["requestSignature"] = intent
		return submit(body)
	}
	if format == "approval-send-v2" {
		intent, e := transferIntent(key, c, args, time.Now())
		if e != nil {
			return nil, e
		}
		body := cloneDocument(args)
		body["requestSignature"] = intent
		return submit(body)
	}
	input := cloneDocument(args)
	seen := map[string]bool{}
	for round := 0; round <= 8; round++ {
		out, e := submit(input)
		if e != nil || out.IsError {
			return out, e
		}
		b, e := json.Marshal(out.StructuredContent)
		var d document
		if e != nil || json.Unmarshal(b, &d) != nil {
			return nil, errors.New("invalid execution response; inspect the original approval")
		}
		requests, exists := d["requiresSignatures"]
		if !exists {
			return out, nil
		}
		items, ok := requests.([]any)
		if !ok || len(items) > 16 {
			return nil, errors.New("invalid execution signature list")
		}
		if len(items) == 0 {
			return out, nil
		}
		if round == 8 {
			return nil, errors.New("execution continuation limit reached; reconcile the original approval before continuing")
		}
		signatures := []any{}
		for _, item := range items {
			v, ok := item.(map[string]any)
			if !ok {
				return nil, errors.New("invalid execution signature request")
			}
			id := stringAt(v, "requestId")
			if seen[id] {
				return nil, errors.New("repeated execution signing request; reconcile the original approval")
			}
			stamp, e := executionStamp(key, c, v, verificationKey)
			if e != nil {
				return nil, e
			}
			seen[id] = true
			signatures = append(signatures, stamp)
		}
		// Every continuation retains the original entity, approval and business.
		// It never repeats a previous submission or polls until settlement.
		input = cloneDocument(args)
		input["signatures"] = signatures
	}
	panic("unreachable execution continuation")
}
func cloneDocument(d document) document {
	out := document{}
	for k, v := range d {
		out[k] = v
	}
	return out
}

// NewServer keeps stdout exclusively for MCP. The remote catalog is loaded lazily,
// so browser consent never blocks the host's MCP initialization handshake.
func NewServer(remote *Remote, version string) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "reah-go-mcp", Version: version}, &mcp.ServerOptions{Instructions: "If Reah tools are missing or signing is unavailable, call reah_local_connection_status with check=true and follow nextAction using the exact server and profile. OAuth login, key possession proof and user-confirmed financial registration are separate stages. Financial registration is available to authorized Owners, Admins and Operators (Members), subject to live server permissions and Turnkey authorization; a role or OAuth token alone is not signing readiness. Do not claim signing readiness from local awaiting_provider state. Resume incomplete registration with enroll-key while the registration window is open. Expired browser signing access requires the user to renew Web authentication, not replace MCP credentials. Only start a new login when status explicitly requires one, such as login_required or registration_expired. Use --no-browser --json for labeled OAuth and registration URLs; open each stage's private URL for the user and keep login running until its callback completes. Never approve financial registration for the user or retry a payment to test connectivity.", Capabilities: &mcp.ServerCapabilities{Tools: &mcp.ToolCapabilities{}}, SupportedProtocolVersions: []string{"2025-11-25"}})
	s.AddReceivingMiddleware(func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
			defer cancel()
			switch method {
			case "tools/list":
				tools, e := remote.List(ctx)
				if e != nil {
					// Keep diagnosis discoverable even before login or during an outage.
					return &mcp.ListToolsResult{Tools: []*mcp.Tool{connectionStatusTool()}}, nil
				}
				for _, tool := range tools {
					if tool.Name == connectionStatusToolName {
						return nil, errors.New("remote catalog conflicts with the local connection diagnostic")
					}
				}
				return &mcp.ListToolsResult{Tools: append([]*mcp.Tool{connectionStatusTool()}, tools...)}, nil
			case "tools/call":
				request, ok := req.(*mcp.CallToolRequest)
				if !ok || request.Params == nil {
					return nil, errors.New("invalid tool call")
				}
				if len(request.Params.Arguments) > 192*1024 {
					return errorResult("Tool arguments exceed the local request limit"), nil
				}
				var args document
				decoder := json.NewDecoder(bytes.NewReader(request.Params.Arguments))
				decoder.UseNumber()
				e := decoder.Decode(&args)
				if len(request.Params.Arguments) == 0 {
					args = document{}
					e = nil
				}
				if e != nil || args == nil {
					return errorResult("Tool arguments must be an object"), nil
				}
				if request.Params.Name == connectionStatusToolName {
					return remote.connectionStatus(ctx, args), nil
				}
				out, e := remote.Call(ctx, request.Params.Name, args)
				if e != nil {
					return errorResult(e.Error()), nil
				}
				return out, nil
			}
			return next(ctx, method, req)
		}
	})
	return s
}
func errorResult(message string) *mcp.CallToolResult {
	return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: message}}}
}
