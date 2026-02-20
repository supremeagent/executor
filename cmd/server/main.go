package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/mylxsw/asteria/log"
	"github.com/supremeagent/executor/internal/httpapi"
	"github.com/supremeagent/executor/pkg/sdk"
)

func main() {
	addr := flag.String("addr", "0.0.0.0:8080", "Server address")
	flag.Parse()

	client := sdk.New()
	handler := httpapi.NewHandler(client)
	router := httpapi.NewRouter(handler)

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
	client.Shutdown()
	log.Info("Server stopped")
}
