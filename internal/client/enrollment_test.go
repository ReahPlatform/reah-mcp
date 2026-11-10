package client

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func proofFixture(t *testing.T, c Config, s *Session) document {
	t.Helper()
	now := time.Now()
	deadline := now.Add(5 * time.Minute).UTC().Format(time.RFC3339Nano)
	expiry := now.Add(time.Hour).UTC().Format(time.RFC3339Nano)
	payload, _ := json.Marshal(document{"purpose": "reah.mcp.credential-possession.v1", "resource": c.Server, "clientId": s.ClientID, "connectionId": testID, "userId": testOtherID, "publicKey": s.DeviceKey.PublicKey, "nonce": strings.Repeat("a", 64), "enrollmentExpiresAt": deadline, "credentialExpiresAt": expiry, "authorizationCreatedAt": now.Add(-time.Minute).UTC().Format(time.RFC3339Nano), "targets": []any{document{"entityId": testID, "suborgId": testID, "authorizerTurnkeyUserId": testOtherID}, document{"entityId": testOtherID, "suborgId": testOtherID, "authorizerTurnkeyUserId": testOtherID}}})
	key, _ := hex.DecodeString(s.DeviceKey.PublicKey)
	h := sha256.Sum256(key)
	return document{"connection_id": testID, "public_key": s.DeviceKey.PublicKey, "public_key_fingerprint": hex.EncodeToString(h[:]), "entity_ids": []any{testID, testOtherID}, "state": "awaiting_key_proof", "proof_payload": string(payload), "enrollment_expires_at": deadline, "credential_expires_at": expiry}
}

func TestEnrollmentSignsExactMultiEntityProofAndRefusesTampering(t *testing.T) {
	c, _ := Configure(DefaultServer, "test")
	s := &Session{Version: 1, Server: c.Server, ClientID: "test-client", DeviceKey: testKey(t), CredentialEnrollmentRequested: true, Tokens: &Tokens{"token", "refresh", time.Now().Add(time.Hour).UnixMilli()}}
	d := proofFixture(t, c, s)
	r, p, e := parseEnrollment(d, c, s)
	if e != nil || r.State != "awaiting_key_proof" {
		t.Fatal(e)
	}
	sig, e := signText(s.DeviceKey, p, false)
	if e != nil {
		t.Fatal(e)
	}
	verifyText(t, s.DeviceKey, p, sig, false)
	for _, field := range []string{"purpose", "resource", "clientId", "connectionId", "publicKey", "nonce"} {
		var tampered document
		_ = json.Unmarshal([]byte(stringAt(d, "proof_payload")), &tampered)
		tampered[field] = "untrusted"
		b, _ := json.Marshal(tampered)
		copy := cloneDocument(d)
		copy["proof_payload"] = string(b)
		if _, _, e = parseEnrollment(copy, c, s); e == nil {
			t.Errorf("signed mismatched %s", field)
		}
	}
	copy := cloneDocument(d)
	copy["entity_ids"] = []any{testOtherID, testID}
	if _, _, e = parseEnrollment(copy, c, s); e == nil {
		t.Fatal("signed reordered entities")
	}
	s.Enrollment = r
	copy = cloneDocument(d)
	copy["proof_payload"] = stringAt(d, "proof_payload") + " "
	if _, _, e = parseEnrollment(copy, c, s); e == nil {
		t.Fatal("changed persisted challenge")
	}
	if _, e = (&OAuth{Config: c, Store: &memoryStore{s: s}}).signingKey(context.Background()); e == nil {
		t.Fatal("unproven key allowed signing")
	}
}

func TestEnrollmentPrepareProveResume(t *testing.T) {
	var o *OAuth
	var d document
	calls := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer token" {
			t.Error("missing bearer token")
		}
		if r.URL.Path == "/mcp/credentials/prove" {
			var input document
			_ = json.NewDecoder(r.Body).Decode(&input)
			s, _ := o.Store.Read()
			verifyText(t, s.DeviceKey, stringAt(d, "proof_payload"), stringAt(input, "signature"), false)
			d["state"] = "awaiting_provider"
			calls++
		}
		_ = json.NewEncoder(w).Encode(d)
	}))
	defer server.Close()
	c, _ := Configure(server.URL+"/mcp", "test")
	s := &Session{Version: 1, Server: c.Server, ClientID: "client", DeviceKey: testKey(t), CredentialEnrollmentRequested: true, Tokens: &Tokens{"token", "refresh", time.Now().Add(time.Hour).UnixMilli()}}
	d = proofFixture(t, c, s)
	store := &memoryStore{s: s}
	o = &OAuth{c, store, server.Client()}
	if e := o.EnrollKey(context.Background()); e != nil {
		t.Fatal(e)
	}
	if e := o.EnrollKey(context.Background()); e != nil {
		t.Fatal(e)
	}
	if calls != 1 || store.s.Enrollment.State != "awaiting_provider" {
		t.Fatal("proof replayed or registration state missing")
	}
}

func TestVaultLocksAcrossInstancesAndRejectsUnsafeFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	c, _ := Configure(DefaultServer, "lock-test")
	a, b := &Vault{Config: c, Directory: dir}, &Vault{Config: c, Directory: dir}
	started, release := make(chan struct{}), make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		if e := a.Exclusive(context.Background(), func() error { close(started); <-release; return nil }); e != nil {
			t.Error(e)
			close(started)
		}
	}()
	<-started
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if e := b.Exclusive(ctx, func() error { t.Error("simultaneous refresh lock acquired"); return nil }); e == nil {
		t.Error("lock did not block")
	}
	close(release)
	wg.Wait()
	if e := b.Exclusive(context.Background(), func() error { return nil }); e != nil {
		t.Fatal(e)
	}
	if runtime.GOOS != "windows" {
		if e := os.Chmod(filepath.Join(dir, c.ID+".lock"), 0666); e != nil {
			t.Fatal(e)
		}
		if e := a.Exclusive(context.Background(), func() error { t.Error("unsafe file accepted"); return nil }); e == nil {
			t.Fatal("unsafe coordination file accepted")
		}
	}
}

func TestLogoutDisablesLocalAccessWhenRevokeFails(t *testing.T) {
	c, _ := Configure(DefaultServer, "test")
	store := &memoryStore{s: &Session{Version: 1, Server: c.Server, ClientID: "client", DeviceKey: testKey(t), Tokens: &Tokens{"token", "refresh", time.Now().Add(time.Hour).UnixMilli()}}}
	o := &OAuth{c, store, &http.Client{Transport: roundTripper(func(*http.Request) (*http.Response, error) { return nil, errConnection })}}
	if e := o.Logout(context.Background()); e == nil {
		t.Fatal("unconfirmed revocation reported as success")
	}
	if store.s.Tokens != nil || store.s.PendingRevocation != "refresh" {
		t.Fatal("local credentials remained usable")
	}
	if _, e := o.AccessToken(context.Background()); e == nil {
		t.Fatal("disabled connection still usable")
	}
}

func TestBearerReloadsTokensAndRejectsOtherDestinations(t *testing.T) {
	c, _ := Configure(DefaultServer, "test")
	store := &memoryStore{s: &Session{Tokens: &Tokens{"first", "refresh", time.Now().Add(time.Hour).UnixMilli()}}}
	o := &OAuth{Config: c, Store: store}
	calls := 0
	transport := bearerTransport{o, roundTripper(func(r *http.Request) (*http.Response, error) {
		calls++
		want := "Bearer first"
		if calls == 2 {
			want = "Bearer second"
		}
		if r.Header.Get("Authorization") != want {
			t.Error("stale bearer token")
		}
		return &http.Response{StatusCode: 200, Body: http.NoBody}, nil
	})}
	r, _ := http.NewRequest("POST", DefaultServer, nil)
	if _, e := transport.RoundTrip(r); e != nil {
		t.Fatal(e)
	}
	store.s.Tokens.AccessToken = "second"
	if _, e := transport.RoundTrip(r); e != nil {
		t.Fatal(e)
	}
	r, _ = http.NewRequest("POST", "https://other.example/mcp", nil)
	if _, e := transport.RoundTrip(r); e == nil || calls != 2 {
		t.Fatal("bearer leaked to another destination")
	}
}
