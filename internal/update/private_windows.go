package update

import (
	"errors"
	"os"
)

// The cache inherits the current user's profile directory ACLs on Windows.
func privatePath(path string, directory bool) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || (directory && !info.IsDir()) || (!directory && !info.Mode().IsRegular()) {
		return errors.New("update cache contains a non-regular file or directory")
	}
	return nil
}
