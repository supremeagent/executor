package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/mylxsw/asteria/log"
	"github.com/supremeagent/executor/pkg/api"
	"github.com/supremeagent/executor/pkg/executor"
	"github.com/supremeagent/executor/pkg/executor/claude"
	"github.com/supremeagent/executor/pkg/executor/codex"
	"github.com/supremeagent/executor/pkg/streaming"
)

func main() {
	addr := flag.String("addr", "0.0.0.0:8080", "Server address")
	flag.Parse()

	// Create executor registry
	registry := executor.NewRegistry()

	// Register Claude Code executor
	claudeFactory := claude.NewFactory()
	registry.Register("claude_code", claudeFactory)

	// Register Codex executor
	codexFactory := codex.NewFactory()
	registry.Register("codex", codexFactory)

	// Create SSE manager
	sseManager := streaming.NewManager()

	// Create API handler
	handler := api.NewHandler(registry, sseManager)

	// Create router
	router := api.NewRouter(handler)

	// Create server
	server := &http.Server{
		Addr:    *addr,
		Handler: router,
	}

	// Start server in goroutine
	go func() {
		log.Infof("Starting server on %s", *addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Fprintf(os.Stderr, "Server error: %v\n", err)
			os.Exit(1)
		}
	}()

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Info("Shutting down server...")

	// Graceful shutdown
	registry.ShutdownAll()

	log.Info("Server stopped")
}
