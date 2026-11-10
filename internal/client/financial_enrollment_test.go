package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"
)

func financialEnrollmentFixture(t *testing.T, c Config, s *Session, profile any) document {
	t.Helper()
	d := proofFixture(t, c, s)
	if profile == nil {
		return d
	}
	var proof document
	if err := json.Unmarshal([]byte(stringAt(d, "proof_payload")), &proof); err != nil {
		t.Fatal(err)
	}
	proof["policyProfile"] = profile
	b, err := json.Marshal(proof)
	if err != nil {
		t.Fatal(err)
	}
	d["proof_payload"] = string(b)
	return d
}

func TestFinancialEnrollmentProfileMustMatchExplicitSelection(t *testing.T) {
	c, err := Configure(DefaultServer, "financial-enrollment")
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"", "wallet-transfer-v1", "financial-operations-v1", "unknown-profile"} {
		for _, advertised := range []any{nil, "", "wallet-transfer-v1", "financial-operations-v1", "unknown-profile", true} {
			name := expected + "/"
			if advertised == nil {
				name += "omitted"
			} else {
				b, _ := json.Marshal(advertised)
				name += string(b)
			}
			t.Run(name, func(t *testing.T) {
				s := &Session{ClientID: "client", DeviceKey: testKey(t), CredentialPolicyProfile: expected}
				d := financialEnrollmentFixture(t, c, s, advertised)
				r, payload, err := parseEnrollment(d, c, s)
				want := (expected == "" || expected == "wallet-transfer-v1") && advertised == nil || expected == "financial-operations-v1" && advertised == "financial-operations-v1"
				if (err == nil) != want {
					t.Fatalf("profile acceptance = %v, want %v: %v", err == nil, want, err)
				}
				if want && (r.State != "awaiting_key_proof" || payload != stringAt(d, "proof_payload")) {
					t.Fatal("accepted proof bytes or state changed")
				}
				if s.CredentialPolicyProfile != expected {
					t.Fatal("proof silently changed requested authorization profile")
				}
			})
		}
	}
}

func TestFinancialEnrollmentFrozenProofCannotBeReplaced(t *testing.T) {
	c, _ := Configure(DefaultServer, "frozen-financial")
	s := &Session{ClientID: "client", DeviceKey: testKey(t), CredentialPolicyProfile: "financial-operations-v1"}
	d := financialEnrollmentFixture(t, c, s, "financial-operations-v1")
	r, _, err := parseEnrollment(d, c, s)
	if err != nil {
		t.Fatal(err)
	}
	s.Enrollment = r
	for _, field := range []string{"policyProfile", "nonce", "userId", "authorizationCreatedAt"} {
		t.Run(field, func(t *testing.T) {
			copy := cloneDocument(d)
			var proof document
			if err := json.Unmarshal([]byte(stringAt(d, "proof_payload")), &proof); err != nil {
				t.Fatal(err)
			}
			switch field {
			case "policyProfile":
				proof[field] = "wallet-transfer-v1"
			case "nonce":
				proof[field] = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
			case "userId":
				proof[field] = testID
			case "authorizationCreatedAt":
				proof[field] = time.Now().Add(-2 * time.Minute).UTC().Format(time.RFC3339Nano)
			}
			b, err := json.Marshal(proof)
			if err != nil {
				t.Fatal(err)
			}
			copy["proof_payload"] = string(b)
			if _, _, err := parseEnrollment(copy, c, s); err == nil {
				t.Fatal("replaced persisted financial challenge")
			}
		})
	}
}

func TestFinancialEnrollmentPrepareProveUsesExactProfileAndProof(t *testing.T) {
	for _, profile := range []string{"", "wallet-transfer-v1", "financial-operations-v1"} {
		t.Run(profile, func(t *testing.T) {
			var d document
			key := testKey(t)
			prepareCalls, proveCalls := 0, 0
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer token" {
					t.Error("missing bearer authorization")
				}
				var input document
				if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
					t.Error(err)
				}
				switch r.URL.Path {
				case "/mcp/credentials/prepare":
					want := document{}
					if profile == "financial-operations-v1" {
						want["policyProfile"] = profile
					}
					if !reflect.DeepEqual(input, want) {
						t.Errorf("prepare body = %v, want %v", input, want)
					}
					prepareCalls++
				case "/mcp/credentials/prove":
					if !exactFields(input, "signature") {
						t.Error("proof submission must contain only the local signature")
					}
					verifyText(t, key, stringAt(d, "proof_payload"), stringAt(input, "signature"), false)
					d["state"] = "awaiting_provider"
					proveCalls++
				default:
					t.Errorf("unexpected enrollment request: %s", r.URL.Path)
				}
				if err := json.NewEncoder(w).Encode(d); err != nil {
					t.Error(err)
				}
			}))
			defer server.Close()
			c, _ := Configure(server.URL+"/mcp", "financial-enrollment")
			s := &Session{Version: 1, Server: c.Server, ClientID: "client", DeviceKey: key, CredentialEnrollmentRequested: true, CredentialPolicyProfile: profile, Tokens: &Tokens{"token", "refresh", time.Now().Add(time.Hour).UnixMilli()}}
			var proofProfile any
			if profile == "financial-operations-v1" {
				proofProfile = profile
			}
			d = financialEnrollmentFixture(t, c, s, proofProfile)
			store := &memoryStore{s: s}
			o := &OAuth{Config: c, Store: store, HTTP: server.Client()}
			if err := o.EnrollKey(context.Background()); err != nil {
				t.Fatal(err)
			}
			if err := o.EnrollKey(context.Background()); err != nil {
				t.Fatal(err)
			}
			if prepareCalls != 2 || proveCalls != 1 || store.s.Enrollment.State != "awaiting_provider" || store.s.CredentialPolicyProfile != profile {
				t.Fatal("proof replayed, profile upgraded or registration state lost")
			}
		})
	}
}

func TestFinancialEnrollmentRejectsInvalidLoginBeforeNetworkOrVaultChanges(t *testing.T) {
	c, _ := Configure(DefaultServer, "financial-enrollment")
	s := &Session{ClientID: "existing", DeviceKey: testKey(t), CredentialPolicyProfile: "wallet-transfer-v1"}
	store := &memoryStore{s: s}
	calls := 0
	o := &OAuth{Config: c, Store: store, HTTP: &http.Client{Transport: roundTripper(func(*http.Request) (*http.Response, error) {
		calls++
		return nil, errConnection
	})}}
	for _, input := range []struct {
		enroll  bool
		profile string
	}{{true, "unknown-profile"}, {true, ""}, {false, "financial-operations-v1"}} {
		if err := o.LoginWithPolicyProfile(context.Background(), input.enroll, input.profile, func(string) error {
			t.Error("opened consent for invalid request")
			return nil
		}); err == nil {
			t.Fatal("accepted invalid profile or financial signing without enrollment")
		}
	}
	if calls != 0 || store.s != s || s.CredentialPolicyProfile != "wallet-transfer-v1" {
		t.Fatal("invalid profile touched network or existing authorization")
	}
}

func TestEnrollmentRequiresOriginalAuthorizationWindow(t *testing.T) {
	c, _ := Configure(DefaultServer, "authorization-window")
	for _, profile := range []string{"wallet-transfer-v1", "financial-operations-v1"} {
		t.Run(profile, func(t *testing.T) {
			s := &Session{ClientID: "client", DeviceKey: testKey(t), CredentialPolicyProfile: profile}
			var advertised any
			if profile == "financial-operations-v1" {
				advertised = profile
			}
			for _, name := range []string{"missing", "null", "invalid", "zero", "future", "extended", "exact-24h", "valid"} {
				t.Run(name, func(t *testing.T) {
					d := financialEnrollmentFixture(t, c, s, advertised)
					var proof document
					if err := json.Unmarshal([]byte(stringAt(d, "proof_payload")), &proof); err != nil {
						t.Fatal(err)
					}
					expiry, err := time.Parse(time.RFC3339Nano, stringAt(d, "credential_expires_at"))
					if err != nil {
						t.Fatal(err)
					}
					switch name {
					case "missing":
						delete(proof, "authorizationCreatedAt")
					case "null":
						proof["authorizationCreatedAt"] = nil
					case "invalid":
						proof["authorizationCreatedAt"] = "not-a-time"
					case "zero":
						proof["authorizationCreatedAt"] = time.Time{}.Format(time.RFC3339Nano)
					case "future":
						proof["authorizationCreatedAt"] = time.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano)
					case "extended":
						proof["authorizationCreatedAt"] = expiry.Add(-24*time.Hour - time.Nanosecond).Format(time.RFC3339Nano)
					case "exact-24h":
						proof["authorizationCreatedAt"] = expiry.Add(-24 * time.Hour).Format(time.RFC3339Nano)
					}
					raw, err := json.Marshal(proof)
					if err != nil {
						t.Fatal(err)
					}
					d["proof_payload"] = string(raw)
					_, payload, err := parseEnrollment(d, c, s)
					want := name == "valid" || name == "exact-24h"
					if (err == nil) != want {
						t.Fatalf("acceptance = %v, want %v: %v", err == nil, want, err)
					}
					if want && payload != string(raw) {
						t.Fatal("rewrote proof bytes before signing")
					}
				})
			}
		})
	}
}
