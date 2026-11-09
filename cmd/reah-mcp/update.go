package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/ReahPlatform/reah-mcp/internal/update"
	"github.com/spf13/cobra"
)

// Release builds embed a fixed trust root. Source builds do not auto-update.
var updateManifestURL, updatePublicKey string

type delegatedExit struct{ code int }

func (e *delegatedExit) Error() string {
	return fmt.Sprintf("updated client exited with status %d", e.code)
}

func addUpdates(root *cobra.Command) func([]string) error {
	var disabled bool
	pin := os.Getenv("REAH_MCP_VERSION")
	root.PersistentFlags().BoolVar(&disabled, "no-auto-update", false, "Disable automatic downloads (already staged updates still apply)")
	root.PersistentFlags().StringVar(&pin, "pin-version", pin, "Run this installed version and disable automatic downloads (or REAH_MCP_VERSION)")
	manager, configErr := update.New(update.Config{Version: root.Version, ManifestURL: updateManifestURL, PublicKey: updatePublicKey})
	var check bool
	updateCmd := &cobra.Command{Use: "update", Short: "Verify and stage the latest signed stable release for the next start", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if configErr != nil {
			return configErr
		}
		if manager == nil {
			return errors.New("this build has no update channel; install an official release to enable updates")
		}
		if pin != "" && !check {
			return errors.New("remove the version pin before installing an update")
		}
		result, err := manager.Check(cmd.Context(), !check)
		if err != nil {
			return err
		}
		return json.NewEncoder(cmd.OutOrStdout()).Encode(result)
	}}
	updateCmd.Flags().BoolVar(&check, "check", false, "Check signed release metadata without downloading an executable")
	root.AddCommand(updateCmd)
	for _, cmd := range root.Commands() {
		if cmd.Name() != "serve" {
			continue
		}
		serve := cmd.RunE
		cmd.RunE = func(cmd *cobra.Command, args []string) error {
			if manager == nil || disabled || pin != "" || os.Getenv("REAH_MCP_AUTO_UPDATE") == "off" {
				return serve(cmd, args)
			}
			ctx, cancel := context.WithCancel(cmd.Context())
			done := make(chan struct{})
			go func() {
				defer close(done)
				manager.Auto(ctx, cmd.ErrOrStderr())
			}()
			defer func() { cancel(); <-done }()
			return serve(cmd, args)
		}
	}
	// Delegate before Cobra parses commands and flags. The original launcher must
	// accept commands introduced by a newer cached client, including --version.
	return func(args []string) error {
		if configErr != nil {
			return configErr
		}
		selectedPin, err := versionPin(args, os.Getenv("REAH_MCP_VERSION"))
		if err != nil {
			return err
		}
		if manager == nil {
			if selectedPin != "" && selectedPin != root.Version {
				return errors.New("version pin requires an installed release; this build has no update channel")
			}
			return nil
		}
		path, err := manager.Select(selectedPin)
		if err != nil {
			if selectedPin != "" {
				return err
			}
			fmt.Fprintf(root.ErrOrStderr(), "reah-mcp update: %v; using the current executable.\n", err)
			return nil
		}
		if path != "" {
			// A mispackaged release must fail instead of delegating to itself forever.
			current, err := os.Executable()
			if err != nil {
				return err
			}
			running, err := os.Stat(current)
			if err != nil {
				return err
			}
			selected, err := os.Stat(path)
			if err != nil {
				return err
			}
			if os.SameFile(running, selected) {
				return errors.New("cached executable does not match its release version; reinstall an official release")
			}
			// Never retry an operation in the launcher after the child starts.
			return runUpdated(path)
		}
		return nil
	}
}

// Only this stable bootstrap option is interpreted by the old launcher; all
// other arguments are passed unchanged to the selected release.
func versionPin(args []string, pin string) (string, error) {
	for i := 0; i < len(args); i++ {
		if args[i] == "--" {
			break
		}
		if value, ok := strings.CutPrefix(args[i], "--pin-version="); ok {
			pin = value
		} else if args[i] == "--pin-version" {
			i++
			if i == len(args) {
				return "", errors.New("--pin-version requires a version")
			}
			pin = args[i]
		}
	}
	return pin, nil
}
