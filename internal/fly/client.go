// Package fly provides a client for the Fly.io Machines API.
package fly

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

const (
	// DefaultBaseURL is the public Fly Machines API endpoint.
	DefaultBaseURL = "https://api.machines.dev"

	// InternalBaseURL is used when running inside Fly's network.
	InternalBaseURL = "http://_api.internal:4280"
)

// Client is an HTTP client for the Fly Machines API.
type Client struct {
	baseURL    string
	appName    string
	token      string
	httpClient *http.Client
}

// NewClient creates a new Fly Machines API client.
// It reads configuration from environment variables:
//   - FLY_API_TOKEN: API authentication token (required)
//   - FLY_MACHINES_API_BASE: Base URL (defaults to https://api.machines.dev)
//   - CATTY_EXEC_APP: Fly app name for executor machines (required)
func NewClient() (*Client, error) {
	token := os.Getenv("FLY_API_TOKEN")
	if token == "" {
		return nil, fmt.Errorf("FLY_API_TOKEN environment variable is required")
	}

	appName := os.Getenv("CATTY_EXEC_APP")
	if appName == "" {
		return nil, fmt.Errorf("CATTY_EXEC_APP environment variable is required")
	}

	baseURL := os.Getenv("FLY_MACHINES_API_BASE")
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}

	return &Client{
		baseURL: baseURL,
		appName: appName,
		token:   token,
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}, nil
}

// NewClientWithConfig creates a client with explicit configuration.
func NewClientWithConfig(baseURL, appName, token string) *Client {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	return &Client{
		baseURL: baseURL,
		appName: appName,
		token:   token,
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

// AppName returns the configured app name.
func (c *Client) AppName() string {
	return c.appName
}

// do performs an HTTP request with authentication.
func (c *Client) do(method, path string, body any) (*http.Response, error) {
	url := c.baseURL + path

	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequest(method, url, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	return c.httpClient.Do(req)
}

// readError reads an error response body.
func readError(resp *http.Response) error {
	body, _ := io.ReadAll(resp.Body)
	return fmt.Errorf("API error %d: %s", resp.StatusCode, string(body))
}
