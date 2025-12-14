package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// DefaultAPIAddr is the default address of the local API server.
const DefaultAPIAddr = "http://127.0.0.1:4815"

// APIClient is a client for the local TUI API.
type APIClient struct {
	baseURL string
	client  *http.Client
}

// NewAPIClient creates a new API client.
func NewAPIClient(baseURL string) *APIClient {
	if baseURL == "" {
		baseURL = DefaultAPIAddr
	}
	return &APIClient{
		baseURL: baseURL,
		client: &http.Client{
			Timeout: 120 * time.Second, // Long timeout for machine creation
		},
	}
}

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

// SessionInfo is the response for getting session info.
type SessionInfo struct {
	SessionID    string    `json:"session_id"`
	MachineID    string    `json:"machine_id"`
	ConnectURL   string    `json:"connect_url"`
	Region       string    `json:"region"`
	CreatedAt    time.Time `json:"created_at"`
	MachineState string    `json:"machine_state,omitempty"`
}

// CreateSession creates a new session.
func (c *APIClient) CreateSession(req *CreateSessionRequest) (*CreateSessionResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	resp, err := c.client.Post(c.baseURL+"/v1/sessions", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, readError(resp)
	}

	var result CreateSessionResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return &result, nil
}

// ListSessions lists all sessions.
func (c *APIClient) ListSessions() ([]*SessionInfo, error) {
	resp, err := c.client.Get(c.baseURL + "/v1/sessions")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, readError(resp)
	}

	var result []*SessionInfo
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return result, nil
}

// GetSession gets a session by ID.
func (c *APIClient) GetSession(sessionID string, live bool) (*SessionInfo, error) {
	url := c.baseURL + "/v1/sessions/" + sessionID
	if live {
		url += "?live=true"
	}

	resp, err := c.client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, readError(resp)
	}

	var result SessionInfo
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return &result, nil
}

// StopSession stops a session.
func (c *APIClient) StopSession(sessionID string, delete bool) error {
	url := c.baseURL + "/v1/sessions/" + sessionID + "/stop"
	if delete {
		url += "?delete=true"
	}

	resp, err := c.client.Post(url, "", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return readError(resp)
	}

	io.Copy(io.Discard, resp.Body)
	return nil
}

func readError(resp *http.Response) error {
	body, _ := io.ReadAll(resp.Body)
	return fmt.Errorf("API error %d: %s", resp.StatusCode, string(body))
}
