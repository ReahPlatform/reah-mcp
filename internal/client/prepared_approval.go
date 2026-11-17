package client

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"
)

const preparedApprovalProtocol = "approval-prepared-v1"

var errPreparedApproval = errors.New("untrusted prepared approval; inspect the original business terms before retrying")

// The authenticated Gateway owns business encoding. The client verifies these
// protected approval fields, never an Agent-selected payload, method or schema.
// Additional outer annotations are descriptive and never enter signing input.
func preparedApproval(key DeviceKey, c SigningContext, tool string, args document, value any, now time.Time) (document, document, error) {
	raw, err := json.Marshal(value)
	var d document
	if err != nil || len(raw) > 192*1024 || json.Unmarshal(raw, &d) != nil {
		return nil, nil, errPreparedApproval
	}
	for field, expected := range map[string]string{"protocol": preparedApprovalProtocol, "resource": c.Resource, "connectionId": c.ConnectionID, "userId": c.UserID, "entityId": c.Binding.EntityID, "publicKey": key.PublicKey, "policyProfile": c.Binding.PolicyProfile, "tool": tool} {
		if d[field] != expected {
			return nil, nil, errPreparedApproval
		}
	}
	original := cloneDocument(args)
	delete(original, "preparationContext")
	input, ok := d["input"].(map[string]any)
	if !ok || !sameJSON(d["arguments"], args) || !sameJSON(input, original) {
		return nil, nil, errPreparedApproval
	}
	for _, field := range append(signatureFields, "payloadBytes") {
		if _, exists := input[field]; exists {
			return nil, nil, errPreparedApproval
		}
	}
	nonce := stringAt(args, "requestId")
	if context, ok := args["preparationContext"].(map[string]any); ok {
		if nested := stringAt(context, "requestId"); nested != "" {
			if nonce != "" && nested != nonce {
				return nil, nil, errPreparedApproval
			}
			nonce = nested
		}
	}
	if !validUUID(nonce) || d["requestId"] != nonce {
		return nil, nil, errPreparedApproval
	}
	issued, e1 := time.Parse(time.RFC3339Nano, stringAt(d, "issuedAt"))
	expires, e2 := time.Parse(time.RFC3339Nano, stringAt(d, "expiresAt"))
	binding, e3 := time.Parse(time.RFC3339Nano, c.Binding.ExpiresAt)
	if e1 != nil || e2 != nil || e3 != nil || issued.After(now.Add(30*time.Second)) || issued.Before(now.Add(-time.Minute)) || !expires.After(now) || !expires.After(issued) || expires.After(binding) {
		return nil, nil, errPreparedApproval
	}
	payload := stringAt(d, "payloadBytes")
	wire, err := base64.StdEncoding.Strict().DecodeString(payload)
	if err != nil || len(wire) == 0 || len(wire) > 128*1024 || base64.StdEncoding.EncodeToString(wire) != payload {
		return nil, nil, errPreparedApproval
	}
	// The authenticated Gateway owns the payload's format and business checks.
	// Sign its exact opaque representation without decoding an approval schema.
	signature, err := signText(key, payload, false)
	if err != nil {
		return nil, nil, err
	}
	return document{"payloadBytes": payload, "publicKey": key.PublicKey, "signature": signature}, input, nil
}

func sameJSON(a, b any) bool {
	left, e1 := json.Marshal(a)
	right, e2 := json.Marshal(b)
	return e1 == nil && e2 == nil && bytes.Equal(left, right)
}
