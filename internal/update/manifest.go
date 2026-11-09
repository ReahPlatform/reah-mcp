// Package update stages signed client releases without replacing a running executable.
package update

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const SignatureContext = "reah-mcp-update-v1\n"
const maxManifestBytes = 128 << 10
const maxBinaryBytes = 128 << 20

var versionPattern = regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

type Envelope struct {
	Payload   string `json:"payload"`
	Signature string `json:"signature"`
}

type Manifest struct {
	Schema      int              `json:"schema"`
	Version     string           `json:"version"`
	PublishedAt time.Time        `json:"publishedAt"`
	ExpiresAt   time.Time        `json:"expiresAt"`
	Targets     map[string]Asset `json:"targets"`
}

type Asset struct {
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

func parseVersion(s string) ([3]uint64, error) {
	var result [3]uint64
	if !versionPattern.MatchString(s) {
		return result, errors.New("expected a stable version such as v0.1.0")
	}
	for i, part := range strings.Split(s[1:], ".") {
		n, err := strconv.ParseUint(part, 10, 64)
		if err != nil {
			return result, errors.New("version component is too large")
		}
		result[i] = n
	}
	return result, nil
}

func compare(a, b string) int {
	av, _ := parseVersion(a)
	bv, _ := parseVersion(b)
	for i := range av {
		if av[i] < bv[i] {
			return -1
		}
		if av[i] > bv[i] {
			return 1
		}
	}
	return 0
}

func httpsURL(s string) bool {
	u, err := url.Parse(s)
	return err == nil && u.Scheme == "https" && u.Hostname() != "" && u.User == nil && u.Fragment == ""
}

func decodeJSON(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if decoder.More() {
		return errors.New("unexpected trailing JSON")
	}
	var extra any
	if err := decoder.Decode(&extra); err == nil {
		return errors.New("unexpected trailing JSON")
	} else if !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}

// Verify authenticates metadata before interpreting any download URL. Cached
// releases may run after expiry; only fresh metadata can authorize a download.
func Verify(data []byte, key ed25519.PublicKey, now time.Time, cached bool) (Manifest, error) {
	var manifest Manifest
	if len(data) > maxManifestBytes || len(key) != ed25519.PublicKeySize {
		return manifest, errors.New("invalid update metadata or verification key")
	}
	var envelope Envelope
	if err := decodeJSON(data, &envelope); err != nil {
		return manifest, errors.New("invalid signed update envelope")
	}
	payload, err := base64.StdEncoding.DecodeString(envelope.Payload)
	if err != nil {
		return manifest, errors.New("invalid update payload")
	}
	signature, err := base64.StdEncoding.DecodeString(envelope.Signature)
	if err != nil || !ed25519.Verify(key, append([]byte(SignatureContext), payload...), signature) {
		return manifest, errors.New("update signature verification failed")
	}
	if err := decodeJSON(payload, &manifest); err != nil {
		return manifest, errors.New("invalid update manifest")
	}
	if _, err := parseVersion(manifest.Version); err != nil || manifest.Schema != 1 {
		return manifest, errors.New("unsupported update schema or version")
	}
	if manifest.PublishedAt.IsZero() || !manifest.ExpiresAt.After(manifest.PublishedAt) || manifest.ExpiresAt.Sub(manifest.PublishedAt) > 90*24*time.Hour {
		return manifest, errors.New("invalid update validity period")
	}
	if !cached && (manifest.PublishedAt.After(now.Add(5*time.Minute)) || !manifest.ExpiresAt.After(now)) {
		return manifest, errors.New("update metadata is expired or not yet valid")
	}
	if len(manifest.Targets) == 0 {
		return manifest, errors.New("update has no release assets")
	}
	for name, asset := range manifest.Targets {
		digest, err := hex.DecodeString(asset.SHA256)
		if !httpsURL(asset.URL) || err != nil || len(digest) != 32 || asset.Size <= 0 || asset.Size > maxBinaryBytes {
			return manifest, fmt.Errorf("invalid update asset for %s", name)
		}
	}
	return manifest, nil
}
