package client

import (
	"crypto/elliptic"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"
)

type gatewayPreparationFixture struct {
	Now       string         `json:"now"`
	Tool      string         `json:"tool"`
	Context   SigningContext `json:"context"`
	Arguments document       `json:"arguments"`
	Prepared  document       `json:"prepared"`
}

func gatewayPreparation(t *testing.T) (gatewayPreparationFixture, DeviceKey, time.Time) {
	t.Helper()
	raw, err := os.ReadFile("testdata/prepared-approval.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture gatewayPreparationFixture
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	now, err := time.Parse(time.RFC3339Nano, fixture.Now)
	if err != nil {
		t.Fatal(err)
	}
	// The fixture has no private key. Rebind only the unsigned public-key echoes
	// to an ephemeral test key; Gateway's signed payload bytes remain untouched.
	key := testKey(t)
	if err := rebindGatewayPreparationKey(&fixture, key); err != nil {
		t.Fatal(err)
	}
	return fixture, key, now
}

// Check the actual producer echo before replacing public test material. Rebinding
// must never repair a response that the running client would reject.
func rebindGatewayPreparationKey(f *gatewayPreparationFixture, key DeviceKey) error {
	public := f.Context.Binding.PublicKey
	compressed, err := hex.DecodeString(public)
	if err != nil || hex.EncodeToString(compressed) != public {
		return errors.New("producer binding has an invalid public key")
	}
	x, _ := elliptic.UnmarshalCompressed(elliptic.P256(), compressed)
	if x == nil || f.Prepared["publicKey"] != public {
		return errors.New("producer public-key echo does not match its binding")
	}
	f.Context.Binding.PublicKey = key.PublicKey
	f.Prepared["publicKey"] = key.PublicKey
	return nil
}

func TestPreparedApprovalAcceptsGatewayProducedEnvelope(t *testing.T) {
	fixture, key, now := gatewayPreparation(t)
	if _, exists := fixture.Prepared["financialState"]; !exists {
		t.Fatal("Gateway fixture no longer covers descriptive financial state")
	}
	signature, input, err := preparedApproval(key, fixture.Context, fixture.Tool, fixture.Arguments, fixture.Prepared, now)
	if err != nil {
		t.Fatalf("rejected actual Gateway preparation: %v", err)
	}
	if !sameJSON(input, fixture.Arguments) || stringAt(signature, "payloadBytes") != stringAt(fixture.Prepared, "payloadBytes") {
		t.Fatal("changed Gateway input or approval payload")
	}
	if _, exists := input["financialState"]; exists {
		t.Fatal("descriptive financial state entered signing input")
	}
	verifyText(t, key, stringAt(signature, "payloadBytes"), stringAt(signature, "signature"), false)
}

func TestGatewayPreparationStillRejectsAlteredProtectedBindings(t *testing.T) {
	for name, mutate := range map[string]func(*gatewayPreparationFixture){
		"arguments amount": func(f *gatewayPreparationFixture) {
			f.Prepared["arguments"].(document)["amount"] = "1"
		},
		"input amount": func(f *gatewayPreparationFixture) {
			f.Prepared["input"].(document)["amount"] = "1"
		},
		"entity": func(f *gatewayPreparationFixture) { f.Prepared["entityId"] = testOtherID },
		"nonce":  func(f *gatewayPreparationFixture) { f.Prepared["requestId"] = testID },
		"issued time": func(f *gatewayPreparationFixture) {
			f.Prepared["issuedAt"] = "2026-11-17T01:04:03.123Z"
		},
		"payload": func(f *gatewayPreparationFixture) {
			f.Prepared["payloadBytes"] = stringAt(f.Prepared, "payloadBytes") + "\n"
		},
	} {
		t.Run(name, func(t *testing.T) {
			fixture, key, now := gatewayPreparation(t)
			mutate(&fixture)
			if _, _, err := preparedApproval(key, fixture.Context, fixture.Tool, fixture.Arguments, fixture.Prepared, now); err == nil {
				t.Fatal("signed an altered Gateway preparation")
			}
		})
	}
}
