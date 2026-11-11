package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"runtime/debug"
	"strconv"
	"time"

	"github.com/ReahPlatform/reah-mcp/internal/client"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"
)

var version = "dev"

func command() *cobra.Command {
	root, _ := startup()
	return root
}

func startup() (*cobra.Command, func([]string) error) {
	root := commandWithOAuth(client.NewOAuth)
	return root, addUpdates(root)
}

func commandWithOAuth(factory func(client.Config) *client.OAuth) *cobra.Command {
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
		return factory(c), nil
	}
	var enroll, noBrowser, jsonProgress bool
	var policyProfile string
	progress := func(cmd *cobra.Command, noBrowser, jsonProgress bool) func(client.LoginStep) error {
		return func(step client.LoginStep) error {
			if jsonProgress {
				if err := json.NewEncoder(cmd.OutOrStdout()).Encode(step); err != nil {
					return err
				}
			} else {
				fmt.Fprintf(cmd.ErrOrStderr(), "[%s] %s\n", step.Stage, step.Message)
				if step.URL != "" {
					fmt.Fprintln(cmd.ErrOrStderr(), "Open this private URL (do not share it):\n"+step.URL)
				}
			}
			if step.URL != "" && !noBrowser {
				if err := openBrowser(cmd.Context(), step.URL); err != nil {
					// Keep the callback alive so opening the printed URL still works.
					fmt.Fprintln(cmd.ErrOrStderr(), "Browser launch failed. Open the URL printed for this stage manually; keep this command running.")
				}
			}
			return nil
		}
	}
	finish := func(cmd *cobra.Command, o *client.OAuth, jsonProgress bool) error {
		status, err := o.Status(cmd.Context())
		if err != nil {
			return err
		}
		if jsonProgress {
			return json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]any{"stage": "local_status", "status": status})
		}
		fmt.Fprintln(cmd.ErrOrStderr(), "Local credentials saved. Financial signing is not confirmed until status --check verifies it.")
		fmt.Fprintf(cmd.ErrOrStderr(), "Verify: reah-mcp status --check --server %s --profile %s\n", strconv.Quote(o.Config.Server), strconv.Quote(o.Config.Profile))
		if enrollRequested, _ := status["credentialEnrollmentRequested"].(bool); enrollRequested {
			fmt.Fprintf(cmd.ErrOrStderr(), "Resume the existing registration: reah-mcp enroll-key --server %s --profile %s\n", strconv.Quote(o.Config.Server), strconv.Quote(o.Config.Profile))
		}
		return nil
	}
	login := &cobra.Command{Use: "login", Short: "Authorize this client in the browser", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		o, e := makeOAuth()
		if e != nil {
			return e
		}
		e = o.LoginWithProgress(cmd.Context(), enroll, policyProfile, progress(cmd, noBrowser, jsonProgress))
		if e != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "Inspect saved progress before starting another login: reah-mcp status --check --server %s --profile %s\n", strconv.Quote(o.Config.Server), strconv.Quote(o.Config.Profile))
			return e
		}
		return finish(cmd, o, jsonProgress)
	}}
	login.Flags().StringVar(&policyProfile, "policy-profile", "wallet-transfer-v1", "Explicit signing policy: wallet-transfer-v1 or financial-operations-v1 (requires --enroll-key)")
	login.Flags().BoolVar(&enroll, "enroll-key", false, "Request user-confirmed signing credential registration")
	login.Flags().BoolVar(&noBrowser, "no-browser", false, "Print the authorization URL without launching a browser")
	login.Flags().BoolVar(&jsonProgress, "json", false, "Emit newline-delimited JSON stages and private continuation URLs for the Agent")
	root.AddCommand(login)
	var check bool
	status := &cobra.Command{Use: "status", Short: "Show connection stages and exact next actions without secrets", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		o, e := makeOAuth()
		if e != nil {
			return e
		}
		d, e := o.Status(cmd.Context())
		if e != nil && d == nil {
			return e
		}
		if check {
			remote := &client.Remote{OAuth: o, Version: buildVersion}
			defer remote.Close()
			d, e = remote.CheckStatus(cmd.Context())
			if d == nil {
				return e
			}
		}
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		if err := enc.Encode(d); err != nil {
			return err
		}
		return e
	}}
	status.Flags().BoolVar(&check, "check", false, "Verify live read access and each enrolled business's signing binding (may refresh OAuth; never moves funds)")
	root.AddCommand(status)
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
	var resumeNoBrowser, resumeJSON bool
	resume := &cobra.Command{Use: "enroll-key", Short: "Resume this connection's proof and browser registration without replacing credentials", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		o, e := makeOAuth()
		if e != nil {
			return e
		}
		if e = o.ResumeRegistration(cmd.Context(), progress(cmd, resumeNoBrowser, resumeJSON)); e != nil {
			return e
		}
		return finish(cmd, o, resumeJSON)
	}}
	resume.Flags().BoolVar(&resumeNoBrowser, "no-browser", false, "Print the existing credential registration URL without launching a browser")
	resume.Flags().BoolVar(&resumeJSON, "json", false, "Emit newline-delimited JSON stages and the private registration URL")
	root.AddCommand(resume)
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
		return fmt.Errorf("browser could not be opened")
	}
	return nil
}
func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	root, launch := startup()
	err := launch(os.Args[1:])
	if err == nil {
		err = root.ExecuteContext(ctx)
	}
	if err != nil {
		var delegated *delegatedExit
		if errors.As(err, &delegated) {
			os.Exit(delegated.code)
		}
		fmt.Fprintln(os.Stderr, "reah-mcp:", err)
		os.Exit(1)
	}
}
