package client

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/asn1"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"math/big"
)

type DeviceKey struct {
	PrivateKey       string `json:"privateKey,omitempty"`
	PublicKey        string `json:"publicKey"`
	Storage          string `json:"storage,omitempty"`
	EnclaveReference string `json:"enclaveReference,omitempty"`
}

const (
	softwareKeyStorage = "software"
	enclaveKeyStorage  = "secure-enclave"
)

func keyStorage(key DeviceKey) string {
	if key.Storage == "" {
		return softwareKeyStorage
	}
	return key.Storage
}

func generateStoredKey(storage string) (DeviceKey, error) {
	switch storage {
	case softwareKeyStorage:
		return generateKey()
	case enclaveKeyStorage:
		result, err := runEnclave(enclaveRequest{Action: "create"})
		if err != nil {
			return DeviceKey{}, err
		}
		key := DeviceKey{PublicKey: result.PublicKey, Storage: enclaveKeyStorage, EnclaveReference: result.Reference}
		return key, validateDeviceKey(key)
	default:
		return DeviceKey{}, errors.New("unsupported key storage; use software or secure-enclave")
	}
}

func generateKey() (DeviceKey, error) {
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return DeviceKey{}, err
	}
	b, err := x509.MarshalPKCS8PrivateKey(k)
	if err != nil {
		return DeviceKey{}, err
	}
	return DeviceKey{PrivateKey: string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: b})), PublicKey: hex.EncodeToString(elliptic.MarshalCompressed(k.Curve, k.X, k.Y))}, nil
}

func privateKey(key DeviceKey) (*ecdsa.PrivateKey, error) {
	if keyStorage(key) != softwareKeyStorage || key.EnclaveReference != "" {
		return nil, errors.New("device private key is not exportable")
	}
	b, rest := pem.Decode([]byte(key.PrivateKey))
	if b == nil || len(rest) != 0 || b.Type != "PRIVATE KEY" {
		return nil, errors.New("invalid local device key; reconnect")
	}
	v, err := x509.ParsePKCS8PrivateKey(b.Bytes)
	if err != nil {
		return nil, errors.New("invalid local device key; reconnect")
	}
	k, ok := v.(*ecdsa.PrivateKey)
	if !ok || k.Curve != elliptic.P256() || hex.EncodeToString(elliptic.MarshalCompressed(k.Curve, k.X, k.Y)) != key.PublicKey {
		return nil, errors.New("local device key does not match its public key")
	}
	return k, nil
}

// Validate persisted metadata without opening hardware. OAuth reads, refresh and
// revocation must remain possible even when the signing device is unavailable.
func validateDeviceKey(key DeviceKey) error {
	if keyStorage(key) == softwareKeyStorage {
		_, err := privateKey(key)
		return err
	}
	if key.Storage != enclaveKeyStorage || key.PrivateKey != "" {
		return errors.New("invalid local device key storage; reconnect")
	}
	point, err := hex.DecodeString(key.PublicKey)
	x, _ := elliptic.UnmarshalCompressed(elliptic.P256(), point)
	ref, refErr := base64.StdEncoding.Strict().DecodeString(key.EnclaveReference)
	if err != nil || x == nil || hex.EncodeToString(point) != key.PublicKey || refErr != nil || len(ref) == 0 || len(ref) > 1024 || base64.StdEncoding.EncodeToString(ref) != key.EnclaveReference {
		return errors.New("invalid Secure Enclave key reference; reconnect")
	}
	return nil
}

func signText(key DeviceKey, payload string, der bool) (string, error) {
	if err := validateDeviceKey(key); err != nil {
		return "", err
	}
	h := sha256.Sum256([]byte(payload))
	if keyStorage(key) == enclaveKeyStorage {
		result, err := runEnclave(enclaveRequest{Action: "sign", Reference: key.EnclaveReference, PublicKey: key.PublicKey, Payload: base64.StdEncoding.EncodeToString([]byte(payload))})
		if err != nil {
			return "", err
		}
		b, err := base64.StdEncoding.Strict().DecodeString(result.Signature)
		if err != nil {
			return "", errors.New("invalid Secure Enclave signature")
		}
		return checkedEnclaveSignature(key.PublicKey, h[:], b, der)
	}
	k, err := privateKey(key)
	if err != nil {
		return "", err
	}
	if der {
		b, e := ecdsa.SignASN1(rand.Reader, k, h[:])
		return hex.EncodeToString(b), e
	}
	r, s, err := ecdsa.Sign(rand.Reader, k, h[:])
	if err != nil {
		return "", err
	}
	b := make([]byte, 64)
	r.FillBytes(b[:32])
	s.FillBytes(b[32:])
	return hex.EncodeToString(b), nil
}

// CryptoKit emits DER; existing enrollment and execution protocols also require
// fixed-width P1363. Verify the digest and enrolled public key before converting.
func checkedEnclaveSignature(publicKey string, digest, signature []byte, der bool) (string, error) {
	point, _ := hex.DecodeString(publicKey)
	x, y := elliptic.UnmarshalCompressed(elliptic.P256(), point)
	if x == nil || len(digest) != sha256.Size || !ecdsa.VerifyASN1(&ecdsa.PublicKey{Curve: elliptic.P256(), X: x, Y: y}, digest, signature) {
		return "", errors.New("Secure Enclave signature does not match the device key and request")
	}
	if der {
		return hex.EncodeToString(signature), nil
	}
	var values struct{ R, S *big.Int }
	if rest, err := asn1.Unmarshal(signature, &values); err != nil || len(rest) != 0 || values.R == nil || values.S == nil {
		return "", errors.New("invalid Secure Enclave signature")
	}
	b := make([]byte, 64)
	values.R.FillBytes(b[:32])
	values.S.FillBytes(b[32:])
	return hex.EncodeToString(b), nil
}
