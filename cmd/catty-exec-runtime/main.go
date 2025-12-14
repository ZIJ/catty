package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/izalutski/catty/internal/executor"
)

func main() {
	addr := os.Getenv("CATTY_EXEC_ADDR")
	if addr == "" {
		addr = ":8080"
	}

	server := executor.NewServer()

	httpServer := &http.Server{
		Addr:         addr,
		Handler:      server.Handler(),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 0, // No timeout for WebSocket
		IdleTimeout:  120 * time.Second,
	}

	// Channel for shutdown signals
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, os.Interrupt, syscall.SIGTERM)

	// Channel for server errors
	serverErr := make(chan error, 1)

	// Start server
	go func() {
		log.Printf("Starting executor server on %s", addr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serverErr <- err
		}
	}()

	// Wait for shutdown or error
	select {
	case err := <-serverErr:
		log.Fatalf("Server error: %v", err)
	case <-shutdown:
		log.Println("Shutting down server...")
	}

	// Graceful shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := httpServer.Shutdown(ctx); err != nil {
		log.Fatalf("Shutdown error: %v", err)
	}

	log.Println("Server stopped")
}
