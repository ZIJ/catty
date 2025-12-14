package executor

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/coder/websocket"
)

const (
	// WorkspaceDir is where uploaded workspaces are extracted.
	WorkspaceDir = "/workspace"
	// MaxUploadSize is the maximum size of workspace upload (100MB).
	MaxUploadSize = 100 << 20
)

// Server is the executor HTTP/WebSocket server.
type Server struct {
	connectToken    string
	cmd             []string
	mu              sync.Mutex
	pty             *PTY
	workspaceReady  bool
	workspaceDir    string
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
	mux.HandleFunc("/upload", s.handleUpload)
	mux.HandleFunc("/connect", s.handleConnect)
	return mux
}

// handleHealthz handles health check requests.
func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}

// handleUpload handles workspace zip uploads.
func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Validate token
	if !s.validateToken(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// Check if already uploaded
	s.mu.Lock()
	if s.workspaceReady {
		s.mu.Unlock()
		http.Error(w, "workspace already uploaded", http.StatusConflict)
		return
	}
	s.mu.Unlock()

	// Limit upload size
	r.Body = http.MaxBytesReader(w, r.Body, MaxUploadSize)

	// Create workspace directory
	if err := os.MkdirAll(WorkspaceDir, 0755); err != nil {
		slog.Error("failed to create workspace dir", "error", err)
		http.Error(w, "failed to create workspace", http.StatusInternalServerError)
		return
	}

	// Save uploaded zip to temp file
	tmpFile, err := os.CreateTemp("", "workspace-*.zip")
	if err != nil {
		slog.Error("failed to create temp file", "error", err)
		http.Error(w, "failed to process upload", http.StatusInternalServerError)
		return
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	// Copy upload to temp file
	written, err := io.Copy(tmpFile, r.Body)
	tmpFile.Close()
	if err != nil {
		slog.Error("failed to save upload", "error", err)
		http.Error(w, "failed to save upload", http.StatusInternalServerError)
		return
	}

	slog.Info("received workspace upload", "size", written)

	// Extract zip
	if err := extractZip(tmpPath, WorkspaceDir); err != nil {
		slog.Error("failed to extract workspace", "error", err)
		http.Error(w, "failed to extract workspace: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Mark workspace as ready
	s.mu.Lock()
	s.workspaceReady = true
	s.workspaceDir = WorkspaceDir
	s.mu.Unlock()

	slog.Info("workspace extracted", "dir", WorkspaceDir)
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}

// extractZip extracts a zip file to the destination directory.
func extractZip(zipPath, destDir string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("failed to open zip: %w", err)
	}
	defer r.Close()

	for _, f := range r.File {
		// Security: prevent zip slip
		destPath := filepath.Join(destDir, f.Name)
		if !strings.HasPrefix(destPath, filepath.Clean(destDir)+string(os.PathSeparator)) {
			return fmt.Errorf("invalid file path: %s", f.Name)
		}

		if f.FileInfo().IsDir() {
			os.MkdirAll(destPath, f.Mode())
			continue
		}

		// Create parent directories
		if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
			return fmt.Errorf("failed to create dir: %w", err)
		}

		// Extract file
		destFile, err := os.OpenFile(destPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			return fmt.Errorf("failed to create file: %w", err)
		}

		srcFile, err := f.Open()
		if err != nil {
			destFile.Close()
			return fmt.Errorf("failed to open zip entry: %w", err)
		}

		_, err = io.Copy(destFile, srcFile)
		srcFile.Close()
		destFile.Close()
		if err != nil {
			return fmt.Errorf("failed to extract file: %w", err)
		}
	}

	return nil
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

	// Determine working directory
	workDir := "/"
	if s.workspaceReady && s.workspaceDir != "" {
		workDir = s.workspaceDir
	}

	slog.Debug("creating PTY", "command", s.cmd, "workdir", workDir)

	pty := NewPTY(s.cmd[0], s.cmd[1:]...)
	pty.SetWorkDir(workDir)
	if err := pty.Start(); err != nil {
		slog.Error("PTY start failed", "error", err)
		return nil, err
	}

	s.pty = pty
	return pty, nil
}
