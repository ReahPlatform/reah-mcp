package client

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/google/uuid"
)

func validUUID(s string) bool {
	id, e := uuid.Parse(s)
	return e == nil && id != uuid.Nil && id.String() == s
}
func digest(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }

var errEnrollment = errors.New("invalid or mismatched credential proof; no signature was sent")

func exactFields(d document, fields ...string) bool {
	if len(d) != len(fields) {
		return false
	}
	for _, k := range fields {
		if _, ok := d[k]; !ok {
			return false
		}
	}
	return true
}

func parseEnrollment(d document, c Config, s *Session) (*Receipt, string, error) {
	id, state, payload := stringAt(d, "connection_id"), stringAt(d, "state"), stringAt(d, "proof_payload")
	keyBytes, e := hex.DecodeString(s.DeviceKey.PublicKey)
	if e != nil {
		return nil, "", errEnrollment
	}
	h := sha256.Sum256(keyBytes)
	ids, ok := d["entity_ids"].([]any)
	deadline, e1 := time.Parse(time.RFC3339Nano, stringAt(d, "enrollment_expires_at"))
	expiry, e2 := time.Parse(time.RFC3339Nano, stringAt(d, "credential_expires_at"))
	if !validUUID(id) || d["public_key"] != s.DeviceKey.PublicKey || d["public_key_fingerprint"] != hex.EncodeToString(h[:]) || !ok || len(ids) < 1 || len(ids) > 100 || e1 != nil || e2 != nil || deadline.After(expiry) || len(payload) > 65536 || !slices.Contains([]string{"awaiting_key_proof", "awaiting_provider", "expired", "revoked"}, state) {
		return nil, "", errEnrollment
	}
	for i, v := range ids {
		str, ok := v.(string)
		if !ok || !validUUID(str) || (i > 0 && str <= ids[i-1].(string)) {
			return nil, "", errEnrollment
		}
	}
	if s.Enrollment != nil && (s.Enrollment.ConnectionID != id || s.Enrollment.CredentialExpiresAt != d["credential_expires_at"]) {
		return nil, "", errEnrollment
	}
	if state == "awaiting_key_proof" || state == "awaiting_provider" {
		now := time.Now()
		if !deadline.After(now) || deadline.After(now.Add(610*time.Second)) || expiry.After(now.Add(30*24*time.Hour)) {
			return nil, "", errEnrollment
		}
		var p document
		fields := []string{"purpose", "resource", "clientId", "connectionId", "userId", "publicKey", "nonce", "enrollmentExpiresAt", "credentialExpiresAt", "authorizationCreatedAt", "targets"}
		if s.CredentialPolicyProfile == "financial-operations-v1" {
			fields = append(fields, "policyProfile")
		} else if s.CredentialPolicyProfile != "" && s.CredentialPolicyProfile != "wallet-transfer-v1" {
			return nil, "", errEnrollment
		}
		if json.Unmarshal([]byte(payload), &p) != nil || !exactFields(p, fields...) || (s.CredentialPolicyProfile == "financial-operations-v1" && p["policyProfile"] != s.CredentialPolicyProfile) || p["purpose"] != "reah.mcp.credential-possession.v1" || p["resource"] != c.Server || p["clientId"] != s.ClientID || p["connectionId"] != id || !validUUID(stringAt(p, "userId")) || p["publicKey"] != s.DeviceKey.PublicKey {
			return nil, "", errEnrollment
		}
		nonce := stringAt(p, "nonce")
		b, e := hex.DecodeString(nonce)
		if e != nil || len(b) != 32 || hex.EncodeToString(b) != nonce {
			return nil, "", errEnrollment
		}
		pd, e1 := time.Parse(time.RFC3339Nano, stringAt(p, "enrollmentExpiresAt"))
		pe, e2 := time.Parse(time.RFC3339Nano, stringAt(p, "credentialExpiresAt"))
		created, e3 := time.Parse(time.RFC3339Nano, stringAt(p, "authorizationCreatedAt"))
		// Financial authority ends at the original consent deadline, not a new
		// lifetime measured from preparation or token refresh.
		if e1 != nil || e2 != nil || e3 != nil || created.IsZero() || created.After(now) || !deadline.After(created) || !expiry.After(created) || expiry.After(created.Add(24*time.Hour)) || !pd.Equal(deadline) || !pe.Equal(expiry) {
			return nil, "", errEnrollment
		}
		ts, ok := p["targets"].([]any)
		if !ok || len(ts) != len(ids) {
			return nil, "", errEnrollment
		}
		for i, v := range ts {
			t, ok := v.(map[string]any)
			if !ok || !exactFields(t, "entityId", "suborgId", "authorizerTurnkeyUserId") || t["entityId"] != ids[i] || len(stringAt(t, "suborgId")) < 1 || len(stringAt(t, "suborgId")) > 512 || len(stringAt(t, "authorizerTurnkeyUserId")) < 1 || len(stringAt(t, "authorizerTurnkeyUserId")) > 512 {
				return nil, "", errEnrollment
			}
		}
		if s.Enrollment != nil && s.Enrollment.ProofHash != digest(payload) {
			return nil, "", errEnrollment
		}
	} else if payload != "" {
		return nil, "", errEnrollment
	}
	return &Receipt{id, state, stringAt(d, "credential_expires_at"), digest(payload)}, payload, nil
}

func (o *OAuth) prepareKey(ctx context.Context, s *Session) error {
	if !s.CredentialEnrollmentRequested || s.Tokens == nil {
		return errors.New("start a new login with --enroll-key before registering a signing credential")
	}
	input := document{}
	if s.CredentialPolicyProfile == "financial-operations-v1" {
		input["policyProfile"] = s.CredentialPolicyProfile
	} else if s.CredentialPolicyProfile != "" && s.CredentialPolicyProfile != "wallet-transfer-v1" {
		return errEnrollment
	}
	d, e := o.request(ctx, "/mcp/credentials/prepare", input, s.Tokens.AccessToken)
	if e != nil {
		return e
	}
	r, p, e := parseEnrollment(d, o.Config, s)
	if e != nil {
		return e
	}
	s.Enrollment = r
	if e = o.Store.Write(s); e != nil {
		return e
	}
	if r.State == "awaiting_provider" {
		return nil
	}
	if r.State != "awaiting_key_proof" {
		return errors.New("credential registration expired or was revoked; reconnect")
	}
	sig, e := signText(s.DeviceKey, p, false)
	if e != nil {
		return e
	}
	d, e = o.request(ctx, "/mcp/credentials/prove", document{"signature": sig}, s.Tokens.AccessToken)
	if e != nil {
		return e
	}
	r, _, e = parseEnrollment(d, o.Config, s)
	if e != nil {
		return e
	}
	if r.State != "awaiting_provider" {
		return errors.New("credential possession is not confirmed; resume enroll-key")
	}
	s.Enrollment = r
	return o.Store.Write(s)
}

func (o *OAuth) EnrollKey(ctx context.Context) error {
	token, e := o.AccessToken(ctx)
	if e != nil {
		return e
	}
	return o.Store.Exclusive(ctx, func() error {
		s, e := o.Store.Read()
		if e != nil {
			return e
		}
		if s == nil || s.Tokens == nil || s.Tokens.AccessToken != token {
			return errors.New("connection changed; run enroll-key again")
		}
		return o.prepareKey(ctx, s)
	})
}

func (o *OAuth) signingKey(ctx context.Context) (DeviceKey, error) {
	var k DeviceKey
	e := o.Store.Exclusive(ctx, func() error {
		s, e := o.Store.Read()
		if e != nil {
			return e
		}
		if s == nil || s.Tokens == nil || !s.CredentialEnrollmentRequested || s.Enrollment == nil || s.Enrollment.State != "awaiting_provider" {
			return errors.New("log in with --enroll-key and complete registration in Reah first")
		}
		k = s.DeviceKey
		return nil
	})
	return k, e
}
