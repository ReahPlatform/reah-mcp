package update

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gofrs/flock"
)

func signed(t *testing.T, key ed25519.PrivateKey, manifest Manifest) []byte {
	t.Helper()
	payload, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(Envelope{
		Payload:   base64.StdEncoding.EncodeToString(payload),
		Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(key, append([]byte(SignatureContext), payload...))),
	})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func release(version, address string, binary []byte) Manifest {
	digest := sha256.Sum256(binary)
	return Manifest{Schema: 1, Version: version, PublishedAt: time.Now().Add(-time.Minute), ExpiresAt: time.Now().Add(time.Hour), Targets: map[string]Asset{
		target(): {URL: address, SHA256: hex.EncodeToString(digest[:]), Size: int64(len(binary))},
	}}
}

func TestVerifyRejectsUntrustedMetadata(t *testing.T) {
	public, private, _ := ed25519.GenerateKey(rand.Reader)
	_, wrong, _ := ed25519.GenerateKey(rand.Reader)
	good := release("v1.1.0", "https://example.com/client", []byte("client"))
	if _, err := Verify(signed(t, private, good), public, time.Now(), false); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		mutate func(*Manifest)
	}{
		{"expired", func(m *Manifest) {
			m.PublishedAt = time.Now().Add(-2 * time.Hour)
			m.ExpiresAt = time.Now().Add(-time.Hour)
		}},
		{"future", func(m *Manifest) {
			m.PublishedAt = time.Now().Add(time.Hour)
			m.ExpiresAt = time.Now().Add(2 * time.Hour)
		}},
		{"long expiry", func(m *Manifest) { m.ExpiresAt = m.PublishedAt.Add(91 * 24 * time.Hour) }},
		{"schema", func(m *Manifest) { m.Schema = 2 }},
		{"prerelease", func(m *Manifest) { m.Version = "v1.1.0-rc.1" }},
		{"path version", func(m *Manifest) { m.Version = "../../client" }},
		{"overflow", func(m *Manifest) { m.Version = "v18446744073709551616.0.0" }},
		{"HTTP asset", func(m *Manifest) {
			m.Targets[target()] = Asset{URL: "http://example.com/client", SHA256: strings.Repeat("0", 64), Size: 1}
		}},
		{"oversized", func(m *Manifest) { a := m.Targets[target()]; a.Size = maxBinaryBytes + 1; m.Targets[target()] = a }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manifest := release("v1.1.0", "https://example.com/client", []byte("client"))
			test.mutate(&manifest)
			if _, err := Verify(signed(t, private, manifest), public, time.Now(), false); err == nil {
				t.Fatal("accepted invalid metadata")
			}
		})
	}
	if _, err := Verify(signed(t, wrong, good), public, time.Now(), false); err == nil {
		t.Fatal("accepted a different signing key")
	}
	data := signed(t, private, good)
	var envelope Envelope
	json.Unmarshal(data, &envelope)
	envelope.Payload = base64.StdEncoding.EncodeToString([]byte(`{"schema":1}`))
	tampered, _ := json.Marshal(envelope)
	if _, err := Verify(tampered, public, time.Now(), false); err == nil {
		t.Fatal("accepted tampered payload")
	}
	good.PublishedAt = time.Now().Add(-2 * time.Hour)
	good.ExpiresAt = time.Now().Add(-time.Hour)
	if _, err := Verify(signed(t, private, good), public, time.Now(), true); err != nil {
		t.Fatalf("an already installed authentic release should work offline after expiry: %v", err)
	}
}

type fixture struct {
	mu       sync.Mutex
	manager  *Manager
	manifest Manifest
	private  ed25519.PrivateKey
	binary   []byte
	response []byte
	status   int
	requests int
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	public, private, _ := ed25519.GenerateKey(rand.Reader)
	f := &fixture{private: private, binary: []byte("first executable"), status: http.StatusOK}
	mux := http.NewServeMux()
	server := httptest.NewTLSServer(mux)
	t.Cleanup(server.Close)
	f.manifest = release("v1.1.0", server.URL+"/binary", f.binary)
	mux.HandleFunc("/update.json", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.requests++
		if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
			t.Error("update request sent credentials")
		}
		w.Write(signed(t, private, f.manifest))
	})
	mux.HandleFunc("/binary", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		w.WriteHeader(f.status)
		if f.response != nil {
			w.Write(f.response)
		} else {
			w.Write(f.binary)
		}
	})
	var err error
	f.manager, err = New(Config{Version: "v1.0.0", ManifestURL: server.URL + "/update.json", PublicKey: base64.StdEncoding.EncodeToString(public), Directory: t.TempDir(), HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestStageCheckPinAndCacheIntegrity(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	result, err := f.manager.Check(ctx, false)
	if err != nil || result.Staged || result.AvailableVersion != "v1.1.0" {
		t.Fatalf("check: %+v %v", result, err)
	}
	if path, err := f.manager.Select(""); err != nil || path != "" {
		t.Fatalf("check installed bytes: %s %v", path, err)
	}
	result, err = f.manager.Check(ctx, true)
	if err != nil || !result.Staged || !result.NextStart {
		t.Fatalf("stage: %+v %v", result, err)
	}
	path, err := f.manager.Select("")
	if err != nil || path == "" {
		t.Fatalf("select: %s %v", path, err)
	}
	if current, err := f.manager.Select("v1.0.0"); err != nil || current != "" {
		t.Fatalf("pin launcher: %s %v", current, err)
	}
	if _, err := f.manager.Select("v0.9.0"); err == nil {
		t.Fatal("missing pin silently fell back")
	}
	if err := os.WriteFile(path, []byte("modified"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := f.manager.Select(""); err == nil {
		t.Fatal("ran a modified cached executable")
	}
}

func TestFailedDownloadPreservesInstalledRelease(t *testing.T) {
	for _, mode := range []string{"HTTP failure", "truncated", "too large", "wrong hash"} {
		t.Run(mode, func(t *testing.T) {
			f := newFixture(t)
			if _, err := f.manager.Check(context.Background(), true); err != nil {
				t.Fatal(err)
			}
			f.mu.Lock()
			f.manifest.Version = "v1.2.0"
			switch mode {
			case "HTTP failure":
				f.status = http.StatusServiceUnavailable
			case "truncated":
				f.response = []byte("short")
			case "too large":
				f.response = append(append([]byte{}, f.binary...), 'x')
			case "wrong hash":
				f.response = []byte(strings.Repeat("x", len(f.binary)))
			}
			f.mu.Unlock()
			if _, err := f.manager.Check(context.Background(), true); err == nil {
				t.Fatal("installed a failed download")
			}
			path, err := f.manager.Select("")
			if err != nil || filepath.Base(filepath.Dir(path)) != "v1.1.0" {
				t.Fatalf("previous release lost: %s %v", path, err)
			}
			if _, err := os.Stat(filepath.Join(f.manager.root, "v1.2.0")); !os.IsNotExist(err) {
				t.Fatal("partial release was activated")
			}
		})
	}
}

func TestRollbackAndRepublishedAssetsRejected(t *testing.T) {
	f := newFixture(t)
	if _, err := f.manager.Check(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	f.manifest.Version = "v1.0.0"
	f.mu.Unlock()
	if _, err := f.manager.Check(context.Background(), true); err == nil {
		t.Fatal("accepted rollback")
	}
	f.mu.Lock()
	f.manifest.Version = "v1.1.0"
	asset := f.manifest.Targets[target()]
	asset.SHA256 = strings.Repeat("0", 64)
	f.manifest.Targets[target()] = asset
	f.mu.Unlock()
	if _, err := f.manager.Check(context.Background(), true); err == nil {
		t.Fatal("accepted changed release bytes")
	}
}

func TestAutomaticThrottleAndManualOverride(t *testing.T) {
	f := newFixture(t)
	if _, err := f.manager.check(context.Background(), true, true); err != nil {
		t.Fatal(err)
	}
	if _, err := f.manager.check(context.Background(), true, true); err != nil {
		t.Fatal(err)
	}
	if count := f.requestCount(); count != 1 {
		t.Fatalf("automatic checks were not throttled: %d", count)
	}
	if _, err := f.manager.Check(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if f.requestCount() != 2 {
		t.Fatal("manual check was throttled")
	}
}

func TestVersionComparison(t *testing.T) {
	for _, pair := range [][2]string{{"v1.9.0", "v1.10.0"}, {"v1.99.99", "v2.0.0"}, {"v0.0.0", "v0.0.1"}} {
		if compare(pair[0], pair[1]) >= 0 || compare(pair[1], pair[0]) <= 0 || compare(pair[0], pair[0]) != 0 {
			t.Fatal(pair)
		}
	}
}

func (f *fixture) requestCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.requests
}

func TestConcurrentUpdateDoesNotStartAnotherDownload(t *testing.T) {
	f := newFixture(t)
	if err := f.manager.directory(); err != nil {
		t.Fatal(err)
	}
	lock := flock.New(filepath.Join(f.manager.root, "update.lock"))
	defer lock.Close()
	if ok, err := lock.TryLock(); err != nil || !ok {
		t.Fatalf("lock: %v %v", ok, err)
	}
	defer lock.Unlock()
	if _, err := f.manager.Check(context.Background(), true); err == nil {
		t.Fatal("ignored another updater's lock")
	}
	if f.requestCount() != 0 {
		t.Fatal("downloaded while another updater held the lock")
	}
}

func TestExpiredChannelDoesNotReplaceInstalledRelease(t *testing.T) {
	f := newFixture(t)
	if _, err := f.manager.Check(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	f.manifest.Version = "v1.2.0"
	f.manifest.PublishedAt = time.Now().Add(-2 * time.Hour)
	f.manifest.ExpiresAt = time.Now().Add(-time.Hour)
	f.mu.Unlock()
	if _, err := f.manager.Check(context.Background(), true); err == nil {
		t.Fatal("accepted expired channel")
	}
	if path, err := f.manager.Select(""); err != nil || filepath.Base(filepath.Dir(path)) != "v1.1.0" {
		t.Fatalf("installed version lost: %s %v", path, err)
	}
}
