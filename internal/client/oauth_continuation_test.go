package client

import (
	"context"
	"net/http"
	"net/url"
	"testing"
	"time"
)

func TestCallbackContinuationRejectsOtherOriginsAndExtraFields(t *testing.T) {
	for _, destination := range []string{
		"https://attacker.example/mcp/credentials?connectionId=" + testID,
		"http://reah.example/mcp/credentials?connectionId=" + testID,
		"https://reah.example/other?connectionId=" + testID,
		"https://reah.example:444/mcp/credentials?connectionId=" + testID,
		"https://user@reah.example/mcp/credentials?connectionId=" + testID,
		"https://reah.example/mcp/credentials?connectionId=invalid",
		"https://reah.example/mcp/credentials?connectionId=" + testID + "#fragment",
		"https://reah.example/mcp/credentials?connectionId=" + testID + "&token=secret",
		"https://reah.example/mcp/credentials?connectionId=" + testID + "&connectionId=" + testOtherID,
	} {
		t.Run(destination, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
			defer cancel()
			listener, err := newCallback(ctx, "/callback", "state", "https://reah.example")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.close()
			go func() { result := <-listener.result; result.finish(true, destination) }()
			u, _ := url.Parse(listener.redirect)
			u.RawQuery = url.Values{"state": {"state"}, "code": {"code"}}.Encode()
			browser := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
			response, err := browser.Get(u.String())
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			if response.StatusCode != http.StatusBadRequest || response.Header.Get("Location") != "" {
				t.Fatalf("unsafe redirect: %d %s", response.StatusCode, response.Header.Get("Location"))
			}
		})
	}
}
