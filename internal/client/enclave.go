package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

// Populated only by macOS builds with the embedded, locally compiled helper.
// No system PATH lookup, runtime compiler or separately installed helper is used.
var enclaveHelper []byte

type enclaveRequest struct {
	Action    string `json:"action"`
	Reference string `json:"reference,omitempty"`
	PublicKey string `json:"publicKey,omitempty"`
	Payload   string `json:"payload,omitempty"`
}

type enclaveResponse struct {
	Availability string `json:"availability,omitempty"`
	Reference    string `json:"reference,omitempty"`
	PublicKey    string `json:"publicKey,omitempty"`
	Signature    string `json:"signature,omitempty"`
	Error        string `json:"error,omitempty"`
}

func enclaveBuildAvailable() bool { return runtime.GOOS == "darwin" && len(enclaveHelper) > 0 }

func enclaveAvailable() (bool, error) {
	result, err := runEnclave(enclaveRequest{Action: "availability"})
	if err != nil {
		return false, err
	}
	return parseEnclaveAvailability(result)
}

func parseEnclaveAvailability(result enclaveResponse) (bool, error) {
	if result == (enclaveResponse{Availability: "available"}) {
		return true, nil
	}
	if result == (enclaveResponse{Availability: "unsupported"}) {
		return false, nil
	}
	return false, errors.New("invalid Secure Enclave availability response; no software fallback is used")
}

func runEnclave(request enclaveRequest) (enclaveResponse, error) {
	var result enclaveResponse
	if !enclaveBuildAvailable() {
		return result, errors.New("Secure Enclave requires a compatible macOS build; use a macOS release or build with make build on a Mac with Xcode command line tools; no software fallback is used")
	}
	// A private, short-lived directory avoids mutable shared helper caches. The
	// extracted file contains executable code only; key references travel on stdin.
	dir, err := os.MkdirTemp("", "reah-mcp-enclave-")
	if err != nil {
		return result, errors.New("cannot prepare the embedded Secure Enclave helper")
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "signer")
	if err = os.WriteFile(path, enclaveHelper, 0700); err != nil {
		return result, errors.New("cannot prepare the embedded Secure Enclave helper")
	}
	body, err := json.Marshal(request)
	if err != nil {
		return result, err
	}
	if len(body) > 1024*1024 {
		return result, errors.New("Secure Enclave signing request exceeds its size limit")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, path)
	command.Dir = dir
	// Do not allow inherited DYLD_* variables to inject code into the signer.
	command.Env = []string{"PATH=/usr/bin:/bin"}
	command.Stdin = bytes.NewReader(body)
	var output limitedEnclaveOutput
	command.Stdout = &output
	command.Stderr = io.Discard
	if err = command.Run(); err != nil {
		return result, errors.New("Secure Enclave helper failed or timed out; unlock this Mac and retry; no signature was sent and no software fallback is used")
	}
	decoder := json.NewDecoder(bytes.NewReader(output.Bytes()))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&result); err != nil || decoder.Decode(new(any)) != io.EOF {
		return enclaveResponse{}, errors.New("invalid Secure Enclave helper response")
	}
	switch result.Error {
	case "":
		return result, nil
	case "hardware_unavailable":
		return enclaveResponse{}, errors.New("Secure Enclave is not available on this Mac; use a compatible physical Mac; no software fallback is used")
	case "key_unavailable":
		return enclaveResponse{}, errors.New("Secure Enclave key is unavailable or does not match this device; unlock the Mac and retry, or explicitly register a new device; no signature was sent and no software fallback is used")
	case "invalid_request", "key_mismatch", "access_control_unavailable":
		return enclaveResponse{}, errors.New("Secure Enclave rejected the key or signing request; no signature was sent and no software fallback is used")
	default:
		return enclaveResponse{}, errors.New("invalid Secure Enclave helper response")
	}
}

type limitedEnclaveOutput struct{ buffer bytes.Buffer }

func (b *limitedEnclaveOutput) Bytes() []byte { return b.buffer.Bytes() }

func (b *limitedEnclaveOutput) Write(value []byte) (int, error) {
	if b.buffer.Len()+len(value) > 4096 {
		return 0, errors.New("Secure Enclave helper response exceeds its limit")
	}
	return b.buffer.Write(value)
}
