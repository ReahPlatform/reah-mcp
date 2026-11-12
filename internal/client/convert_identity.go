package client

import (
	"strings"

	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/btcutil/base58"
	"github.com/btcsuite/btcd/chaincfg"
)

const (
	convertSolanaChain  = "svm:1151111081099710"
	convertBitcoinChain = "utxo:20000000000001"
	convertTronChain    = "tron:728126428"
)

// Protected converts use Ledger's numeric chain identities. These differ from
// Send v2 identities; non-EVM token fields retain their original base58 case.
func protectedConvertChain(asset, token string) string {
	if chain := convertAsset(asset, token); chain != "" {
		if chain == "4217" && token == "" {
			return ""
		}
		return "eip155:" + chain
	}
	chain, reference, ok := strings.Cut(asset, "/")
	if !ok {
		return ""
	}
	native := ""
	switch chain {
	case convertSolanaChain:
		native = "slip44:501"
	case convertBitcoinChain:
		native = "slip44:0"
	case convertTronChain:
		native = "slip44:195"
	default:
		return ""
	}
	if token == "" {
		if reference == native {
			return chain
		}
		return ""
	}
	if chain != convertBitcoinChain && validProtectedConvertAddress(chain, token) && reference == "token:"+strings.ToLower(token) {
		return chain
	}
	return ""
}

func validProtectedConvertAddress(chain, address string) bool {
	if address != strings.TrimSpace(address) {
		return false
	}
	switch {
	case strings.HasPrefix(chain, "eip155:"):
		return evmAddress.MatchString(address) && !strings.EqualFold(address, "0x0000000000000000000000000000000000000000")
	case chain == convertSolanaChain:
		return len(address) >= 32 && len(address) <= 44 && len(base58.Decode(address)) == 32
	case chain == convertTronChain:
		payload, version, err := base58.CheckDecode(address)
		return err == nil && version == 0x41 && len(payload) == 20
	case chain == convertBitcoinChain:
		decoded, err := btcutil.DecodeAddress(address, &chaincfg.MainNetParams)
		return err == nil && decoded.IsForNet(&chaincfg.MainNetParams)
	default:
		return false
	}
}
