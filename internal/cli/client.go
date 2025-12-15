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
	baseURL   string
	client    *http.Client
	authToken string
}

// NewAPIClient creates a new API client.
func NewAPIClient(baseURL string) *APIClient {
	if baseURL == "" {
		baseURL = DefaultAPIAddr
	}
	return &APIClient{
		baseURL:   baseURL,
		authToken: GetAccessToken(), // Load from stored credentials
		client: &http.Client{
			Timeout: 120 * time.Second, // Long timeout for machine creation
		},
	}
}

// doRequest performs an HTTP request with auth headers.
func (c *APIClient) doRequest(method, url string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.authToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.authToken)
	}
	return c.client.Do(req)
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
	Label        string            `json:"label"`
	MachineID    string            `json:"machine_id"`
	ConnectURL   string            `json:"connect_url"`
	ConnectToken string            `json:"connect_token"`
	Headers      map[string]string `json:"headers"`
}

// SessionInfo is the response for getting session info.
type SessionInfo struct {
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

// CreateSession creates a new session.
func (c *APIClient) CreateSession(req *CreateSessionRequest) (*CreateSessionResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	resp, err := c.doRequest("POST", c.baseURL+"/v1/sessions", bytes.NewReader(body))
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
	resp, err := c.doRequest("GET", c.baseURL+"/v1/sessions", nil)
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

	resp, err := c.doRequest("GET", url, nil)
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

	resp, err := c.doRequest("POST", url, nil)
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

// APIError represents an error response from the API.
type APIError struct {
	StatusCode int
	ErrorCode  string
	Message    string
	UpgradeURL string
}

func (e *APIError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return fmt.Sprintf("API error %d: %s", e.StatusCode, e.ErrorCode)
}

// IsQuotaExceeded returns true if this is a quota exceeded error.
func (e *APIError) IsQuotaExceeded() bool {
	return e.StatusCode == http.StatusPaymentRequired && e.ErrorCode == "quota_exceeded"
}

// CreateCheckoutSession creates a Stripe checkout session and returns the URL.
func (c *APIClient) CreateCheckoutSession() (string, error) {
	resp, err := c.doRequest("POST", c.baseURL+"/v1/billing/checkout", nil)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("checkout failed: %s", string(body))
	}

	var result struct {
		CheckoutURL string `json:"checkout_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}

	return result.CheckoutURL, nil
}

func readError(resp *http.Response) error {
	body, _ := io.ReadAll(resp.Body)

	// Try to parse as structured error
	var errResp struct {
		Error      string `json:"error"`
		Message    string `json:"message"`
		UpgradeURL string `json:"upgrade_url"`
	}
	if err := json.Unmarshal(body, &errResp); err == nil && errResp.Error != "" {
		return &APIError{
			StatusCode: resp.StatusCode,
			ErrorCode:  errResp.Error,
			Message:    errResp.Message,
			UpgradeURL: errResp.UpgradeURL,
		}
	}

	// Fall back to generic error
	return &APIError{
		StatusCode: resp.StatusCode,
		Message:    string(body),
	}
}
