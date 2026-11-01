package client

import (
	"crypto/ecdsa"
	"crypto/sha256"
	"crypto/x509"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"time"

	"github.com/google/uuid"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"
	_ "google.golang.org/protobuf/types/known/timestamppb"
)

// Pinned Conductor approval schema; see docs/signing-schema.md for provenance.
//
//go:embed approval.descriptor.bin
var approvalDescriptor []byte

var approvalFile, approvalFileError = loadApprovalDescriptor()

func loadApprovalDescriptor() (protoreflect.FileDescriptor, error) {
	var d descriptorpb.FileDescriptorProto
	if err := proto.Unmarshal(approvalDescriptor, &d); err != nil {
		return nil, err
	}
	d.Dependency = []string{"google/protobuf/timestamp.proto"}
	return protodesc.NewFile(&d, protoregistry.GlobalFiles)
}

type SigningContext struct {
	UserID       string `json:"userId"`
	ConnectionID string `json:"connectionId"`
	Resource     string `json:"resource"`
	Binding      struct {
		EntityID  string `json:"entityId"`
		PublicKey string `json:"publicKey"`
		SuborgID  string `json:"suborgId"`
		ExpiresAt string `json:"expiresAt"`
	} `json:"binding"`
}

func signingContext(value any, server, entity string, key DeviceKey) (SigningContext, error) {
	var c SigningContext
	b, e := json.Marshal(value)
	if e == nil {
		e = json.Unmarshal(b, &c)
	}
	expiry, te := time.Parse(time.RFC3339Nano, c.Binding.ExpiresAt)
	if e != nil || te != nil || !expiry.After(time.Now()) || c.Resource != server || !validUUID(c.UserID) || !validUUID(c.ConnectionID) || c.Binding.EntityID != entity || c.Binding.PublicKey != key.PublicKey || !validUUID(c.Binding.SuborgID) {
		return c, errors.New("local key is not authorized for this business; complete credential registration in Reah")
	}
	return c, nil
}

func operationProof(key DeviceKey, c SigningContext, name string, args document) (document, error) {
	if !regexp.MustCompile(`^[a-zA-Z0-9_.-]{1,128}$`).MatchString(name) {
		return nil, errors.New("invalid tool name")
	}
	p, e := json.Marshal(document{"domain": "reah.mcp.operation.v1", "resource": c.Resource, "connectionId": c.ConnectionID, "tool": name, "entityId": c.Binding.EntityID, "nonce": uuid.NewString(), "expiresAt": time.Now().Unix() + 60, "arguments": args})
	if e != nil {
		return nil, e
	}
	sig, e := signText(key, string(p), false)
	return document{"payload": string(p), "signature": sig}, e
}

var transferFields = []string{"entityId", "requestId", "instrumentAccountId", "walletId", "networkId", "instrumentId", "fromAddress", "toAddress", "amount", "asset", "gasSource", "memo", "notes"}

func transferIntent(key DeviceKey, c SigningContext, a document, now time.Time) (document, error) {
	allowed := make(map[string]bool)
	for _, k := range transferFields {
		allowed[k] = true
	}
	for k, v := range a {
		if _, ok := v.(string); !allowed[k] || !ok {
			return nil, errors.New("use explicit transfer terms; signing material is generated locally")
		}
	}
	for _, k := range transferFields {
		if k != "memo" && k != "notes" && stringAt(a, k) == "" {
			return nil, fmt.Errorf("missing transfer field: %s", k)
		}
	}
	for _, k := range []string{"entityId", "requestId", "instrumentAccountId", "walletId", "networkId", "instrumentId"} {
		if !validUUID(stringAt(a, k)) {
			return nil, fmt.Errorf("invalid transfer UUID: %s", k)
		}
	}
	if !regexp.MustCompile(`^[1-9][0-9]{0,77}$`).MatchString(stringAt(a, "amount")) || (a["gasSource"] != "GAS_SOURCE_FROM_WALLET" && a["gasSource"] != "GAS_SOURCE_GAS_STATION") || len(stringAt(a, "memo")) > 1024 || len(stringAt(a, "notes")) > 4096 {
		return nil, errors.New("use a positive minor-unit integer amount and explicitly select wallet or Gas Station gas")
	}
	d := document{"v": 2, "balanceSource": "BALANCE_SOURCE_WALLET"}
	for _, k := range transferFields {
		if k != "entityId" && k != "requestId" {
			if v, ok := a[k]; ok {
				d[k] = v
			}
		}
	}
	party := func(address string) document {
		return document{"partyType": "PARTY_TYPE_ADDRESS", "address": address, "instrumentId": a["instrumentId"], "amount": a["amount"]}
	}
	d["fromTo"] = document{"from": []any{party(stringAt(a, "fromAddress"))}, "to": []any{party(stringAt(a, "toAddress"))}}
	return approvalIntent(key, c, document{"sendTransaction": d}, stringAt(a, "requestId"), now)
}

func approvalIntent(key DeviceKey, c SigningContext, details document, nonce string, now time.Time) (document, error) {
	if approvalFileError != nil {
		return nil, errors.New("approval schema is unavailable")
	}
	expires, e := time.Parse(time.RFC3339Nano, c.Binding.ExpiresAt)
	if e != nil {
		return nil, e
	}
	if cap := now.Add(7 * 24 * time.Hour); expires.After(cap) {
		expires = cap
	}
	b, e := json.Marshal(document{"details": details, "domain": "reah.approval_intent.v1", "entityId": c.Binding.EntityID, "initiatorUserId": c.UserID, "nonce": nonce, "issuedAt": now.UTC().Format(time.RFC3339Nano), "expiresAt": expires.UTC().Format(time.RFC3339Nano)})
	if e != nil {
		return nil, e
	}
	m := dynamicpb.NewMessage(approvalFile.Messages().ByName("SignedApprovalIntent"))
	if e = protojson.Unmarshal(b, m); e != nil {
		return nil, errors.New("invalid structured approval intent")
	}
	wire, e := (proto.MarshalOptions{Deterministic: true}).Marshal(m)
	if e != nil {
		return nil, e
	}
	payload := base64.StdEncoding.EncodeToString(wire)
	sig, e := signText(key, payload, false)
	return document{"payloadBytes": payload, "publicKey": key.PublicKey, "signature": sig}, e
}

const stagingKey = `-----BEGIN PUBLIC KEY-----
MFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAEvtIyoor6aZLs9QVQ1Cj5bekX28hL
QpIRvUwuKaxV7lIfxTxRfYTX0YE10XgwELydE9spUeUwO34xEEXKBifXjA==
-----END PUBLIC KEY-----`
const productionKey = `-----BEGIN PUBLIC KEY-----
MFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAEP0tIL+DaFftn17dRB87c19wbf5QV
4NYTeww6cXvSILTf3rFRvflz6EApe9mGA2+6v/6lD7dLhVAdcej3kSyQrg==
-----END PUBLIC KEY-----`

func executionStamp(key DeviceKey, c SigningContext, item document, verificationKey string) (document, error) {
	bad := errors.New("untrusted execution signing request; inspect the original approval before retrying")
	payload, id := stringAt(item, "requestPayload"), stringAt(item, "requestId")
	if id == "" || payload == "" || len(payload) > 128*1024 {
		return nil, bad
	}
	if verificationKey == "" {
		switch c.Resource {
		case DefaultServer:
			verificationKey = stagingKey
		case "https://platform.reah.com/mcp":
			verificationKey = productionKey
		default:
			return nil, bad
		}
	}
	p, _ := pem.Decode([]byte(verificationKey))
	if p == nil {
		return nil, bad
	}
	v, e := x509.ParsePKIXPublicKey(p.Bytes)
	if e != nil {
		return nil, bad
	}
	pub, ok := v.(*ecdsa.PublicKey)
	if !ok {
		return nil, bad
	}
	sig, e := base64.StdEncoding.Strict().DecodeString(stringAt(item, "signature"))
	h := sha256.Sum256([]byte(payload))
	if e != nil || !ecdsa.VerifyASN1(pub, h[:], sig) {
		return nil, bad
	}
	var body document
	if json.Unmarshal([]byte(payload), &body) != nil {
		return nil, bad
	}
	params, ok := body["parameters"].(map[string]any)
	ts, e := strconv.ParseInt(stringAt(body, "timestampMs"), 10, 64)
	if !ok || e != nil || body["organizationId"] != c.Binding.SuborgID || (body["type"] != "ACTIVITY_TYPE_SIGN_RAW_PAYLOADS" && body["type"] != "ACTIVITY_TYPE_SIGN_TRANSACTION_V2") || stringAt(params, "signWith") == "" || ts > time.Now().Add(30*time.Second).UnixMilli() || ts < time.Now().Add(-10*time.Minute).UnixMilli() {
		return nil, bad
	}
	stamp, e := signText(key, payload, true)
	if e != nil {
		return nil, e
	}
	b, e := json.Marshal(document{"publicKey": key.PublicKey, "scheme": "SIGNATURE_SCHEME_TK_API_P256", "signature": stamp})
	if e != nil {
		return nil, e
	}
	return document{"requestId": id, "signature": base64.RawURLEncoding.EncodeToString(b)}, nil
}
