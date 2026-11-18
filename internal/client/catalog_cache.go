package client

import (
	"context"
	"encoding/json"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const catalogLifetime = time.Minute
const maxCachedCatalogBytes = 4 * 1024 * 1024

// Only public tool metadata is retained. The digest binds its credential
// lifecycle without retaining tokens, keys, principals or permission decisions.
type catalogSnapshot struct {
	identity [32]byte
	expires  time.Time
	tools    []byte
}

func (r *Remote) invalidateCatalog() {
	r.catalogMu.Lock()
	defer r.catalogMu.Unlock()
	r.catalogGeneration++
	r.catalogSnapshot = catalogSnapshot{}
}

func (r *Remote) catalogIdentity(ctx context.Context, refreshToken bool) ([32]byte, error) {
	if refreshToken {
		if _, err := r.OAuth.AccessToken(ctx); err != nil {
			return [32]byte{}, err
		}
	}
	var identity [32]byte
	err := r.OAuth.Store.Exclusive(ctx, func() error {
		s, err := r.OAuth.Store.Read()
		if err != nil {
			return err
		}
		if s == nil || s.Tokens == nil || s.Tokens.AccessToken == "" || s.PendingRevocation != "" {
			return errConnection
		}
		if err := checkInvocationCredentials(ctx, r.OAuth.Config, s); err != nil {
			return err
		}
		identity, err = sessionIdentity(r.OAuth.Config, s)
		return err
	})
	return identity, err
}

// An explicit List refreshes discovery. Ordinary calls reuse an immutable
// snapshot for at most one minute; every invocation still authenticates at the
// server and signed calls obtain fresh signing context and request proofs.
func (r *Remote) catalog(ctx context.Context, refresh bool) ([]*mcp.Tool, bool, error) {
	identity, err := r.catalogIdentity(ctx, true)
	if err != nil {
		r.invalidateCatalog()
		return nil, false, err
	}
	r.catalogMu.Lock()
	snapshot := r.catalogSnapshot
	if !refresh && snapshot.identity == identity && time.Now().Before(snapshot.expires) {
		r.catalogMu.Unlock()
		var tools []*mcp.Tool
		if json.Unmarshal(snapshot.tools, &tools) != nil {
			return nil, false, errConnection
		}
		return tools, true, nil
	}
	r.catalogGeneration++
	generation := r.catalogGeneration
	r.catalogSnapshot = catalogSnapshot{}
	r.catalogMu.Unlock()

	// Network discovery never holds the metadata mutex. Concurrent refreshes
	// may complete independently; only the latest generation can publish.
	tools, err := r.fetchTools(ctx)
	if err != nil {
		return nil, false, err
	}
	current, err := r.catalogIdentity(ctx, false)
	if err != nil {
		return nil, false, errConnection
	}
	if current != identity {
		return nil, false, errConnection
	}
	raw, err := json.Marshal(tools)
	if err != nil {
		return nil, false, errConnection
	}
	if len(raw) <= maxCachedCatalogBytes {
		r.catalogMu.Lock()
		if r.catalogGeneration == generation {
			r.catalogSnapshot = catalogSnapshot{identity: identity, expires: time.Now().Add(catalogLifetime), tools: raw}
		}
		r.catalogMu.Unlock()
	}
	return tools, false, nil
}
