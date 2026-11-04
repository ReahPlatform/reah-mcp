package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"runtime/debug"
	"time"

	"github.com/ReahPlatform/reah-mcp/internal/client"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"
)

var version = "dev"

func command() *cobra.Command {
	var server, profile string
	buildVersion := version
	if buildVersion == "dev" {
		if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
			buildVersion = info.Main.Version
		}
	}
	root := &cobra.Command{Use: "reah-mcp", Short: "Connect AI tools to Reah with OAuth and local signing", Version: buildVersion, SilenceUsage: true, SilenceErrors: true}
	root.PersistentFlags().StringVar(&server, "server", client.DefaultServer, "Reah HTTPS MCP endpoint")
	root.PersistentFlags().StringVar(&profile, "profile", "default", "Local credential profile")
	makeOAuth := func() (*client.OAuth, error) {
		c, e := client.Configure(server, profile)
		if e != nil {
			return nil, e
		}
		return client.NewOAuth(c), nil
	}
	var enroll, noBrowser bool
	var policyProfile string
	login := &cobra.Command{Use: "login", Short: "Authorize this client in the browser", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		o, e := makeOAuth()
		if e != nil {
			return e
		}
		e = o.LoginWithPolicyProfile(cmd.Context(), enroll, policyProfile, func(u string) error {
			fmt.Fprintln(cmd.ErrOrStderr(), "Open this URL to continue (do not share it):\n"+u)
			if noBrowser {
				return nil
			}
			return openBrowser(cmd.Context(), u)
		})
		if e == nil {
			fmt.Fprintln(cmd.ErrOrStderr(), "Reah OAuth login complete. If requested, finish credential registration in the browser.")
		}
		return e
	}}
	login.Flags().StringVar(&policyProfile, "policy-profile", "wallet-transfer-v1", "Explicit signing policy: wallet-transfer-v1 or financial-operations-v1 (requires --enroll-key)")
	login.Flags().BoolVar(&enroll, "enroll-key", false, "Request owner-confirmed signing credential registration")
	login.Flags().BoolVar(&noBrowser, "no-browser", false, "Print the authorization URL without launching a browser")
	root.AddCommand(login)
	root.AddCommand(&cobra.Command{Use: "status", Short: "Show local connection status without secrets", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		o, e := makeOAuth()
		if e != nil {
			return e
		}
		d, e := o.Status(cmd.Context())
		if e != nil {
			return e
		}
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(d)
	}})
	root.AddCommand(&cobra.Command{Use: "logout", Short: "Revoke the connection and remove local credentials", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		o, e := makeOAuth()
		if e != nil {
			return e
		}
		if e = o.Logout(cmd.Context()); e != nil {
			return e
		}
		fmt.Fprintln(cmd.ErrOrStderr(), "Reah disconnected.")
		return nil
	}})
	root.AddCommand(&cobra.Command{Use: "enroll-key", Short: "Resume proof of possession for the current connection", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		o, e := makeOAuth()
		if e != nil {
			return e
		}
		return o.EnrollKey(cmd.Context())
	}})
	root.AddCommand(&cobra.Command{Use: "serve", Short: "Serve the authenticated Reah catalog over stdio", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		o, e := makeOAuth()
		if e != nil {
			return e
		}
		remote := &client.Remote{OAuth: o, Version: buildVersion}
		defer remote.Close()
		return client.NewServer(remote, buildVersion).Run(cmd.Context(), &mcp.StdioTransport{})
	}})
	return root
}
func openBrowser(parent context.Context, url string) error {
	ctx, cancel := context.WithTimeout(parent, 10*time.Second)
	defer cancel()
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.CommandContext(ctx, "open", url)
	case "windows":
		cmd = exec.CommandContext(ctx, "rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.CommandContext(ctx, "xdg-open", url)
	}
	if e := cmd.Run(); e != nil {
		return fmt.Errorf("browser could not be opened; rerun login with --no-browser")
	}
	return nil
}
func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	if err := command().ExecuteContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "reah-mcp:", err)
		os.Exit(1)
	}
}
