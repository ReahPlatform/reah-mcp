package client

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"

	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const preparedApprovalProtocol = "approval-prepared-v1"

var errPreparedApproval = errors.New("untrusted prepared approval; inspect the original business terms before retrying")

// The authenticated Gateway owns business encoding. The client accepts only this
// fixed approval envelope, never an Agent-selected payload, method or schema.
func preparedApproval(key DeviceKey, c SigningContext, tool string, args document, value any, now time.Time) (document, document, error) {
	raw, err := json.Marshal(value)
	var d document
	if err != nil || len(raw) > 192*1024 || json.Unmarshal(raw, &d) != nil || !exactFields(d, "protocol", "resource", "connectionId", "userId", "entityId", "publicKey", "policyProfile", "tool", "requestId", "issuedAt", "expiresAt", "arguments", "input", "payloadBytes") {
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
	fields, err := approvalEnvelope(wire)
	if err != nil || len(fields[1]) == 0 || string(fields[2]) != c.Binding.EntityID || string(fields[3]) != c.UserID || string(fields[4]) != nonce || string(fields[6]) != "reah.approval_intent.v1" || !sameTimestamp(fields[5], expires) || !sameTimestamp(fields[7], issued) {
		return nil, nil, errPreparedApproval
	}
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

// Only the stable SignedApprovalIntent outer fields are interpreted here.
// Business details stay opaque and are validated by Gateway, Ledger and Hodor.
func approvalEnvelope(wire []byte) (map[protowire.Number][]byte, error) {
	fields := map[protowire.Number][]byte{}
	for len(wire) > 0 {
		number, kind, n := protowire.ConsumeTag(wire)
		if n < 0 || kind != protowire.BytesType || number < 1 || number > 7 || fields[number] != nil {
			return nil, errPreparedApproval
		}
		value, size := protowire.ConsumeBytes(wire[n:])
		if size < 0 {
			return nil, errPreparedApproval
		}
		fields[number] = value
		wire = wire[n+size:]
	}
	if len(fields) != 7 {
		return nil, errPreparedApproval
	}
	return fields, nil
}
func sameTimestamp(wire []byte, expected time.Time) bool {
	var timestamp timestamppb.Timestamp
	return proto.Unmarshal(wire, &timestamp) == nil && len(timestamp.ProtoReflect().GetUnknown()) == 0 && timestamp.CheckValid() == nil && timestamp.AsTime().Equal(expected)
}
