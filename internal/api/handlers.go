package api

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/user"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/izalutski/catty/internal/fly"
)

// CreateSessionRequest is the request body for creating a session.
type CreateSessionRequest struct {
	Agent    string   `json:"agent"`
	Cmd      []string `json:"cmd"`
	Region   string   `json:"region"`
	CPUs     int      `json:"cpus"`
	MemoryMB int      `json:"memory_mb"`
	TTLSec   int      `json:"ttl_sec"`
}

// CreateSessionResponse is the response for creating a session.
type CreateSessionResponse struct {
	SessionID    string            `json:"session_id"`
	MachineID    string            `json:"machine_id"`
	ConnectURL   string            `json:"connect_url"`
	ConnectToken string            `json:"connect_token"`
	Headers      map[string]string `json:"headers"`
}

// SessionResponse is the response for getting a session.
type SessionResponse struct {
	SessionID    string    `json:"session_id"`
	MachineID    string    `json:"machine_id"`
	ConnectURL   string    `json:"connect_url"`
	Region       string    `json:"region"`
	CreatedAt    time.Time `json:"created_at"`
	MachineState string    `json:"machine_state,omitempty"`
}

// ErrorResponse is the response for errors.
type ErrorResponse struct {
	Error string `json:"error"`
}

// Handlers contains HTTP handlers for the API.
type Handlers struct {
	flyClient *fly.Client
	store     *SessionStore
}

// NewHandlers creates new API handlers.
func NewHandlers(flyClient *fly.Client, store *SessionStore) *Handlers {
	return &Handlers{
		flyClient: flyClient,
		store:     store,
	}
}

// getImage returns the executor image to use for new machines.
// Fetches fresh each time to pick up new deployments.
func (h *Handlers) getImage() (string, error) {
	return h.flyClient.GetCurrentImage()
}

// CreateSession handles POST /v1/sessions.
func (h *Handlers) CreateSession(w http.ResponseWriter, r *http.Request) {
	var req CreateSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	// Generate session ID and connect token
	sessionID := uuid.New().String()
	connectToken, err := generateToken(32)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate token: "+err.Error())
		return
	}

	// Get current user for metadata
	currentUser, _ := user.Current()
	owner := "unknown"
	if currentUser != nil {
		owner = currentUser.Username
	}

	// Set defaults
	if req.Region == "" || req.Region == "auto" {
		req.Region = "iad"
	}
	if req.CPUs == 0 {
		req.CPUs = 1
	}
	if req.MemoryMB == 0 {
		req.MemoryMB = 1024
	}
	if len(req.Cmd) == 0 {
		req.Cmd = []string{"/bin/sh"}
	}

	// Get the current executor image
	image, err := h.getImage()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get executor image: "+err.Error())
		return
	}

	// Build environment for the machine
	machineEnv := map[string]string{
		"CONNECT_TOKEN": connectToken,
		"CATTY_CMD":     joinCmd(req.Cmd),
	}

	// Pass through ANTHROPIC_API_KEY if available
	if apiKey := os.Getenv("ANTHROPIC_API_KEY"); apiKey != "" {
		machineEnv["ANTHROPIC_API_KEY"] = apiKey
	}

	// Build machine config
	machineReq := &fly.CreateMachineRequest{
		Region: req.Region,
		Config: &fly.MachineConfig{
			Image: image,
			Env:   machineEnv,
			Services: []fly.MachineService{
				{
					Protocol:     "tcp",
					InternalPort: 8080,
					Ports: []fly.ServicePort{
						{Port: 443, Handlers: []string{"tls", "http"}},
						{Port: 80, Handlers: []string{"http"}},
					},
				},
			},
			Guest: &fly.GuestConfig{
				CPUs:     req.CPUs,
				MemoryMB: req.MemoryMB,
				CPUKind:  "shared",
			},
			Metadata: map[string]string{
				"project": "catty",
				"session": sessionID,
				"owner":   owner,
				"agent":   req.Agent,
			},
		},
	}

	// Create the machine
	machine, err := h.flyClient.CreateMachine(machineReq)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create machine: "+err.Error())
		return
	}

	// Wait for machine to start
	if err := h.flyClient.WaitMachine(machine.ID, "started", 60*time.Second); err != nil {
		// Try to clean up
		h.flyClient.DeleteMachine(machine.ID, true)
		writeError(w, http.StatusInternalServerError, "machine failed to start: "+err.Error())
		return
	}

	// Build connect URL
	connectURL := fmt.Sprintf("wss://%s.fly.dev/connect", h.flyClient.AppName())

	// Save session
	session := &Session{
		SessionID:    sessionID,
		MachineID:    machine.ID,
		ConnectToken: connectToken,
		ConnectURL:   connectURL,
		Region:       machine.Region,
		CreatedAt:    time.Now(),
	}
	if err := h.store.Save(session); err != nil {
		// Log but don't fail - machine is already running
		fmt.Printf("warning: failed to save session: %v\n", err)
	}

	// Return response
	resp := &CreateSessionResponse{
		SessionID:    sessionID,
		MachineID:    machine.ID,
		ConnectURL:   connectURL,
		ConnectToken: connectToken,
		Headers: map[string]string{
			"fly-force-instance-id": machine.ID,
		},
	}

	writeJSON(w, http.StatusOK, resp)
}

// ListSessions handles GET /v1/sessions.
func (h *Handlers) ListSessions(w http.ResponseWriter, r *http.Request) {
	sessions := h.store.List()
	responses := make([]*SessionResponse, 0, len(sessions))
	for _, s := range sessions {
		responses = append(responses, &SessionResponse{
			SessionID:  s.SessionID,
			MachineID:  s.MachineID,
			ConnectURL: s.ConnectURL,
			Region:     s.Region,
			CreatedAt:  s.CreatedAt,
		})
	}
	writeJSON(w, http.StatusOK, responses)
}

// GetSession handles GET /v1/sessions/{session_id}.
func (h *Handlers) GetSession(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "session_id")
	session, ok := h.store.Get(sessionID)
	if !ok {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}

	resp := &SessionResponse{
		SessionID:  session.SessionID,
		MachineID:  session.MachineID,
		ConnectURL: session.ConnectURL,
		Region:     session.Region,
		CreatedAt:  session.CreatedAt,
	}

	// Optionally fetch live machine state
	if r.URL.Query().Get("live") == "true" {
		machine, err := h.flyClient.GetMachine(session.MachineID)
		if err == nil {
			resp.MachineState = machine.State
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

// StopSession handles POST /v1/sessions/{session_id}/stop.
func (h *Handlers) StopSession(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "session_id")
	session, ok := h.store.Get(sessionID)
	if !ok {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}

	// Check if delete is requested
	deleteAfter := r.URL.Query().Get("delete") == "true"

	// Stop the machine
	if err := h.flyClient.StopMachine(session.MachineID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to stop machine: "+err.Error())
		return
	}

	// Delete if requested
	if deleteAfter {
		if err := h.flyClient.DeleteMachine(session.MachineID, false); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to delete machine: "+err.Error())
			return
		}
		if err := h.store.Delete(sessionID); err != nil {
			fmt.Printf("warning: failed to delete session record: %v\n", err)
		}
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "stopped"})
}

// generateToken generates a random token.
func generateToken(length int) (string, error) {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(bytes), nil
}

// joinCmd joins command parts for environment variable.
func joinCmd(cmd []string) string {
	if len(cmd) == 0 {
		return "/bin/sh"
	}
	// Simple space-joined for now; could use JSON for complex cases
	result := ""
	for i, part := range cmd {
		if i > 0 {
			result += " "
		}
		result += part
	}
	return result
}

// writeJSON writes a JSON response.
func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

// writeError writes an error response.
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, &ErrorResponse{Error: message})
}
