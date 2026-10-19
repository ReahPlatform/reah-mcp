package server

import (
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"net/http"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// HTTPHandler serves one configured principal and entity. It is not an OAuth
// authorization server and must not be shared between unrelated tenants.
func HTTPHandler(s *mcp.Server, token string) (http.Handler, error) {
	if len(token) < 32 || strings.ContainsAny(token, " \r\n\t") {
		return nil, errors.New("REAH_MCP_HTTP_TOKEN must contain at least 32 non-whitespace characters")
	}
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return s }, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
	expected := sha256.Sum256([]byte("Bearer " + token))
	protected := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		actual := sha256.Sum256([]byte(r.Header.Get("Authorization")))
		if subtle.ConstantTimeCompare(actual[:], expected[:]) != 1 {
			w.Header().Set("WWW-Authenticate", `Bearer realm="reah-mcp"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if r.URL.Path != "/mcp" {
			http.NotFound(w, r)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 256<<10)
		handler.ServeHTTP(w, r)
	})
	return http.NewCrossOriginProtection().Handler(protected), nil
}
