package client

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const credentialPolicyBodyLimit = 64 * 1024
const signingEntityNotReady = "ENTITY_SIGNING_NOT_READY"
const financialAuthorizationTooLong = "FINANCIAL_AUTHORIZATION_TOO_LONG"
const credentialEnrollmentNotFound = "CREDENTIAL_ENROLLMENT_NOT_FOUND"

type blockedSigningEntity struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Runtime diagnostics only. Business lists never enter the OS credential store.
type credentialPolicyError struct {
	Reason          string
	NextAction      string
	BlockedEntities []blockedSigningEntity
}

func (p *credentialPolicyError) message() string {
	if p.Reason == credentialEnrollmentNotFound {
		return "No signing enrollment exists for this connection. Explicitly resume enroll-key to prepare its existing authorization; read access is unchanged."
	}
	if p.Reason == financialAuthorizationTooLong {
		return "Financial signing requires authorization for 1 hour or 1 day (24 hours or less). Start a new login with user consent and choose a shorter duration. Read access is unaffected. Run status --check for the login command with your original signing policy."
	}
	message := "These businesses have read access only. Ask a business Owner to enable signing access or check registration before attempting financial actions. Repeating enrollment does not grant access. Other businesses may have separate signing access."
	for _, entity := range p.BlockedEntities {
		message += fmt.Sprintf("\n- %s (%s)", strconv.Quote(entity.Name), entity.ID)
	}
	return message
}

// Reject duplicate fields and trailing documents before interpreting a policy.
func policyFields(raw []byte) (map[string]json.RawMessage, error) {
	d := json.NewDecoder(bytes.NewReader(raw))
	token, err := d.Token()
	if err != nil || token != json.Delim('{') {
		return nil, errors.New("invalid policy object")
	}
	fields := map[string]json.RawMessage{}
	for d.More() {
		token, err = d.Token()
		key, ok := token.(string)
		if err != nil || !ok {
			return nil, errors.New("invalid policy field")
		}
		if _, exists := fields[key]; exists {
			return nil, errors.New("duplicate policy field")
		}
		var value json.RawMessage
		if err = d.Decode(&value); err != nil {
			return nil, err
		}
		fields[key] = value
	}
	if token, err = d.Token(); err != nil || token != json.Delim('}') {
		return nil, errors.New("invalid policy object")
	}
	if _, err = d.Token(); err != io.EOF {
		return nil, errors.New("trailing policy document")
	}
	return fields, nil
}

func parseCredentialPolicy(raw []byte, status int, path string) *credentialPolicyError {
	if status != http.StatusConflict || len(raw) > credentialPolicyBodyLimit || !utf8.Valid(raw) {
		return nil
	}
	switch path {
	case "/mcp/credentials/prepare", "/mcp/credentials/prove", "/mcp/credentials/status":
	default:
		return nil
	}
	fields, err := policyFields(raw)
	if err != nil {
		return nil
	}
	var reason, action, code string
	var retryable *bool
	if json.Unmarshal(fields["reason"], &reason) != nil || json.Unmarshal(fields["next_action"], &action) != nil ||
		json.Unmarshal(fields["error"], &code) != nil || json.Unmarshal(fields["retryable"], &retryable) != nil || retryable == nil || *retryable {
		return nil
	}
	p := &credentialPolicyError{Reason: reason, NextAction: action}
	switch reason {
	case credentialEnrollmentNotFound:
		if path != "/mcp/credentials/status" || code != "credential_not_ready" || action != "enroll_key" {
			return nil
		}
	case financialAuthorizationTooLong:
		var maximum int
		if code != "financial_authorization_too_long" || action != "reauthorize" || json.Unmarshal(fields["max_authorization_seconds"], &maximum) != nil || maximum != 86400 {
			return nil
		}
	case signingEntityNotReady:
		if code != "credential_not_ready" || action != "review_business_selection" {
			return nil
		}
		var entities []json.RawMessage
		if json.Unmarshal(fields["blocked_entities"], &entities) != nil || len(entities) < 1 || len(entities) > 100 {
			return nil
		}
		seen := map[string]bool{}
		for _, rawEntity := range entities {
			entityFields, err := policyFields(rawEntity)
			if err != nil {
				return nil
			}
			var entity blockedSigningEntity
			if json.Unmarshal(entityFields["id"], &entity.ID) != nil || json.Unmarshal(entityFields["name"], &entity.Name) != nil ||
				!validUUID(entity.ID) || seen[entity.ID] || len(entity.Name) > 1024 || strings.TrimSpace(entity.Name) == "" || !utf8.ValidString(entity.Name) ||
				strings.ContainsFunc(entity.Name, func(r rune) bool { return unicode.IsControl(r) || unicode.Is(unicode.Cf, r) }) {
				return nil
			}
			seen[entity.ID] = true
			p.BlockedEntities = append(p.BlockedEntities, entity)
		}
	default:
		return nil
	}
	return p
}

func (o *OAuth) applyCredentialPolicy(out document, err error) bool {
	var ce *connectionError
	if !errors.As(err, &ce) {
		return false
	}
	if ce.Policy != nil {
		p := ce.Policy
		if p.Reason == credentialEnrollmentNotFound {
			return false
		}
		out["reason"], out["retryable"] = p.Reason, false
		if p.Reason == signingEntityNotReady {
			out["phase"], out["signingStatus"] = "read_only_ready", "partially_unavailable"
			out["blockedEntities"] = p.BlockedEntities
			out["nextAction"] = o.action("review_business_access", p.message(), "status", "--check")
			out["signingNote"] = p.message()
		} else {
			out["phase"], out["signingStatus"] = "signing_authorization_required", "not_ready"
			out["maxAuthorizationSeconds"] = 86400
			next := o.loginAction(out)
			next["action"], next["message"] = p.NextAction, p.message()+" This replaces the previous grant only after user consent."
			out["nextAction"], out["checkError"] = next, ce.Error()
		}
		return true
	}
	if ce.Code == "credential_not_ready" {
		out["phase"], out["signingStatus"] = "signing_setup_unavailable", "not_ready"
		out["reason"], out["retryable"] = "CREDENTIAL_NOT_READY", false
		out["nextAction"] = o.action("inspect_connection", "Signing setup is not ready. Inspect this connection's registration status and authorization settings before trying again; do not blindly repeat enroll-key or replace working read access.", "status", "--check")
		out["checkError"] = ce.Error()
		return true
	}
	return false
}
