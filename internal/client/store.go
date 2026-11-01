package client

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/gofrs/flock"
	"github.com/zalando/go-keyring"
)

type Tokens struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
	ExpiresAt    int64  `json:"expiresAt"`
}
type Receipt struct {
	ConnectionID        string `json:"connectionId"`
	State               string `json:"state"`
	CredentialExpiresAt string `json:"credentialExpiresAt"`
	ProofHash           string `json:"proofHash"`
}
type Session struct {
	Version                       int       `json:"version"`
	Server                        string    `json:"server"`
	ClientID                      string    `json:"clientId"`
	DeviceKey                     DeviceKey `json:"deviceKey"`
	Tokens                        *Tokens   `json:"tokens,omitempty"`
	CredentialEnrollmentRequested bool      `json:"credentialEnrollmentRequested,omitempty"`
	Enrollment                    *Receipt  `json:"enrollment,omitempty"`
	PendingRevocation             string    `json:"pendingRevocation,omitempty"`
}
type Store interface {
	Read() (*Session, error)
	Write(*Session) error
	Remove() error
	Exclusive(context.Context, func() error) error
}
type Vault struct {
	Config    Config
	Directory string
}

// A separate service avoids racing an older Node client's different lock protocol.
const vaultService = "Reah MCP Go"

func (v *Vault) Read() (*Session, error) {
	raw, e := keyring.Get(vaultService, v.Config.ID)
	if errors.Is(e, keyring.ErrNotFound) {
		return nil, nil
	}
	if e != nil {
		return nil, errors.New("cannot read the OS credential store; unlock it or reconnect; no file fallback is used")
	}
	var s Session
	if json.Unmarshal([]byte(raw), &s) != nil || s.Version != 1 || s.Server != v.Config.Server || s.ClientID == "" {
		return nil, errors.New("invalid saved Reah credentials; reconnect")
	}
	if _, e = privateKey(s.DeviceKey); e != nil {
		return nil, e
	}
	if s.Tokens != nil && (s.Tokens.AccessToken == "" || s.Tokens.RefreshToken == "" || s.Tokens.ExpiresAt <= 0) {
		return nil, errors.New("invalid saved Reah token; reconnect")
	}
	return &s, nil
}
func (v *Vault) Write(s *Session) error {
	raw, e := json.Marshal(s)
	if e != nil {
		return e
	}
	// Stay within Windows Credential Manager and the macOS helper's input limit.
	// Reject before invoking the provider; never spill large credentials to disk.
	if len(raw) > 2500 {
		return errors.New("Reah credentials exceed the OS vault limit; disconnect and reconnect")
	}
	if e = keyring.Set(vaultService, v.Config.ID, string(raw)); e != nil {
		return errors.New("cannot save Reah credentials in the OS credential store; no file fallback is used")
	}
	return nil
}
func (v *Vault) Remove() error {
	e := keyring.Delete(vaultService, v.Config.ID)
	if e != nil && !errors.Is(e, keyring.ErrNotFound) {
		return errors.New("cannot remove Reah credentials from the OS credential store")
	}
	return nil
}
func (v *Vault) Exclusive(ctx context.Context, fn func() error) error {
	dir := v.Directory
	if dir == "" {
		home, e := os.UserHomeDir()
		if e != nil {
			return e
		}
		dir = filepath.Join(home, ".reah-mcp-go")
	}
	if e := os.MkdirAll(dir, 0700); e != nil {
		return e
	}
	info, e := os.Lstat(dir)
	if e != nil || !info.IsDir() || !privateDirectory(info) {
		return errors.New("Reah coordination directory must be private and owned by this user")
	}
	path := filepath.Join(dir, v.Config.ID+".lock")
	if f, e := os.Lstat(path); e == nil && !privateFile(f) {
		return errors.New("unsafe Reah coordination file")
	}
	lock := flock.New(path, flock.SetPermissions(0600))
	defer lock.Close()
	wait, cancel := context.WithTimeout(ctx, 6*time.Minute)
	defer cancel()
	ok, e := lock.TryLockContext(wait, 100*time.Millisecond)
	if e != nil || !ok {
		return errors.New("another Reah login or refresh is running")
	}
	defer lock.Unlock()
	return fn()
}
