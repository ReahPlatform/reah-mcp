package main

import (
	"errors"
	"os"
	"os/exec"
)

func runUpdated(path string) error {
	child := exec.Command(path, os.Args[1:]...)
	child.Stdin, child.Stdout, child.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := child.Run(); err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return &delegatedExit{code: exit.ExitCode()}
		}
		return err
	}
	return &delegatedExit{code: 0}
}
