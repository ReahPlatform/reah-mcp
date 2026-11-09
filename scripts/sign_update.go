//go:build ignore

// Sign the public update manifest without putting the signing seed in build flags.
package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/ReahPlatform/reah-mcp/internal/update"
)

func run() error {
	if len(os.Args) != 2 {
		return fmt.Errorf("usage: go run scripts/sign_update.go RELEASE_DIRECTORY")
	}
	seed, err := base64.StdEncoding.DecodeString(os.Getenv("REAH_MCP_UPDATE_SIGNING_SEED"))
	if err != nil || len(seed) != ed25519.SeedSize {
		return fmt.Errorf("REAH_MCP_UPDATE_SIGNING_SEED must be a base64 Ed25519 seed")
	}
	private := ed25519.NewKeyFromSeed(seed)
	public, err := base64.StdEncoding.DecodeString(os.Getenv("REAH_MCP_UPDATE_PUBLIC_KEY"))
	if err != nil || !bytes.Equal(private.Public().(ed25519.PublicKey), public) {
		return fmt.Errorf("release signing key does not match the embedded update public key")
	}
	directory := os.Args[1]
	payload, err := os.ReadFile(filepath.Join(directory, "update-manifest.json"))
	if err != nil {
		return err
	}
	envelope := update.Envelope{
		Payload:   base64.StdEncoding.EncodeToString(payload),
		Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(private, append([]byte(update.SignatureContext), payload...))),
	}
	data, err := json.Marshal(envelope)
	if err != nil {
		return err
	}
	manifest, err := update.Verify(data, public, time.Now(), false)
	if err != nil {
		return err
	}
	if len(manifest.Targets) != 6 {
		return fmt.Errorf("a release must contain all six supported platforms")
	}
	for _, platform := range []string{"darwin", "linux", "windows"} {
		for _, arch := range []string{"amd64", "arm64"} {
			name := "reah-mcp_" + manifest.Version + "_" + platform + "_" + arch
			if platform == "windows" {
				name += ".exe"
			}
			asset := manifest.Targets[platform+"_"+arch]
			if asset.URL != "https://github.com/ReahPlatform/reah-mcp/releases/download/"+manifest.Version+"/"+name {
				return fmt.Errorf("unexpected release URL for %s", name)
			}
			binary, err := os.ReadFile(filepath.Join(directory, name))
			if err != nil {
				return err
			}
			digest := sha256.Sum256(binary)
			if int64(len(binary)) != asset.Size || hex.EncodeToString(digest[:]) != asset.SHA256 {
				return fmt.Errorf("release bytes do not match metadata for %s", name)
			}
		}
	}
	if err := os.WriteFile(filepath.Join(directory, "update.json"), append(data, '\n'), 0644); err != nil {
		return err
	}
	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "sign update:", err)
		os.Exit(1)
	}
}
