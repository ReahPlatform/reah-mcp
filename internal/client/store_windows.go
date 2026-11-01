package client

import "os"

// Windows protects the user's profile through its ACL rather than Unix mode bits.
func privateDirectory(info os.FileInfo) bool { return info.IsDir() }
func privateFile(info os.FileInfo) bool      { return info.Mode().IsRegular() }
