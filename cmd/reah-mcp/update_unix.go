//go:build !windows

package main

import (
	"os"
	"syscall"
)

func runUpdated(path string) error {
	return syscall.Exec(path, append([]string{path}, os.Args[1:]...), os.Environ())
}
