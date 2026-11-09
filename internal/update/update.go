package update

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/gofrs/flock"
)

// Config is embedded at release time. Update traffic never uses Reah OAuth.
type Config struct {
	Version     string
	ManifestURL string
	PublicKey   string
	// Directory and HTTPClient are injectable for isolated callers and tests.
	Directory  string
	HTTPClient *http.Client
}

type Manager struct {
	config Config
	key    ed25519.PublicKey
	root   string
	http   *http.Client
}

type Result struct {
	CurrentVersion   string `json:"currentVersion"`
	AvailableVersion string `json:"availableVersion"`
	Staged           bool   `json:"staged"`
	NextStart        bool   `json:"activateOnNextStart"`
}

func New(config Config) (*Manager, error) {
	if config.ManifestURL == "" && config.PublicKey == "" {
		return nil, nil
	}
	if _, err := parseVersion(config.Version); err != nil {
		return nil, errors.New("automatic updates require a stable release build")
	}
	key, err := base64.StdEncoding.DecodeString(config.PublicKey)
	if err != nil || len(key) != ed25519.PublicKeySize || !httpsURL(config.ManifestURL) {
		return nil, errors.New("invalid embedded update configuration")
	}
	root := config.Directory
	if root == "" {
		base, err := os.UserConfigDir()
		if err != nil {
			return nil, err
		}
		channel := sha256.Sum256([]byte(config.ManifestURL + "\n" + config.PublicKey + "\n" + target()))
		root = filepath.Join(base, "Reah", "mcp-updates", hex.EncodeToString(channel[:16]))
	}
	client := &http.Client{Timeout: 2 * time.Minute}
	if config.HTTPClient != nil {
		*client = *config.HTTPClient
	}
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 || !httpsURL(req.URL.String()) {
			return errors.New("unsafe update redirect")
		}
		return nil
	}
	// A copied transport may be provided by tests; no client OAuth transport is used.
	return &Manager{config: config, key: ed25519.PublicKey(key), root: root, http: client}, nil
}

func (m *Manager) directory() error {
	if err := os.MkdirAll(m.root, 0700); err != nil {
		return err
	}
	return privatePath(m.root, true)
}

func executableName() string {
	if runtime.GOOS == "windows" {
		return "reah-mcp.exe"
	}
	return "reah-mcp"
}

func target() string { return runtime.GOOS + "_" + runtime.GOARCH }

func readPrivate(path string, limit int64) ([]byte, error) {
	if err := privatePath(path, false); err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err == nil && int64(len(data)) > limit {
		err = errors.New("update file exceeds size limit")
	}
	return data, err
}

func (m *Manager) cached(version string) (Manifest, error) {
	var manifest Manifest
	dir := filepath.Join(m.root, version)
	if err := privatePath(dir, true); err != nil {
		return manifest, err
	}
	data, err := readPrivate(filepath.Join(dir, "update.json"), maxManifestBytes)
	if err != nil {
		return manifest, err
	}
	manifest, err = Verify(data, m.key, time.Now(), true)
	if err == nil && manifest.Version != version {
		err = errors.New("cached update version does not match its directory")
	}
	return manifest, err
}

// latest uses signed metadata as a persistent rollback floor, even if an
// executable is damaged. An invalid cache must never cause a silent downgrade.
func (m *Manager) latest() (string, error) {
	latest := m.config.Version
	entries, err := os.ReadDir(m.root)
	if errors.Is(err, os.ErrNotExist) {
		return latest, nil
	}
	if err != nil {
		return "", err
	}
	for _, entry := range entries {
		if _, err := parseVersion(entry.Name()); err != nil {
			continue
		}
		if _, err := m.cached(entry.Name()); err != nil {
			return "", fmt.Errorf("invalid cached update %s: %w", entry.Name(), err)
		}
		if compare(entry.Name(), latest) > 0 {
			latest = entry.Name()
		}
	}
	return latest, nil
}

func verifyBinary(path string, asset Asset) error {
	if err := privatePath(path, false); err != nil {
		return err
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	hash := sha256.New()
	size, err := io.Copy(hash, io.LimitReader(f, asset.Size+1))
	if err != nil {
		return err
	}
	if size != asset.Size || hex.EncodeToString(hash.Sum(nil)) != asset.SHA256 {
		return errors.New("cached executable checksum or size mismatch")
	}
	return nil
}

// Select verifies local bytes only. It never downloads or restarts an active
// session. An explicit pin can select a previously installed signed release.
func (m *Manager) Select(pin string) (string, error) {
	if pin != "" {
		if _, err := parseVersion(pin); err != nil {
			return "", err
		}
		if pin == m.config.Version {
			return "", nil
		}
	}
	if err := m.directory(); err != nil {
		return "", err
	}
	version := pin
	if version == "" {
		var err error
		version, err = m.latest()
		if err != nil || version == m.config.Version {
			return "", err
		}
	}
	manifest, err := m.cached(version)
	if err != nil {
		return "", fmt.Errorf("requested update is not installed: %w", err)
	}
	asset, ok := manifest.Targets[target()]
	if !ok {
		return "", errors.New("cached update does not support this platform")
	}
	path := filepath.Join(m.root, version, executableName())
	if err := verifyBinary(path, asset); err != nil {
		return "", err
	}
	return path, nil
}

func (m *Manager) get(ctx context.Context, address string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, errors.New("invalid update URL")
	}
	req.Header.Set("User-Agent", "reah-mcp-updater/"+m.config.Version)
	resp, err := m.http.Do(req)
	if err != nil {
		return nil, errors.New("update download failed; check network access to GitHub Releases")
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("update server returned HTTP %d", resp.StatusCode)
	}
	return resp, nil
}

// Check verifies the stable channel; install stages an immutable release for
// the next process. The lock coordinates all profiles and concurrent clients.
func (m *Manager) Check(ctx context.Context, install bool) (Result, error) {
	return m.check(ctx, install, false)
}

func (m *Manager) check(ctx context.Context, install, automatic bool) (Result, error) {
	result := Result{CurrentVersion: m.config.Version}
	if err := m.directory(); err != nil {
		return result, err
	}
	lockPath := filepath.Join(m.root, "update.lock")
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return result, err
	}
	f.Close()
	if err := privatePath(lockPath, false); err != nil {
		return result, err
	}
	lock := flock.New(lockPath)
	defer lock.Close()
	locked, err := lock.TryLock()
	if err != nil {
		return result, err
	}
	if !locked {
		return result, errors.New("another client is checking for updates; retry later")
	}
	defer lock.Unlock()
	if automatic {
		data, _ := readPrivate(filepath.Join(m.root, "last-check"), 100)
		last, _ := time.Parse(time.RFC3339, string(data))
		if elapsed := time.Since(last); elapsed >= 0 && elapsed < 24*time.Hour {
			return result, nil
		}
		// Persist attempts as well as successes so offline startups do not hammer
		// the release endpoint. An explicit update command always bypasses this.
		if err := os.WriteFile(filepath.Join(m.root, "last-check"), []byte(time.Now().UTC().Format(time.RFC3339)), 0600); err != nil {
			return result, err
		}
	}
	floor, err := m.latest()
	if err != nil {
		return result, err
	}
	resp, err := m.get(ctx, m.config.ManifestURL)
	if err != nil {
		return result, err
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxManifestBytes+1))
	resp.Body.Close()
	if err != nil {
		return result, errors.New("could not read update metadata")
	}
	manifest, err := Verify(data, m.key, time.Now(), false)
	if err != nil {
		return result, err
	}
	if compare(manifest.Version, floor) < 0 {
		return result, errors.New("update channel attempted to roll back an installed release")
	}
	asset, ok := manifest.Targets[target()]
	if !ok {
		return result, errors.New("release does not support this platform")
	}
	result.AvailableVersion = manifest.Version
	result.NextStart = compare(floor, m.config.Version) > 0
	if _, err := os.Lstat(filepath.Join(m.root, manifest.Version)); err == nil {
		cached, err := m.cached(manifest.Version)
		if err != nil {
			return result, err
		}
		if cached.Targets[target()] != asset {
			return result, errors.New("published release assets changed without a new version")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return result, err
	}
	if install && compare(manifest.Version, m.config.Version) > 0 {
		if _, err := os.Lstat(filepath.Join(m.root, manifest.Version)); err == nil {
			if _, err := m.Select(manifest.Version); err != nil {
				return result, err
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return result, err
		} else if err := m.stage(ctx, manifest, asset, data); err != nil {
			return result, err
		}
		result.Staged, result.NextStart = true, true
	}
	// This timestamp is only a throttle; signed metadata remains authoritative.
	if install {
		if err := os.WriteFile(filepath.Join(m.root, "last-check"), []byte(time.Now().UTC().Format(time.RFC3339)), 0600); err != nil {
			return result, err
		}
	}
	return result, nil
}

func (m *Manager) stage(ctx context.Context, manifest Manifest, asset Asset, metadata []byte) error {
	dir, err := os.MkdirTemp(m.root, ".download-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	resp, err := m.get(ctx, asset.URL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	path := filepath.Join(dir, executableName())
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	hash := sha256.New()
	size, copyErr := io.Copy(io.MultiWriter(f, hash), io.LimitReader(resp.Body, asset.Size+1))
	syncErr := f.Sync()
	closeErr := f.Close()
	if err := errors.Join(copyErr, syncErr, closeErr); err != nil {
		return err
	}
	if size != asset.Size || hex.EncodeToString(hash.Sum(nil)) != asset.SHA256 {
		return errors.New("downloaded update checksum or size mismatch")
	}
	if err := os.Chmod(path, 0700); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "update.json"), metadata, 0600); err != nil {
		return err
	}
	return os.Rename(dir, filepath.Join(m.root, manifest.Version))
}

// Auto runs alongside stdio service. Failures leave the current client usable;
// all diagnostics go to stderr, never into the MCP protocol stream.
func (m *Manager) Auto(ctx context.Context, diagnostics io.Writer) {
	for {
		attempt, cancel := context.WithTimeout(ctx, 3*time.Minute)
		result, err := m.check(attempt, true, true)
		cancel()
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			fmt.Fprintf(diagnostics, "reah-mcp update: %v\n", err)
		} else if result.Staged {
			fmt.Fprintf(diagnostics, "reah-mcp update: %s is ready; reconnect to use it.\n", result.AvailableVersion)
		}
		timer := time.NewTimer(24 * time.Hour)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}
