package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestCommandContractWithoutAccessingCredentials(t *testing.T) {
	for _, name := range []string{"login", "status", "logout", "enroll-key", "serve"} {
		cmd := command()
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&out)
		cmd.SetArgs([]string{name, "--help"})
		if e := cmd.Execute(); e != nil {
			t.Fatal(e)
		}
		if !strings.Contains(out.String(), "--profile") || !strings.Contains(out.String(), "--server") {
			t.Fatalf("missing shared flags: %s", name)
		}
	}
	for _, args := range [][]string{{"serve", "extra"}, {"status", "--server", "http://invalid.example/mcp"}, {"login", "--profile", "../../unsafe"}} {
		cmd := command()
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&out)
		cmd.SetArgs(args)
		if e := cmd.Execute(); e == nil {
			t.Errorf("accepted invalid CLI: %v", args)
		}
		if out.Len() != 0 {
			t.Fatal("error wrote help or credentials to stdout")
		}
	}
}

func TestStdioInitializationWithoutCredentials(t *testing.T) {
	executable, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	child := exec.CommandContext(ctx, executable, "-test.run=^TestStdioHelper$")
	child.Env = append(os.Environ(), "REAH_TEST_STDIO_HELPER=1", "PATH=/usr/bin:/bin")
	var stderr bytes.Buffer
	child.Stderr = &stderr
	in, e := child.StdinPipe()
	if e != nil {
		t.Fatal(e)
	}
	out, e := child.StdoutPipe()
	if e != nil {
		t.Fatal(e)
	}
	if e = child.Start(); e != nil {
		t.Fatal(e)
	}
	defer func() { _ = child.Process.Kill() }()
	encoder := json.NewEncoder(in)
	if e = encoder.Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{"protocolVersion": "2025-11-25", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "test-host", "version": "1"}}}); e != nil {
		t.Fatal(e)
	}
	var reply struct {
		Result struct {
			Capabilities map[string]any `json:"capabilities"`
		} `json:"result"`
	}
	if e = json.NewDecoder(out).Decode(&reply); e != nil {
		t.Fatalf("stdio initialization: %v; %s", e, stderr.String())
	}
	if _, ok := reply.Result.Capabilities["tools"]; !ok {
		t.Fatal("tools capability missing")
	}
	if e = encoder.Encode(map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"}); e != nil {
		t.Fatal(e)
	}
	_ = in.Close()
	if e = child.Wait(); e != nil {
		t.Fatalf("stdio shutdown: %v; %s", e, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("unexpected initialization output: %s", stderr.String())
	}
}

func TestStdioHelper(t *testing.T) {
	if os.Getenv("REAH_TEST_STDIO_HELPER") != "1" {
		return
	}
	cmd := command()
	cmd.SetArgs([]string{"serve"})
	if e := cmd.Execute(); e != nil {
		os.Exit(1)
	}
	os.Exit(0)
}
