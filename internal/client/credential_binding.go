package client

import (
	"context"
	"crypto/sha256"
	"encoding/json"
)

// Login always replaces the saved device key, including read-only consent.
// Refresh preserves it. Each pending invocation keeps that existing lifecycle
// binding; it never grants authority in place of server authorization.
type invocationCredentials struct{ session [32]byte }
type invocationCredentialsKey struct{}

func sessionIdentity(config Config, s *Session) ([32]byte, error) {
	if s == nil || s.Tokens == nil || s.PendingRevocation != "" {
		return [32]byte{}, errConnection
	}
	raw, err := json.Marshal(struct {
		Config                               Config
		Version                              int
		Server, ClientID, PublicKey, Profile string
		EnrollmentRequested                  bool
		Enrollment                           *Receipt
	}{config, s.Version, s.Server, s.ClientID, s.DeviceKey.PublicKey, s.CredentialPolicyProfile, s.CredentialEnrollmentRequested, s.Enrollment})
	return sha256.Sum256(raw), err
}
func (r *Remote) bindInvocation(ctx context.Context) (context.Context, error) {
	var identity [32]byte
	err := r.OAuth.Store.Exclusive(ctx, func() error {
		s, err := r.OAuth.Store.Read()
		if err != nil {
			return err
		}
		identity, err = sessionIdentity(r.OAuth.Config, s)
		return err
	})
	if err != nil {
		r.invalidateCatalog()
		return nil, err
	}
	return context.WithValue(ctx, invocationCredentialsKey{}, invocationCredentials{identity}), nil
}
func checkInvocationCredentials(ctx context.Context, config Config, s *Session) error {
	binding, ok := ctx.Value(invocationCredentialsKey{}).(invocationCredentials)
	if !ok {
		return nil
	}
	identity, err := sessionIdentity(config, s)
	if err != nil {
		return err
	}
	if identity != binding.session {
		return errConnection
	}
	return nil
}
