package client

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/btcsuite/btcd/btcutil/base58"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/dynamicpb"
)

func TestProtectedConvertChainIdentities(t *testing.T) {
	const solanaMint = "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v"
	const tronToken = "TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6t"
	for _, test := range []struct{ name, asset, token, chain string }{
		{"Solana native", convertSolanaChain + "/slip44:501", "", convertSolanaChain},
		{"Solana token", convertSolanaChain + "/token:" + strings.ToLower(solanaMint), solanaMint, convertSolanaChain},
		{"Bitcoin mainnet", convertBitcoinChain + "/slip44:0", "", convertBitcoinChain},
		{"TRON native", convertTronChain + "/slip44:195", "", convertTronChain},
		{"TRON token", convertTronChain + "/token:" + strings.ToLower(tronToken), tronToken, convertTronChain},
		{"send identity", "solana:5eykt4UsFv8P8NJdTREpY1vzqKqZKvdp/token:" + solanaMint, solanaMint, ""},
		{"wrong namespace", "eip155:1151111081099710/token:" + strings.ToLower(solanaMint), solanaMint, ""},
		{"wrong Solana profile", "svm:103/slip44:501", "", ""},
		{"wrong coin type", convertSolanaChain + "/slip44:60", "", ""},
		{"wrong BTC profile", "utxo:20000000000002/slip44:1", "", ""},
		{"BTC token", convertBitcoinChain + "/token:" + solanaMint, solanaMint, ""},
		{"uncanonical reference", convertSolanaChain + "/token:" + solanaMint, solanaMint, ""},
		{"wrong token", convertSolanaChain + "/token:" + strings.ToLower(solanaMint), tronToken, ""},
		{"wrong TRON checksum", convertTronChain + "/token:" + strings.ToLower(tronToken[:33]+"1"), tronToken[:33] + "1", ""},
		{"inferred native", convertTronChain + "/slip44:195", tronToken, ""},
		{"Tempo native", "eip155:4217/slip44:60", "", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := protectedConvertChain(test.asset, test.token); got != test.chain {
				t.Fatalf("chain=%q, want %q", got, test.chain)
			}
		})
	}
}

func TestNonEVMConvertPreservesProtectedIdentityAndAddresses(t *testing.T) {
	key := testKey(t)
	c := testContext(key, DefaultServer)
	c.Binding.PolicyProfile = "financial-operations-v1"
	for _, test := range []struct {
		chain, asset, token, address string
		decimals                     int
	}{
		{convertSolanaChain, "slip44:501", "", "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v", 9},
		{convertSolanaChain, "token:epjfwdd5aufqssqem2qn1xzybapc8g4weggkzwytdt1v", "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v", "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v", 6},
		{convertTronChain, "slip44:195", "", "TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6t", 6},
		{convertTronChain, "token:tr7nhqjekqxgtci8q8zy4pl8otszgjlj6t", "TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6t", "TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6t", 6},
		{convertBitcoinChain, "slip44:0", "", "1BoatSLRHtKNngkdXEeobR76b53LETtpyT", 8},
	} {
		for _, destination := range []bool{false, true} {
			t.Run(test.chain+test.asset+map[bool]string{false: "/source", true: "/destination"}[destination], func(t *testing.T) {
				args := convertArgs()
				ctx := args["convertContext"].(document)
				ctx["sourceAsset"], ctx["originTokenAddress"], ctx["originAddress"] = test.chain+"/"+test.asset, test.token, test.address
				ctx["sourceDecimals"], ctx["destinationNetworkId"] = test.decimals, testOtherID
				args["gasSource"] = "GAS_SOURCE_FROM_WALLET"
				if destination {
					ctx["sourceAsset"], ctx["originTokenAddress"], ctx["originAddress"], ctx["sourceDecimals"] = "eip155:8453/slip44:60", "", "0x1111111111111111111111111111111111111111", 18
					ctx["destinationAsset"], ctx["destinationTokenAddress"], ctx["destinationDecimals"] = test.chain+"/"+test.asset, test.token, test.decimals
					args["destinationAddress"] = test.address
				}
				intent, body, err := convertIntent(key, c, args, time.Now())
				if err != nil {
					t.Fatal(err)
				}
				wire, err := base64.StdEncoding.DecodeString(stringAt(intent, "payloadBytes"))
				if err != nil {
					t.Fatal(err)
				}
				m := dynamicpb.NewMessage(approvalFile.Messages().ByName("SignedApprovalIntent"))
				if err := proto.Unmarshal(wire, m); err != nil {
					t.Fatal(err)
				}
				encoded, err := protojson.Marshal(m)
				if err != nil || !strings.Contains(string(encoded), test.address) || !strings.Contains(string(encoded), test.chain+"/"+test.asset) || body["convertContext"] != nil {
					t.Fatalf("lost protected identity or leaked context: %s, %v", encoded, err)
				}
				verifyText(t, key, stringAt(intent, "payloadBytes"), stringAt(intent, "signature"), false)
				if test.chain == convertBitcoinChain && !destination {
					args["gasSource"] = "GAS_SOURCE_GAS_STATION"
					if _, _, err := convertIntent(key, c, args, time.Now()); err == nil {
						t.Fatal("Bitcoin accepted sponsored gas")
					}
				}
				if destination {
					args["destinationAddress"] = "not-an-address"
				} else {
					ctx["originAddress"] = "not-an-address"
				}
				if _, _, err := convertIntent(key, c, args, time.Now()); err == nil {
					t.Fatal("signed an invalid conversion address")
				}
			})
		}
	}
}

func TestProtectedBitcoinRejectsOtherNetwork(t *testing.T) {
	if validProtectedConvertAddress(convertBitcoinChain, "mipcBbFg9gMiCh81Kj8tqqdgoZub1ZJRfn") {
		t.Fatal("protected mainnet convert accepted a testnet address")
	}
}

func TestProtectedConvertRejectsMalformedChainAddresses(t *testing.T) {
	for _, test := range []struct{ chain, address string }{
		{convertSolanaChain, "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt10"},
		{convertSolanaChain, base58.Encode(make([]byte, 31))},
		{convertTronChain, base58.CheckEncode(make([]byte, 20), 0x00)},
		{convertTronChain, base58.CheckEncode(make([]byte, 19), 0x41)},
		{convertBitcoinChain, "bc1invalid"},
	} {
		if validProtectedConvertAddress(test.chain, test.address) {
			t.Fatalf("accepted %s %s", test.chain, test.address)
		}
	}
}
