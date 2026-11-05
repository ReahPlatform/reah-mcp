package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
)

const registrationInstructions = "OAuth is connected; financial signing is not yet verified. An authorized Owner, Admin or Operator (Member) must finish credential registration for the selected businesses on this second page. If Reah reports expired browser signing access or REAUTH_REQUIRED, sign in to the Reah website again with email verification or Passkey, then reopen this registration page. Do not replace the MCP connection to fix a browser session."

type connectionError struct{ Code, Message string }

func (e *connectionError) Error() string { return e.Code + ": " + e.Message }

// Only recognized error codes are displayed. Never echo response descriptions,
// request URLs or transport errors, which can contain credentials or HTML.
func oauthHTTPError(r *http.Response, path string) error {
	var body struct {
		Error string `json:"error"`
	}
	_ = json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&body)
	code, message := "request_failed", "Check connectivity and server availability before replacing a connection."
	switch {
	case r.StatusCode == 401 || body.Error == "invalid_grant" || body.Error == "invalid_token":
		code, message = "login_required", "OAuth authorization is no longer usable; run status for the exact login command."
	case body.Error == "credential_not_ready":
		code, message = "credential_not_ready", "Signing enrollment is unavailable for this grant; inspect status and the original login options."
	case r.StatusCode == 429:
		code, message = "rate_limited", "Wait before checking again; do not start a new login to bypass rate limits."
	case r.StatusCode == 403:
		code, message = "access_denied", "The request was denied. Check permissions and server or ingress policy."
		if path == "/mcp/oauth/register" {
			message = "Client registration was denied before browser consent. Check server or ingress policy; repeating browser login cannot fix this request."
		}
	}
	return &connectionError{code, fmt.Sprintf("%s (HTTP %d, %s)", message, r.StatusCode, path)}
}

func safeConnectionError(err error) error {
	var e *connectionError
	if errors.As(err, &e) {
		return e
	}
	return errConnection
}

func policyProfile(s *Session) string {
	if s.CredentialPolicyProfile == "financial-operations-v1" {
		return s.CredentialPolicyProfile
	}
	return "wallet-transfer-v1"
}

func (o *OAuth) action(name, message string, args ...string) document {
	if len(args) > 0 && (args[0] == "login" || args[0] == "enroll-key") {
		args = append(args, "--no-browser", "--json")
	}
	args = append(args, "--server", o.Config.Server, "--profile", o.Config.Profile)
	return document{"action": name, "message": message, "command": "reah-mcp", "args": args}
}

// ResumeRegistration preserves the OAuth grant and device key while the server's
// registration window remains open. It never extends that authorization window.
func (o *OAuth) ResumeRegistration(ctx context.Context, progress func(LoginStep) error) error {
	page, err := o.discover(ctx, true)
	if err != nil {
		return err
	}
	if _, err = o.AccessToken(ctx); err != nil {
		return err
	}
	var connectionID string
	err = o.Store.Exclusive(ctx, func() error {
		s, err := o.Store.Read()
		if err != nil {
			return err
		}
		if s == nil || s.Tokens == nil || !s.CredentialEnrollmentRequested {
			return errors.New("this connection did not request signing; a new login with --enroll-key requires user consent")
		}
		if s.Enrollment == nil || s.Enrollment.State == "awaiting_key_proof" {
			if err = o.prepareKey(ctx, s); err != nil {
				return err
			}
		}
		if s.Enrollment.State != "awaiting_provider" {
			return errors.New("credential registration expired or was revoked; check status before authorizing a new connection")
		}
		if !validUUID(s.Enrollment.ConnectionID) {
			return errEnrollment
		}
		d, err := o.request(ctx, "/mcp/credentials/status", document{}, s.Tokens.AccessToken)
		if err != nil {
			return err
		}
		if _, err = checkedEnrollment(d, s); err != nil {
			return err
		}
		if d["state"] == "expired" || d["state"] == "revoked" {
			return errors.New("the server's credential registration window is closed; run status --check to distinguish existing active bindings from incomplete registration before starting a new login")
		}
		expiry, err := time.Parse(time.RFC3339Nano, s.Enrollment.CredentialExpiresAt)
		if err != nil || !expiry.After(time.Now()) {
			return &connectionError{"login_required", "The credential's authorized duration has expired; authorize a new connection."}
		}
		connectionID = s.Enrollment.ConnectionID
		return nil
	})
	if err != nil {
		return err
	}
	u, _ := url.Parse(page)
	q := u.Query()
	q.Set("connectionId", connectionID)
	u.RawQuery = q.Encode()
	return progress(LoginStep{"credential_registration_required", registrationInstructions, u.String()})
}

// CheckStatus performs only authenticated reads, plus automatic OAuth refresh.
// Possession proof and a tool's presence never establish signing authorization.
func (r *Remote) CheckStatus(ctx context.Context) (document, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	out, err := r.OAuth.Status(ctx)
	if err != nil {
		return nil, err
	}
	if out["credentialsSaved"] != true {
		return out, nil
	}
	initialKey := out["publicKey"]
	tools, err := r.List(ctx)
	if err != nil {
		// Refresh may have consumed a token. Reload before choosing recovery.
		out, statusErr := r.OAuth.Status(ctx)
		if statusErr != nil {
			return nil, statusErr
		}
		out["readOnlyStatus"] = "unavailable"
		out["checkError"] = safeConnectionError(err).Error()
		if out["credentialsSaved"] == true {
			var ce *connectionError
			if errors.As(err, &ce) && ce.Code == "login_required" {
				out["phase"] = "login_required"
				out["nextAction"] = r.OAuth.loginAction(out)
			} else {
				out["phase"] = "connection_check_failed"
				out["nextAction"] = r.OAuth.action("retry_check", "Check connectivity or server policy, then retry status. Do not replace valid saved credentials because of a network error.", "status", "--check")
			}
		}
		return out, err
	}
	out, err = r.OAuth.Status(ctx)
	if err != nil {
		return nil, err
	}
	if out["credentialsSaved"] != true || out["publicKey"] != initialKey {
		out["phase"] = "connection_check_failed"
		out["nextAction"] = r.OAuth.action("retry_check", "The connection changed during validation; recheck before use.", "status", "--check")
		return out, errors.New("connection changed during check; run status --check again")
	}
	out["readOnlyStatus"], out["phase"] = "ready", "read_only_ready"
	out["checkedAt"] = time.Now().UTC().Format(time.RFC3339)
	out["nextAction"] = r.OAuth.action("reload_tools", "Reload the host's MCP connection and verify the authorized business list before use.", "status", "--check")
	if out["credentialEnrollmentRequested"] != true || out["enrollment"] == nil {
		if out["credentialEnrollmentRequested"] == true {
			out["phase"] = "possession_proof_required"
			out["nextAction"] = r.OAuth.action("resume_registration", "Continue the current connection's possession proof and browser registration.", "enroll-key")
		}
		return out, nil
	}
	var s *Session
	err = r.OAuth.Store.Exclusive(ctx, func() error {
		var err error
		s, err = r.OAuth.Store.Read()
		return err
	})
	if err != nil || s == nil || s.Tokens == nil || s.Enrollment == nil {
		return out, errors.New("connection changed during check; run status --check again")
	}
	d, err := r.OAuth.request(ctx, "/mcp/credentials/status", document{}, s.Tokens.AccessToken)
	if err != nil {
		out["phase"], out["signingStatus"] = "signing_check_failed", "unverified"
		out["checkError"] = safeConnectionError(err).Error()
		out["nextAction"] = r.OAuth.action("retry_check", "Read access works, but credential status could not be checked. Do not infer signing permission or replace the grant.", "status", "--check")
		return out, err
	}
	ids, err := checkedEnrollment(d, s)
	if err != nil {
		out["phase"], out["signingStatus"] = "signing_check_failed", "unverified"
		out["nextAction"] = r.OAuth.action("retry_check", "Credential binding could not be verified. Do not sign; recheck this connection.", "status", "--check")
		return out, err
	}
	out["registrationWindowState"] = d["state"]
	if deadline, err := time.Parse(time.RFC3339Nano, stringAt(d, "enrollment_expires_at")); err == nil {
		out["registrationExpiresAt"] = deadline.UTC().Format(time.RFC3339)
	}
	if d["state"] == "revoked" {
		out["phase"], out["signingStatus"] = "credential_inactive", d["state"]
		out["nextAction"] = r.OAuth.loginAction(out)
		return out, nil
	}
	if d["state"] == "awaiting_key_proof" {
		out["phase"] = "possession_proof_required"
		out["nextAction"] = r.OAuth.action("resume_registration", "Resume possession proof for this connection.", "enroll-key")
		return out, nil
	}
	checks := []document{}
	ready := true
	unverified := false
	for _, id := range ids {
		item := document{"entityId": id, "status": "registration_required"}
		result, callErr := r.invoke(ctx, "reah_signing_context", document{"entityId": id})
		if callErr != nil {
			item["status"] = "unverified"
		} else if result.IsError {
			var failure struct {
				Error struct {
					Code string `json:"code"`
				} `json:"error"`
			}
			body, _ := json.Marshal(result.StructuredContent)
			if json.Unmarshal(body, &failure) != nil || failure.Error.Code != "CREDENTIAL_REQUIRED" {
				item["status"] = "unverified"
			}
		} else {
			c, bindingErr := signingContext(result.StructuredContent, r.OAuth.Config.Server, id, s.DeviceKey)
			if bindingErr == nil && c.ConnectionID == s.Enrollment.ConnectionID && c.Binding.PolicyProfile == policyProfile(s) {
				item["status"], item["policyProfile"], item["expiresAt"] = "ready", c.Binding.PolicyProfile, c.Binding.ExpiresAt
			} else {
				item["status"] = "unverified"
			}
		}
		ready = ready && item["status"] == "ready"
		unverified = unverified || item["status"] == "unverified"
		checks = append(checks, item)
	}
	out["businesses"] = checks
	out["signingStatus"], out["phase"] = "registration_required", "credential_registration_required"
	out["nextAction"] = r.OAuth.action("resume_registration", registrationInstructions, "enroll-key")
	// Hodor's enrollment status expires at the short registration deadline,
	// including after successful registration. Existing live bindings outlast it.
	if d["state"] == "expired" && !ready {
		out["signingStatus"], out["phase"] = "registration_incomplete", "registration_expired"
		out["nextAction"] = r.OAuth.loginAction(out)
		out["nextAction"].(document)["message"] = "Read access still works, but at least one signing registration is incomplete and the registration window has closed. To authorize those businesses, the user must consent to a new connection; this replaces the existing grant."
	}
	if unverified {
		out["signingStatus"], out["phase"] = "unverified", "signing_check_failed"
		out["nextAction"] = r.OAuth.action("retry_check", "At least one business's signing binding could not be verified. Recheck before use; do not repeat login for a failed diagnostic.", "status", "--check")
	}
	if ready {
		signed := []string{}
		for _, tool := range tools {
			if p, _ := localProtocol(tool); p.Mode == "signed" {
				signed = append(signed, tool.Name)
			}
		}
		out["signedTools"] = signed
		out["signingStatus"], out["phase"] = "ready", "signing_ready"
		if len(signed) == 0 {
			out["phase"] = "server_capability_unavailable"
			out["nextAction"] = r.OAuth.action("check_server_capabilities", "Credentials are registered, but the live catalog exposes no supported signing tools. Check the server feature flag and client protocol version; do not repeat login.", "status", "--check")
		}
	}
	// A concurrent login must not turn an old grant's checks into new readiness.
	if err = r.OAuth.Store.Exclusive(ctx, func() error {
		current, err := r.OAuth.Store.Read()
		if err != nil {
			return err
		}
		if current == nil || current.Tokens == nil || current.DeviceKey.PublicKey != s.DeviceKey.PublicKey || current.ClientID != s.ClientID || current.Enrollment == nil || current.Enrollment.ConnectionID != s.Enrollment.ConnectionID {
			return errors.New("connection changed during check; run status --check again")
		}
		return nil
	}); err != nil {
		out["signingStatus"], out["phase"] = "unverified", "connection_check_failed"
		out["nextAction"] = r.OAuth.action("retry_check", "The connection changed during validation; recheck before use.", "status", "--check")
		return out, err
	}
	return out, nil
}

func (o *OAuth) loginAction(status document) document {
	args := []string{"login"}
	if status["credentialEnrollmentRequested"] == true {
		args = append(args, "--enroll-key", "--policy-profile", stringAt(status, "policyProfile"))
	}
	return o.action("login", "Authorize a new connection with user consent. This replaces the previous grant.", args...)
}

func checkedEnrollment(d document, s *Session) ([]string, error) {
	ids, ok := d["entity_ids"].([]any)
	expiry, err := time.Parse(time.RFC3339Nano, stringAt(d, "credential_expires_at"))
	state := stringAt(d, "state")
	if !ok || len(ids) < 1 || len(ids) > 100 || err != nil || d["connection_id"] != s.Enrollment.ConnectionID || d["public_key"] != s.DeviceKey.PublicKey || d["credential_expires_at"] != s.Enrollment.CredentialExpiresAt || !slices.Contains([]string{"awaiting_provider", "awaiting_key_proof", "expired", "revoked"}, state) {
		return nil, errEnrollment
	}
	if state == "awaiting_provider" || state == "awaiting_key_proof" {
		if !expiry.After(time.Now()) || digest(stringAt(d, "proof_payload")) != s.Enrollment.ProofHash {
			return nil, errEnrollment
		}
		var proof struct {
			Targets []struct {
				EntityID string `json:"entityId"`
			} `json:"targets"`
		}
		if json.Unmarshal([]byte(stringAt(d, "proof_payload")), &proof) != nil || len(proof.Targets) != len(ids) {
			return nil, errEnrollment
		}
		for i, id := range ids {
			if id != proof.Targets[i].EntityID {
				return nil, errEnrollment
			}
		}
	}
	out := make([]string, 0, len(ids))
	for _, value := range ids {
		id, ok := value.(string)
		if !ok || !validUUID(id) || len(out) > 0 && strings.Compare(id, out[len(out)-1]) <= 0 {
			return nil, errEnrollment
		}
		out = append(out, id)
	}
	return out, nil
}
