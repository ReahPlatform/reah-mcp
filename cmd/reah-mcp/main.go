package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ReahPlatform/reah-mcp/internal/gateway"
	"github.com/ReahPlatform/reah-mcp/internal/server"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	if err := run(); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}

func run() error {
	transport := flag.String("transport", "stdio", "stdio or http")
	listen := flag.String("listen", "127.0.0.1:8080", "HTTP listen address; use a TLS reverse proxy for remote access")
	flag.Parse()
	if *transport != "stdio" && *transport != "http" {
		return fmt.Errorf("unknown transport %q", *transport)
	}
	c, err := gateway.New(os.Getenv("REAH_GRAPHQL_URL"), os.Getenv("REAH_ACCESS_TOKEN"), os.Getenv("REAH_ENTITY_ID"))
	if err != nil {
		return err
	}
	s, err := server.New(c)
	if err != nil {
		return err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if *transport == "stdio" {
		return s.Run(ctx, &mcp.StdioTransport{})
	}
	handler, err := server.HTTPHandler(s, os.Getenv("REAH_MCP_HTTP_TOKEN"))
	if err != nil {
		return err
	}
	h := &http.Server{Addr: *listen, Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 45 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	done := make(chan error, 1)
	go func() { done <- h.ListenAndServe() }()
	log.Printf("reah-mcp HTTP listening on %s/mcp", *listen)
	select {
	case err := <-done:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdown, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		return h.Shutdown(shutdown)
	}
}
