package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/mylxsw/asteria/log"
	"github.com/supremeagent/executor/internal/httpapi"
	"github.com/supremeagent/executor/internal/mcpapi"
	"github.com/supremeagent/executor/pkg/sdk"
)

func main() {
	addr := flag.String("addr", "0.0.0.0:8080", "Server address")
	mcp := flag.Bool("mcp", false, "Run as MCP stdio server instead of HTTP server")
	flag.Parse()

	client := sdk.New()
	defer client.Shutdown()

	if *mcp {
		runMCPServer(client)
		return
	}

	handler := httpapi.NewHandler(client)
	router := httpapi.NewRouter(handler)

	// Register the MCP HTTP (Streamable HTTP) endpoint so remote clients can
	// connect via the Model Context Protocol over HTTP.
	mcpSrv := mcpapi.NewExecutorServer(client)
	router.Handle("/mcp", mcpSrv.HTTPHandler()).Methods(http.MethodPost)

	server := &http.Server{Addr: *addr, Handler: router}

	go func() {
		log.Infof("Starting server on %s", *addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Fprintf(os.Stderr, "Server error: %v\n", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Info("Shutting down server...")
}

func runMCPServer(client *sdk.Client) {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	srv := mcpapi.NewExecutorServer(client)
	if err := srv.Serve(ctx); err != nil && err != context.Canceled {
		fmt.Fprintf(os.Stderr, "MCP server error: %v\n", err)
		os.Exit(1)
	}
}
