package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ReahPlatform/reah-mcp/internal/client"
	"github.com/ReahPlatform/reah-mcp/internal/update"
)

func TestUpdaterSourceBuildDoesNotNeedCredentials(t *testing.T) {
	t.Setenv("REAH_MCP_VERSION", "")
	root := commandWithOAuth(func(client.Config) *client.OAuth { t.Fatal("updater accessed OAuth"); return nil })
	launch := addUpdates(root)
	if err := launch(nil); err != nil {
		t.Fatalf("source build startup: %v", err)
	}
	var output bytes.Buffer
	root.SetOut(&output)
	root.SetErr(&output)
	root.SetArgs([]string{"update", "--check"})
	if err := root.Execute(); err == nil || !strings.Contains(err.Error(), "no update channel") {
		t.Fatalf("expected explicit source-build status, got %v", err)
	}
}

func TestLauncherRejectsDamagedInstalledRelease(t *testing.T) {
	t.Setenv("REAH_MCP_VERSION", "")
	for _, damage := range []string{"corrupt", "truncated", "missing"} {
		t.Run(damage, func(t *testing.T) {
			config, path := cachedLauncherRelease(t)
			manager, err := update.New(config)
			if err != nil {
				t.Fatal(err)
			}
			if selected, err := manager.Select(""); err != nil || selected != path {
				t.Fatalf("valid signed cache: %q %v", selected, err)
			}
			if damage == "missing" {
				err = os.Remove(path)
			} else {
				binary := []byte("cached executable")
				if damage == "truncated" {
					binary = binary[:4]
				} else {
					binary[0] ^= 1
				}
				err = os.WriteFile(path, binary, 0700)
			}
			if err != nil {
				t.Fatal(err)
			}
			root := commandWithOAuth(func(client.Config) *client.OAuth { t.Fatal("launcher accessed OAuth"); return nil })
			root.Version = config.Version
			launch := addUpdatesWithConfig(root, config)
			if err := launch([]string{"serve"}); err == nil || !strings.Contains(err.Error(), "cannot select a verified client release") {
				t.Fatalf("damaged cache must stop startup: %v", err)
			}
			if err := launch([]string{"serve", "--pin-version", config.Version}); err != nil {
				t.Fatalf("explicit launcher rollback must remain available: %v", err)
			}
		})
	}
}

func cachedLauncherRelease(t *testing.T) (update.Config, string) {
	t.Helper()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(t.TempDir(), "updates")
	directory := filepath.Join(root, "v1.1.0")
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	name := "reah-mcp"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	path := filepath.Join(directory, name)
	binary := []byte("cached executable")
	digest := sha256.Sum256(binary)
	manifest := update.Manifest{
		Schema: 1, Version: "v1.1.0", PublishedAt: time.Now().Add(-time.Minute), ExpiresAt: time.Now().Add(time.Hour),
		Targets: map[string]update.Asset{runtime.GOOS + "_" + runtime.GOARCH: {
			URL: "https://example.com/client", SHA256: hex.EncodeToString(digest[:]), Size: int64(len(binary)),
		}},
	}
	payload, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := json.Marshal(update.Envelope{
		Payload: base64.StdEncoding.EncodeToString(payload),
		Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(private,
			append([]byte(update.SignatureContext), payload...))),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "update.json"), metadata, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, binary, 0700); err != nil {
		t.Fatal(err)
	}
	return update.Config{Version: "v1.0.0", ManifestURL: "https://example.com/update.json",
		PublicKey: base64.StdEncoding.EncodeToString(public), Directory: root}, path
}

func TestUpdaterHelpDoesNotNeedCredentials(t *testing.T) {
	root := commandWithOAuth(func(client.Config) *client.OAuth { t.Fatal("help accessed OAuth"); return nil })
	addUpdates(root)
	var output bytes.Buffer
	root.SetOut(&output)
	root.SetErr(&output)
	root.SetArgs([]string{"update", "--help"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, flag := range []string{"--check", "--no-auto-update", "--pin-version"} {
		if !strings.Contains(output.String(), flag) {
			t.Fatalf("missing %s", flag)
		}
	}
}

func TestBootstrapPinPreservesNewArguments(t *testing.T) {
	for _, test := range []struct {
		args      []string
		env, want string
	}{
		{[]string{"new-command", "--future-option", "value"}, "v1.0.0", "v1.0.0"},
		{[]string{"new-command", "--pin-version", "v1.2.0", "--future-option"}, "v1.0.0", "v1.2.0"},
		{[]string{"--pin-version=v1.1.0", "--version"}, "", "v1.1.0"},
		{[]string{"--", "--pin-version=v1.1.0"}, "v1.0.0", "v1.0.0"},
		{[]string{"--pin-version="}, "v1.0.0", ""},
	} {
		got, err := versionPin(test.args, test.env)
		if err != nil || got != test.want {
			t.Fatalf("%v: %q %v", test.args, got, err)
		}
	}
	if _, err := versionPin([]string{"--pin-version"}, ""); err == nil {
		t.Fatal("accepted missing version")
	}
}
