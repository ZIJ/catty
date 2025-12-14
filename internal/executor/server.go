package executor

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/coder/websocket"
)

// Server is the executor HTTP/WebSocket server.
type Server struct {
	connectToken string
	cmd          []string
	mu           sync.Mutex
	pty          *PTY
}

// NewServer creates a new executor server.
func NewServer() *Server {
	// Get connect token from environment
	token := os.Getenv("CONNECT_TOKEN")

	// Get command from environment or use default
	cmdStr := os.Getenv("CATTY_CMD")
	var cmd []string
	if cmdStr != "" {
		cmd = strings.Fields(cmdStr)
	} else {
		cmd = []string{"/bin/sh"}
	}

	slog.Info("executor starting", "command", cmd)

	return &Server{
		connectToken: token,
		cmd:          cmd,
	}
}

// Handler returns the HTTP handler for the server.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.handleHealthz)
	mux.HandleFunc("/connect", s.handleConnect)
	return mux
}

// handleHealthz handles health check requests.
func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}

// handleConnect handles WebSocket connection requests.
func (s *Server) handleConnect(w http.ResponseWriter, r *http.Request) {
	// Validate token
	if !s.validateToken(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// Accept WebSocket
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		CompressionMode: websocket.CompressionDisabled,
	})
	if err != nil {
		slog.Error("websocket accept failed", "error", err)
		return
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	// Get or create PTY
	pty, err := s.getOrCreatePTY()
	if err != nil {
		slog.Error("pty creation failed", "error", err)
		conn.Close(websocket.StatusInternalError, "failed to create pty")
		return
	}

	slog.Info("client connected, starting relay")

	// Run relay
	relay := NewRelay(conn, pty)
	if err := relay.Run(context.Background()); err != nil {
		slog.Error("relay error", "error", err)
	}
}

// validateToken checks if the request has a valid token.
func (s *Server) validateToken(r *http.Request) bool {
	if s.connectToken == "" {
		// No token configured, allow all (for local testing)
		return true
	}

	auth := r.Header.Get("Authorization")
	if auth == "" {
		return false
	}

	// Expect "Bearer <token>"
	parts := strings.SplitN(auth, " ", 2)
	if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
		return false
	}

	return parts[1] == s.connectToken
}

// getOrCreatePTY returns the existing PTY or creates a new one.
func (s *Server) getOrCreatePTY() (*PTY, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.pty != nil {
		return s.pty, nil
	}

	slog.Debug("creating PTY", "command", s.cmd)

	pty := NewPTY(s.cmd[0], s.cmd[1:]...)
	if err := pty.Start(); err != nil {
		slog.Error("PTY start failed", "error", err)
		return nil, err
	}

	s.pty = pty
	return pty, nil
}
