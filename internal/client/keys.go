package client

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
)

type DeviceKey struct {
	PrivateKey string `json:"privateKey"`
	PublicKey  string `json:"publicKey"`
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
	return DeviceKey{string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: b})), hex.EncodeToString(elliptic.MarshalCompressed(k.Curve, k.X, k.Y))}, nil
}

func privateKey(key DeviceKey) (*ecdsa.PrivateKey, error) {
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

func signText(key DeviceKey, payload string, der bool) (string, error) {
	k, err := privateKey(key)
	if err != nil {
		return "", err
	}
	h := sha256.Sum256([]byte(payload))
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
