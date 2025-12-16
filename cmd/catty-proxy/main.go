package main

import (
	"context"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/izalutski/catty/internal/db"
	"github.com/izalutski/catty/internal/proxy"
)

func main() {
	// Setup logger
	logLevel := slog.LevelInfo
	if os.Getenv("CATTY_DEBUG") == "1" {
		logLevel = slog.LevelDebug
	}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: logLevel}))

	// Get required env vars
	anthropicKey := os.Getenv("ANTHROPIC_API_KEY")
	if anthropicKey == "" {
		log.Fatal("ANTHROPIC_API_KEY is required")
	}

	// Initialize database
	dbClient, err := db.NewClient()
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer dbClient.Close()

	// Create proxy
	p, err := proxy.NewProxy(dbClient, anthropicKey, logger)
	if err != nil {
		log.Fatalf("Failed to create proxy: %v", err)
	}

	// Setup router
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(120 * time.Second)) // Long timeout for streaming

	// Health check
	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	// Proxy all Anthropic API paths via session-prefixed routes
	// Format: /s/{label}/v1/messages
	r.Handle("/s/*", p)

	// Get listen address
	addr := os.Getenv("CATTY_PROXY_ADDR")
	if addr == "" {
		addr = "0.0.0.0:8081"
	}

	// Create server
	server := &http.Server{
		Addr:         addr,
		Handler:      r,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 120 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Channel for shutdown signals
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, os.Interrupt, syscall.SIGTERM)

	// Channel for server errors
	serverErr := make(chan error, 1)

	// Start server
	go func() {
		logger.Info("Starting proxy server", "addr", addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serverErr <- err
		}
	}()

	// Wait for shutdown or error
	select {
	case err := <-serverErr:
		log.Fatalf("Server error: %v", err)
	case <-shutdown:
		logger.Info("Shutting down server...")
	}

	// Graceful shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Fatalf("Shutdown error: %v", err)
	}

	logger.Info("Server stopped")
}
