package client

import (
	"crypto/ecdsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"regexp"
	"strconv"
	"time"

	"github.com/google/uuid"
)

type SigningContext struct {
	UserID       string `json:"userId"`
	ConnectionID string `json:"connectionId"`
	Resource     string `json:"resource"`
	Binding      struct {
		EntityID      string `json:"entityId"`
		PublicKey     string `json:"publicKey"`
		SuborgID      string `json:"suborgId"`
		ExpiresAt     string `json:"expiresAt"`
		PolicyProfile string `json:"policyProfile"`
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

const stagingKey = `-----BEGIN PUBLIC KEY-----
MFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAEvtIyoor6aZLs9QVQ1Cj5bekX28hL
QpIRvUwuKaxV7lIfxTxRfYTX0YE10XgwELydE9spUeUwO34xEEXKBifXjA==
-----END PUBLIC KEY-----`
const productionKey = `-----BEGIN PUBLIC KEY-----
MFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAEP0tIL+DaFftn17dRB87c19wbf5QV
4NYTeww6cXvSILTf3rFRvflz6EApe9mGA2+6v/6lD7dLhVAdcej3kSyQrg==
-----END PUBLIC KEY-----`

func executionVerificationKey(resource string) string {
	switch resource {
	case ProductionServer:
		return productionKey
	case StagingServer:
		return stagingKey
	default:
		return ""
	}
}

func executionStamp(key DeviceKey, c SigningContext, item document, verificationKey string) (document, error) {
	bad := errors.New("untrusted execution signing request; inspect the original approval before retrying")
	payload, id := stringAt(item, "requestPayload"), stringAt(item, "requestId")
	if id == "" || payload == "" || len(payload) > 128*1024 {
		return nil, bad
	}
	if verificationKey == "" {
		verificationKey = executionVerificationKey(c.Resource)
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
