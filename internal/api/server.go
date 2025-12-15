package api

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/izalutski/catty/internal/db"
	"github.com/izalutski/catty/internal/fly"
)

// Server is the API server.
type Server struct {
	addr       string
	router     *chi.Mux
	httpServer *http.Server
}

// NewServer creates a new API server.
func NewServer(addr string) (*Server, error) {
	// Initialize Fly client
	flyClient, err := fly.NewClient()
	if err != nil {
		return nil, fmt.Errorf("create fly client: %w", err)
	}

	// Initialize database client
	dbClient, err := db.NewClient()
	if err != nil {
		return nil, fmt.Errorf("create database client: %w", err)
	}

	// Initialize auth handlers
	authHandlers, err := NewAuthHandlers()
	if err != nil {
		return nil, fmt.Errorf("create auth handlers: %w", err)
	}

	// Initialize billing handlers (optional - only if Stripe is configured)
	var billingHandlers *BillingHandlers
	if os.Getenv("STRIPE_SECRET_KEY") != "" {
		billingHandlers, err = NewBillingHandlers(dbClient)
		if err != nil {
			return nil, fmt.Errorf("create billing handlers: %w", err)
		}
	}

	// Create handlers
	handlers := NewHandlers(flyClient, dbClient)

	// Setup router
	r := chi.NewRouter()

	// Middleware
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(60 * time.Second))

	// Routes
	r.Route("/v1", func(r chi.Router) {
		// Auth endpoints (public)
		r.Post("/auth/device", authHandlers.StartDeviceAuth)
		r.Post("/auth/device/token", authHandlers.PollDeviceToken)

		// Billing endpoints (if configured)
		if billingHandlers != nil {
			// Webhook is public (verified by signature)
			r.Post("/billing/webhook", billingHandlers.HandleStripeWebhook)

			// Checkout requires auth - supports both GET (redirect) and POST (JSON)
			r.Group(func(r chi.Router) {
				r.Use(authHandlers.AuthMiddleware)
				r.Get("/billing/checkout", billingHandlers.CreateCheckoutSession)
				r.Post("/billing/checkout", billingHandlers.CreateCheckoutSession)
			})
		}

		// Protected session endpoints
		r.Group(func(r chi.Router) {
			r.Use(authHandlers.AuthMiddleware)
			r.Post("/sessions", handlers.CreateSession)
			r.Get("/sessions", handlers.ListSessions)
			r.Get("/sessions/{session_id}", handlers.GetSession)
			r.Post("/sessions/{session_id}/stop", handlers.StopSession)
		})
	})

	// Billing success/cancel pages (public, outside /v1)
	if billingHandlers != nil {
		r.Get("/billing/success", billingHandlers.BillingSuccess)
		r.Get("/billing/cancel", billingHandlers.BillingCancel)
	}

	// Health check
	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	return &Server{
		addr:   addr,
		router: r,
	}, nil
}

// Run starts the server and blocks until shutdown.
func (s *Server) Run() error {
	s.httpServer = &http.Server{
		Addr:         s.addr,
		Handler:      s.router,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Channel for shutdown signals
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, os.Interrupt, syscall.SIGTERM)

	// Channel for server errors
	serverErr := make(chan error, 1)

	// Start server
	go func() {
		log.Printf("Starting API server on %s", s.addr)
		if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serverErr <- err
		}
	}()

	// Wait for shutdown or error
	select {
	case err := <-serverErr:
		return fmt.Errorf("server error: %w", err)
	case <-shutdown:
		log.Println("Shutting down server...")
	}

	// Graceful shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := s.httpServer.Shutdown(ctx); err != nil {
		return fmt.Errorf("shutdown error: %w", err)
	}

	log.Println("Server stopped")
	return nil
}
