package api

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/izalutski/catty/internal/db"
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
	Label        string            `json:"label"`
	MachineID    string            `json:"machine_id"`
	ConnectURL   string            `json:"connect_url"`
	ConnectToken string            `json:"connect_token"`
	Headers      map[string]string `json:"headers"`
}

// SessionResponse is the response for getting a session.
type SessionResponse struct {
	SessionID    string    `json:"session_id"`
	Label        string    `json:"label"`
	MachineID    string    `json:"machine_id"`
	ConnectURL   string    `json:"connect_url"`
	ConnectToken string    `json:"connect_token,omitempty"`
	Region       string    `json:"region"`
	Status       string    `json:"status"`
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
	db        *db.Client
}

// NewHandlers creates new API handlers.
func NewHandlers(flyClient *fly.Client, dbClient *db.Client) *Handlers {
	return &Handlers{
		flyClient: flyClient,
		db:        dbClient,
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

	// Get authenticated user from context
	authUser := UserFromContext(r.Context())
	if authUser == nil {
		writeError(w, http.StatusUnauthorized, "user not found in context")
		return
	}

	// Get or create user in database
	dbUser, err := h.db.GetOrCreateUser(authUser.ID, authUser.Email)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get/create user: "+err.Error())
		return
	}

	// Generate connect token and label
	connectToken, err := generateToken(32)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate token: "+err.Error())
		return
	}
	label := db.GenerateLabel()

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
				"label":   label,
				"owner":   authUser.Email,
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
	// Use custom domain if set, otherwise fall back to fly.dev
	execHost := os.Getenv("CATTY_EXEC_HOST")
	if execHost == "" {
		execHost = fmt.Sprintf("%s.fly.dev", h.flyClient.AppName())
	}
	connectURL := fmt.Sprintf("wss://%s/connect", execHost)

	// Save session to database
	session := &db.Session{
		UserID:       dbUser.ID,
		MachineID:    machine.ID,
		Label:        label,
		ConnectToken: connectToken,
		ConnectURL:   connectURL,
		Region:       machine.Region,
		Status:       "running",
	}
	savedSession, err := h.db.CreateSession(session)
	if err != nil {
		// Log but don't fail - machine is already running
		fmt.Printf("warning: failed to save session: %v\n", err)
	}

	// Return response
	resp := &CreateSessionResponse{
		SessionID:    savedSession.ID,
		Label:        label,
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
	// Get authenticated user from context
	authUser := UserFromContext(r.Context())
	if authUser == nil {
		writeError(w, http.StatusUnauthorized, "user not found in context")
		return
	}

	// Get user from database
	dbUser, err := h.db.GetUserByWorkosID(authUser.ID)
	if err != nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}

	// List user's sessions
	sessions, err := h.db.ListUserSessions(dbUser.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list sessions: "+err.Error())
		return
	}

	responses := make([]*SessionResponse, 0, len(sessions))
	for _, s := range sessions {
		responses = append(responses, &SessionResponse{
			SessionID:  s.ID,
			Label:      s.Label,
			MachineID:  s.MachineID,
			ConnectURL: s.ConnectURL,
			Region:     s.Region,
			Status:     s.Status,
			CreatedAt:  s.CreatedAt,
		})
	}
	writeJSON(w, http.StatusOK, responses)
}

// GetSession handles GET /v1/sessions/{session_id}.
// session_id can be either the UUID or the label.
func (h *Handlers) GetSession(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "session_id")

	// Get authenticated user from context
	authUser := UserFromContext(r.Context())
	if authUser == nil {
		writeError(w, http.StatusUnauthorized, "user not found in context")
		return
	}

	// Get user from database
	dbUser, err := h.db.GetUserByWorkosID(authUser.ID)
	if err != nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}

	// Try to get session by ID first, then by label
	session, err := h.db.GetSessionByID(sessionID)
	if err != nil {
		session, err = h.db.GetSessionByLabel(dbUser.ID, sessionID)
		if err != nil {
			writeError(w, http.StatusNotFound, "session not found")
			return
		}
	}

	// Verify session belongs to user
	if session.UserID != dbUser.ID {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}

	resp := &SessionResponse{
		SessionID:    session.ID,
		Label:        session.Label,
		MachineID:    session.MachineID,
		ConnectURL:   session.ConnectURL,
		ConnectToken: session.ConnectToken,
		Region:       session.Region,
		Status:       session.Status,
		CreatedAt:    session.CreatedAt,
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
// session_id can be either the UUID or the label.
func (h *Handlers) StopSession(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "session_id")

	// Get authenticated user from context
	authUser := UserFromContext(r.Context())
	if authUser == nil {
		writeError(w, http.StatusUnauthorized, "user not found in context")
		return
	}

	// Get user from database
	dbUser, err := h.db.GetUserByWorkosID(authUser.ID)
	if err != nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}

	// Try to get session by ID first, then by label
	session, err := h.db.GetSessionByID(sessionID)
	if err != nil {
		session, err = h.db.GetSessionByLabel(dbUser.ID, sessionID)
		if err != nil {
			writeError(w, http.StatusNotFound, "session not found")
			return
		}
	}

	// Verify session belongs to user
	if session.UserID != dbUser.ID {
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
		if err := h.db.DeleteSession(session.ID); err != nil {
			fmt.Printf("warning: failed to delete session record: %v\n", err)
		}
	} else {
		// Just update status
		if err := h.db.UpdateSessionStatus(session.ID, "stopped"); err != nil {
			fmt.Printf("warning: failed to update session status: %v\n", err)
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
