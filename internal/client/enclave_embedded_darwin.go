//go:build darwin && secureenclave

package client

import _ "embed"

//go:embed enclave-helper.bin
var embeddedEnclaveHelper []byte

func init() { enclaveHelper = embeddedEnclaveHelper }
