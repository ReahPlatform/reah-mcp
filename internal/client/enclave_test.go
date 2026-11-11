package client

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestEnclaveOutputBound(t *testing.T) {
	var output limitedEnclaveOutput
	if _, err := io.Copy(&output, strings.NewReader(strings.Repeat("x", 4097))); err == nil || len(output.Bytes()) > 4096 {
		t.Fatal("native helper output bypassed its size limit")
	}
}

func enclaveMetadata(t *testing.T) DeviceKey {
	t.Helper()
	return DeviceKey{Storage: enclaveKeyStorage, PublicKey: testKey(t).PublicKey, EnclaveReference: base64.StdEncoding.EncodeToString([]byte("not-a-hardware-key"))}
}

func TestDeviceKeyStorageValidationAndLegacyCompatibility(t *testing.T) {
	legacy := testKey(t)
	raw, err := json.Marshal(legacy)
	if err != nil || strings.Contains(string(raw), "storage") {
		t.Fatal("legacy software vault format changed")
	}
	var restored DeviceKey
	if json.Unmarshal(raw, &restored) != nil || validateDeviceKey(restored) != nil {
		t.Fatal("cannot restore an existing software key")
	}
	hardware := enclaveMetadata(t)
	if err := validateDeviceKey(hardware); err != nil {
		t.Fatal("hardware metadata validation requires opening the device")
	}
	if _, err := privateKey(hardware); err == nil {
		t.Fatal("hardware key exposed an exportable software key")
	}
	for name, mutate := range map[string]func(*DeviceKey){
		"unknown backend": func(k *DeviceKey) { k.Storage = "unknown" },
		"mixed PEM":       func(k *DeviceKey) { k.PrivateKey = legacy.PrivateKey },
		"missing handle":  func(k *DeviceKey) { k.EnclaveReference = "" },
		"invalid base64":  func(k *DeviceKey) { k.EnclaveReference = "!" },
		"large handle":    func(k *DeviceKey) { k.EnclaveReference = base64.StdEncoding.EncodeToString(make([]byte, 1025)) },
		"invalid point":   func(k *DeviceKey) { k.PublicKey = "00" },
		"noncanonical":    func(k *DeviceKey) { k.PublicKey = strings.ToUpper(k.PublicKey) },
		"downgrade":       func(k *DeviceKey) { k.Storage = softwareKeyStorage },
	} {
		t.Run(name, func(t *testing.T) {
			key := hardware
			mutate(&key)
			if validateDeviceKey(key) == nil {
				t.Fatal("accepted invalid hardware metadata")
			}
			if _, err := signText(key, "request", true); err == nil {
				t.Fatal("signed with invalid hardware metadata")
			}
		})
	}
}

func TestEnclaveSignatureFormatsAndRequestBinding(t *testing.T) {
	key := testKey(t)
	digest := sha256.Sum256([]byte("exact financial protocol bytes"))
	signature, err := signText(key, "exact financial protocol bytes", true)
	if err != nil {
		t.Fatal(err)
	}
	der, _ := hex.DecodeString(signature)
	for _, useDER := range []bool{false, true} {
		value, err := checkedEnclaveSignature(key.PublicKey, digest[:], der, useDER)
		if err != nil {
			t.Fatal(err)
		}
		verifyText(t, key, "exact financial protocol bytes", value, useDER)
	}
	otherDigest := sha256.Sum256([]byte("changed financial request"))
	for _, input := range []struct {
		public string
		digest []byte
		sig    []byte
	}{
		{key.PublicKey, otherDigest[:], der},
		{testKey(t).PublicKey, digest[:], der},
		{key.PublicKey, digest[:31], der},
		{key.PublicKey, digest[:], append(append([]byte{}, der...), 0)},
		{key.PublicKey, digest[:], []byte{0x30, 0}},
		{"00", digest[:], der},
	} {
		if _, err := checkedEnclaveSignature(input.public, input.digest, input.sig, false); err == nil {
			t.Fatal("accepted a signature for another key, digest or malformed DER")
		}
	}
}

func TestPlatformKeyStorageCapabilityAndDowngradeProtection(t *testing.T) {
	probeErr := errors.New("device locked or helper unavailable")
	for _, platform := range []string{"darwin", "linux", "windows"} {
		for _, session := range []*Session{nil, {DeviceKey: testKey(t)}, {DeviceKey: enclaveMetadata(t)}} {
			for _, probe := range []struct {
				supported bool
				err       error
			}{{true, nil}, {false, nil}, {false, probeErr}} {
				calls := 0
				storage, err := loginKeyStorage(platform, session, func() (bool, error) {
					calls++
					return probe.supported, probe.err
				})
				if session != nil && keyStorage(session.DeviceKey) == enclaveKeyStorage {
					if storage != enclaveKeyStorage || err != nil || calls != 0 {
						t.Fatal("existing hardware profile used fallback detection")
					}
					continue
				}
				if platform != "darwin" {
					if storage != softwareKeyStorage || err != nil || calls != 0 {
						t.Fatal("non-macOS software profile required native detection")
					}
					continue
				}
				if calls != 1 {
					t.Fatal("macOS login did not check native availability once")
				}
				if probe.err != nil {
					if !errors.Is(err, probeErr) || storage != "" {
						t.Fatal("probe failure silently selected a software key")
					}
					continue
				}
				want := softwareKeyStorage
				if probe.supported {
					want = enclaveKeyStorage
				}
				if storage != want || err != nil {
					t.Fatal("key storage did not match native capability")
				}
			}
		}
	}
	if _, err := generateStoredKey("unknown"); err == nil {
		t.Fatal("unknown key storage accepted")
	}
	if !enclaveBuildAvailable() {
		if _, err := generateStoredKey(enclaveKeyStorage); err == nil {
			t.Fatal("unsupported build fell back to software")
		}
		if _, err := signText(enclaveMetadata(t), "request", true); err == nil {
			t.Fatal("unsupported build signed a hardware key")
		}
	}
}

func TestEnclaveAvailabilityRequiresExplicitNativeResult(t *testing.T) {
	for _, supported := range []bool{true, false} {
		value := "unsupported"
		if supported {
			value = "available"
		}
		got, err := parseEnclaveAvailability(enclaveResponse{Availability: value})
		if err != nil || got != supported {
			t.Fatal("invalid explicit capability result")
		}
	}
	for _, response := range []enclaveResponse{
		{},
		{Availability: "unknown"},
		{Error: "key_unavailable"},
		{Error: "hardware_unavailable"},
		{Availability: "unsupported", Error: "key_unavailable"},
		{Availability: "unsupported", Reference: "unexpected"},
		{Availability: "unsupported", PublicKey: "unexpected"},
		{Availability: "unsupported", Signature: "unexpected"},
	} {
		if _, err := parseEnclaveAvailability(response); err == nil {
			t.Fatal("invalid native response allowed fallback")
		}
	}
}

func TestUnsupportedHardwareLoginPreservesExistingGrant(t *testing.T) {
	if enclaveBuildAvailable() {
		t.Skip("exercises builds without the native component")
	}
	var origin string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/oauth-protected-resource/mcp":
			_ = json.NewEncoder(w).Encode(document{"resource": origin + "/mcp", "authorization_servers": []string{origin}})
		case "/.well-known/oauth-authorization-server":
			_ = json.NewEncoder(w).Encode(discoveryFixture(origin))
		default:
			t.Error("unsupported hardware login modified a remote grant")
		}
	}))
	defer server.Close()
	origin = server.URL
	c, _ := Configure(origin+"/mcp", "preserve-hardware")
	for _, key := range []DeviceKey{enclaveMetadata(t), testKey(t)} {
		if runtime.GOOS != "darwin" && keyStorage(key) != enclaveKeyStorage {
			continue
		}
		session := &Session{Version: 1, Server: c.Server, ClientID: "existing", DeviceKey: key, Tokens: &Tokens{"existing-access", "existing-refresh", time.Now().Add(time.Hour).UnixMilli()}}
		store := &memoryStore{s: session}
		oauth := &OAuth{Config: c, Store: store, HTTP: server.Client()}
		if err := oauth.LoginWithProgress(context.Background(), true, "financial-operations-v1", func(LoginStep) error {
			t.Error("opened consent without a usable signing backend")
			return nil
		}); err == nil {
			t.Fatal("hardware login silently downgraded")
		}
		if store.s != session || store.s.Tokens.RefreshToken != "existing-refresh" || store.s.DeviceKey != key {
			t.Fatal("failed hardware initialization replaced existing credentials")
		}
	}
}

func TestHardwareMetadataDoesNotBlockReadRefreshOrRevocation(t *testing.T) {
	var revoked bool
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/mcp/oauth/token":
			_ = json.NewEncoder(w).Encode(document{"access_token": "new-access", "refresh_token": "new-refresh", "expires_in": 900, "scope": Scope, "token_type": "Bearer"})
		case "/mcp/oauth/revoke":
			revoked = true
		default:
			t.Error("unexpected signing or enrollment request")
		}
	}))
	defer server.Close()
	c, _ := Configure(server.URL+"/mcp", "hardware-offline")
	store := &memoryStore{s: &Session{Version: 1, Server: c.Server, ClientID: "client", DeviceKey: enclaveMetadata(t), Tokens: &Tokens{"expired", "old-refresh", 1}}}
	o := &OAuth{Config: c, Store: store, HTTP: server.Client()}
	if token, err := o.AccessToken(context.Background()); err != nil || token != "new-access" {
		t.Fatal("OAuth refresh tried to unlock or export the hardware key")
	}
	status, err := o.Status(context.Background())
	if err != nil || status["keyStorage"] != enclaveKeyStorage || status["keyExportable"] != false {
		t.Fatal("hardware status missing")
	}
	raw, _ := json.Marshal(status)
	if strings.Contains(string(raw), store.s.DeviceKey.EnclaveReference) {
		t.Fatal("status exposed a hardware reference")
	}
	if err := o.Logout(context.Background()); err != nil || !revoked || store.s != nil {
		t.Fatal("hardware availability blocked remote revocation or local deletion")
	}
}

// Opt in only on a physical Mac. This creates a new, unregistered test key and
// an isolated OS-vault entry, then removes it. No Reah network call is made.
func TestSecureEnclaveHardware(t *testing.T) {
	if os.Getenv("REAH_MCP_TEST_SECURE_ENCLAVE") != "1" {
		t.Skip("set REAH_MCP_TEST_SECURE_ENCLAVE=1 on a physical Mac")
	}
	supported, err := enclaveAvailable()
	if err != nil || !supported {
		t.Fatal("physical test requires available Secure Enclave", err)
	}
	key, err := generateLoginKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	if key.Storage != enclaveKeyStorage || key.PrivateKey != "" || key.EnclaveReference == "" {
		t.Fatal("hardware generation did not return an opaque reference")
	}
	c, _ := Configure(StagingServer, "offline-enclave-test-"+randomText()[:12])
	vault := &Vault{Config: c, Directory: t.TempDir()}
	defer func() {
		if err := vault.Remove(); err != nil {
			t.Error("could not remove the isolated hardware test vault entry")
		}
	}()
	s := &Session{Version: 1, Server: c.Server, ClientID: "offline-test", DeviceKey: key}
	if err := vault.Write(s); err != nil {
		t.Fatal(err)
	}
	restored, err := vault.Read()
	if err != nil || restored == nil || restored.DeviceKey.PublicKey != key.PublicKey {
		t.Fatal("cannot restore the hardware key from the OS vault")
	}
	payload := "Reah MCP offline hardware test; no authorization or transaction"
	point, _ := hex.DecodeString(key.PublicKey)
	x, y := elliptic.UnmarshalCompressed(elliptic.P256(), point)
	public := &ecdsa.PublicKey{Curve: elliptic.P256(), X: x, Y: y}
	hash := sha256.Sum256([]byte(payload))
	for _, der := range []bool{true, false} {
		// Each call launches a fresh embedded process, restoring only the wrapped
		// reference. No in-process private key or native key object is retained.
		signature, err := signText(restored.DeviceKey, payload, der)
		if err != nil {
			t.Fatal(err)
		}
		value, _ := hex.DecodeString(signature)
		if der && !ecdsa.VerifyASN1(public, hash[:], value) || !der && (len(value) != 64 || !ecdsa.Verify(public, hash[:], newInt(value[:32]), newInt(value[32:]))) {
			t.Fatal("native signature is incompatible with existing signing protocols")
		}
	}
	t.Run("financial_enrollment", func(t *testing.T) {
		var proof document
		proveCalls := 0
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/mcp/credentials/prepare":
			case "/mcp/credentials/prove":
				var input document
				if json.NewDecoder(r.Body).Decode(&input) != nil || !exactFields(input, "signature") {
					t.Error("invalid proof submission")
				}
				signature, _ := hex.DecodeString(stringAt(input, "signature"))
				hash := sha256.Sum256([]byte(stringAt(proof, "proof_payload")))
				if len(signature) != 64 || !ecdsa.Verify(public, hash[:], newInt(signature[:32]), newInt(signature[32:])) {
					t.Error("hardware possession proof does not match the financial enrollment contract")
				}
				proveCalls++
				proof["state"] = "awaiting_provider"
			default:
				t.Error("unexpected enrollment endpoint")
			}
			_ = json.NewEncoder(w).Encode(proof)
		}))
		defer server.Close()
		config, _ := Configure(server.URL+"/mcp", "hardware-enrollment")
		session := &Session{Version: 1, Server: config.Server, ClientID: "offline-test", DeviceKey: restored.DeviceKey, CredentialEnrollmentRequested: true, CredentialPolicyProfile: "financial-operations-v1", Tokens: &Tokens{"test-access", "test-refresh", time.Now().Add(time.Hour).UnixMilli()}}
		proof = financialEnrollmentFixture(t, config, session, "financial-operations-v1")
		store := &memoryStore{s: session}
		oauth := &OAuth{Config: config, Store: store, HTTP: server.Client()}
		for range 2 {
			if err := oauth.EnrollKey(context.Background()); err != nil {
				t.Fatal(err)
			}
		}
		if proveCalls != 1 || store.s.Enrollment.State != "awaiting_provider" {
			t.Fatal("hardware proof replayed or skipped provider registration")
		}
	})
	corrupt := restored.DeviceKey
	ref, _ := base64.StdEncoding.DecodeString(corrupt.EnclaveReference)
	ref[len(ref)-1] ^= 1
	corrupt.EnclaveReference = base64.StdEncoding.EncodeToString(ref)
	for _, invalid := range []DeviceKey{corrupt, {Storage: enclaveKeyStorage, PublicKey: testKey(t).PublicKey, EnclaveReference: key.EnclaveReference}} {
		if _, err := signText(invalid, payload, true); err == nil || strings.Contains(err.Error(), invalid.EnclaveReference) {
			t.Fatal("signed with a tampered reference or mismatched public key, or leaked the reference")
		}
	}
	// The normal serialized record still fits the cross-platform vault limit.
	s.Tokens = &Tokens{strings.Repeat("a", 128), strings.Repeat("b", 128), time.Now().Add(time.Hour).UnixMilli()}
	if err := vault.Write(s); err != nil {
		t.Fatal(err)
	}
}
