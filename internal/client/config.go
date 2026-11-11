package client

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"time"
)

const ProductionServer = "https://platform.reah.com/mcp"
const StagingServer = "https://platform.stg.reah.com/mcp"
const DefaultServer = ProductionServer
const Scope = "reah.read"

type Config struct{ Server, Origin, Profile, ID string }

func Configure(server, profile string) (Config, error) {
	u, err := url.Parse(server)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.Path != "/mcp" || u.RawPath != "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.String() != server || !regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`).MatchString(profile) {
		return Config{}, errors.New("use an HTTPS /mcp URL without credentials or query parameters and a simple profile name")
	}
	h := sha256.Sum256([]byte(server + "\n" + profile))
	return Config{server, u.Scheme + "://" + u.Host, profile, hex.EncodeToString(h[:])}, nil
}

func httpClient() *http.Client {
	return &http.Client{Timeout: 60 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("redirects are not allowed") }}
}

// Public errors never include URLs containing OAuth codes, bodies or credentials.
var errConnection = errors.New("Reah could not complete the request; check connectivity and run reah-mcp status --check with the same server and profile before replacing the connection")
