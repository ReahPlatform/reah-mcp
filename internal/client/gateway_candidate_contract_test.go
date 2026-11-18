package client

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"testing"
	"time"
)

// Gateway candidate CI generates this corpus using its actual canonical
// encoder and production projection. No client business format is decoded.
func TestGatewayCandidateCorpus(t *testing.T) {
	path := os.Getenv("REAH_MCP_GATEWAY_CORPUS")
	if path == "" {
		path = "testdata/gateway-prepared-corpus.json"
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) > 2*1024*1024 {
		t.Fatal("synthetic corpus exceeds test limit")
	}
	if os.Getenv("REAH_MCP_GATEWAY_CORPUS") == "" {
		provenance, err := os.ReadFile("testdata/gateway-prepared-corpus.provenance.json")
		if err != nil {
			t.Fatal(err)
		}
		var source struct {
			GatewayRevision   string `json:"gatewayRevision"`
			LedgerAPIRevision string `json:"ledgerApiRevision"`
			SHA256            string `json:"sha256"`
		}
		if err := json.Unmarshal(provenance, &source); err != nil {
			t.Fatal(err)
		}
		for _, revision := range []string{source.GatewayRevision, source.LedgerAPIRevision} {
			decoded, err := hex.DecodeString(revision)
			if err != nil || len(decoded) != 20 {
				t.Fatal("producer provenance requires exact Git revisions")
			}
		}
		digest := sha256.Sum256(raw)
		if source.SHA256 != hex.EncodeToString(digest[:]) {
			t.Fatal("committed corpus differs from its recorded producer artifact")
		}
	}
	var corpus []gatewayPreparationFixture
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&corpus); err != nil {
		t.Fatal(err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		t.Fatal("trailing candidate corpus data")
	}
	if len(corpus) < 3 || len(corpus) > 32 {
		t.Fatal("missing candidate protocol variants")
	}
	var plain, contextual, annotated bool
	for i, original := range corpus {
		t.Run(string(rune('A'+i)), func(t *testing.T) {
			clone := func() gatewayPreparationFixture {
				b, e := json.Marshal(original)
				if e != nil {
					t.Fatal(e)
				}
				var f gatewayPreparationFixture
				if e = json.Unmarshal(b, &f); e != nil {
					t.Fatal(e)
				}
				return f
			}
			key := testKey(t)
			for name, mutate := range map[string]func(*gatewayPreparationFixture){
				"missing producer echo":    func(f *gatewayPreparationFixture) { delete(f.Prepared, "publicKey") },
				"malformed producer echo":  func(f *gatewayPreparationFixture) { f.Prepared["publicKey"] = "invalid" },
				"mismatched producer echo": func(f *gatewayPreparationFixture) { f.Prepared["publicKey"] = key.PublicKey },
				"malformed producer binding": func(f *gatewayPreparationFixture) {
					f.Context.Binding.PublicKey = "invalid"
					f.Prepared["publicKey"] = "invalid"
				},
			} {
				t.Run(name, func(t *testing.T) {
					negative := clone()
					mutate(&negative)
					if rebindGatewayPreparationKey(&negative, key) == nil {
						t.Fatal("invalid producer key binding repaired by test")
					}
				})
			}
			f := clone()
			if err := rebindGatewayPreparationKey(&f, key); err != nil {
				t.Fatal(err)
			}
			now, e := time.Parse(time.RFC3339Nano, f.Now)
			if e != nil {
				t.Fatal(e)
			}
			_, hasContext := f.Arguments["preparationContext"]
			contextual = contextual || hasContext
			plain = plain || !hasContext
			_, hasAnnotation := f.Prepared["futureAnnotation"]
			annotated = annotated || hasAnnotation
			signature, input, e := preparedApproval(key, f.Context, f.Tool, f.Arguments, f.Prepared, now)
			if e != nil {
				t.Fatal("actual candidate rejected", e)
			}
			want := cloneDocument(f.Arguments)
			delete(want, "preparationContext")
			if !sameJSON(input, want) || stringAt(signature, "payloadBytes") != stringAt(f.Prepared, "payloadBytes") {
				t.Fatal("candidate terms or opaque bytes changed")
			}
			for _, name := range []string{"financialState", "futureAnnotation", "preparationContext"} {
				if _, exists := input[name]; exists {
					t.Fatal("preparation metadata entered submission input", name)
				}
			}
			verifyText(t, key, stringAt(signature, "payloadBytes"), stringAt(signature, "signature"), false)
			for name, mutate := range map[string]func(document){
				"wrong public key":   func(d document) { d["publicKey"] = testKey(t).PublicKey },
				"missing public key": func(d document) { delete(d, "publicKey") },
				"entity":             func(d document) { d["entityId"] = testOtherID },
				"nonce":              func(d document) { d["requestId"] = "00000000-0000-4000-8000-000000000001" },
				"input":              func(d document) { d["input"].(map[string]any)["changed"] = true },
				"arguments":          func(d document) { d["arguments"].(map[string]any)["changed"] = true },
				"expired":            func(d document) { d["expiresAt"] = now.Add(-time.Second).Format(time.RFC3339Nano) },
				"payload encoding":   func(d document) { d["payloadBytes"] = stringAt(d, "payloadBytes") + "\n" },
			} {
				t.Run(name, func(t *testing.T) {
					negative := clone()
					if err := rebindGatewayPreparationKey(&negative, key); err != nil {
						t.Fatal(err)
					}
					mutate(negative.Prepared)
					if _, _, err := preparedApproval(key, negative.Context, negative.Tool, negative.Arguments, negative.Prepared, now); err == nil {
						t.Fatal("altered candidate binding accepted")
					}
				})
			}
		})
	}
	if !plain || !contextual || !annotated {
		t.Fatal("producer omitted plain/context-bearing/extensible protocol variants")
	}
}
