//go:build !windows

package client

import (
	"os"
	"syscall"
)

func privateDirectory(info os.FileInfo) bool {
	s, ok := info.Sys().(*syscall.Stat_t)
	return ok && s.Uid == uint32(os.Getuid()) && info.Mode().Perm()&0077 == 0
}

func privateFile(info os.FileInfo) bool {
	s, ok := info.Sys().(*syscall.Stat_t)
	return ok && s.Uid == uint32(os.Getuid()) && info.Mode().IsRegular() && info.Mode().Perm()&0077 == 0
}
