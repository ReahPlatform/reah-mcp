package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/ReahPlatform/reah-mcp/internal/client"
)

func TestUpdaterSourceBuildDoesNotNeedCredentials(t *testing.T) {
	root := commandWithOAuth(func(client.Config) *client.OAuth { t.Fatal("updater accessed OAuth"); return nil })
	addUpdates(root)
	var output bytes.Buffer
	root.SetOut(&output)
	root.SetErr(&output)
	root.SetArgs([]string{"update", "--check"})
	if err := root.Execute(); err == nil || !strings.Contains(err.Error(), "no update channel") {
		t.Fatalf("expected explicit source-build status, got %v", err)
	}
}

func TestUpdaterHelpDoesNotNeedCredentials(t *testing.T) {
	root := commandWithOAuth(func(client.Config) *client.OAuth { t.Fatal("help accessed OAuth"); return nil })
	addUpdates(root)
	var output bytes.Buffer
	root.SetOut(&output)
	root.SetErr(&output)
	root.SetArgs([]string{"update", "--help"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, flag := range []string{"--check", "--no-auto-update", "--pin-version"} {
		if !strings.Contains(output.String(), flag) {
			t.Fatalf("missing %s", flag)
		}
	}
}

func TestBootstrapPinPreservesNewArguments(t *testing.T) {
	for _, test := range []struct {
		args      []string
		env, want string
	}{
		{[]string{"new-command", "--future-option", "value"}, "v1.0.0", "v1.0.0"},
		{[]string{"new-command", "--pin-version", "v1.2.0", "--future-option"}, "v1.0.0", "v1.2.0"},
		{[]string{"--pin-version=v1.1.0", "--version"}, "", "v1.1.0"},
		{[]string{"--", "--pin-version=v1.1.0"}, "v1.0.0", "v1.0.0"},
		{[]string{"--pin-version="}, "v1.0.0", ""},
	} {
		got, err := versionPin(test.args, test.env)
		if err != nil || got != test.want {
			t.Fatalf("%v: %q %v", test.args, got, err)
		}
	}
	if _, err := versionPin([]string{"--pin-version"}, ""); err == nil {
		t.Fatal("accepted missing version")
	}
}
