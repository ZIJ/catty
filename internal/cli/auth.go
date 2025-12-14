package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Credentials stores the user's authentication credentials.
type Credentials struct {
	AccessToken string    `json:"access_token"`
	UserID      string    `json:"user_id"`
	Email       string    `json:"email"`
	ExpiresAt   time.Time `json:"expires_at,omitempty"`
}

// credentialsDir returns the directory for storing credentials.
func credentialsDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get home directory: %w", err)
	}
	return filepath.Join(home, ".catty"), nil
}

// credentialsPath returns the path to the credentials file.
func credentialsPath() (string, error) {
	dir, err := credentialsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "credentials.json"), nil
}

// SaveCredentials saves credentials to disk.
func SaveCredentials(creds *Credentials) error {
	dir, err := credentialsDir()
	if err != nil {
		return err
	}

	// Create directory if needed
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create credentials directory: %w", err)
	}

	path, err := credentialsPath()
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal credentials: %w", err)
	}

	// Write with restricted permissions
	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("write credentials: %w", err)
	}

	return nil
}

// LoadCredentials loads credentials from disk.
func LoadCredentials() (*Credentials, error) {
	path, err := credentialsPath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // No credentials stored
		}
		return nil, fmt.Errorf("read credentials: %w", err)
	}

	var creds Credentials
	if err := json.Unmarshal(data, &creds); err != nil {
		return nil, fmt.Errorf("parse credentials: %w", err)
	}

	return &creds, nil
}

// DeleteCredentials removes stored credentials.
func DeleteCredentials() error {
	path, err := credentialsPath()
	if err != nil {
		return err
	}

	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove credentials: %w", err)
	}

	return nil
}

// IsLoggedIn checks if the user has valid credentials.
func IsLoggedIn() bool {
	creds, err := LoadCredentials()
	if err != nil || creds == nil {
		return false
	}
	// Check if token is expired
	if !creds.ExpiresAt.IsZero() && time.Now().After(creds.ExpiresAt) {
		return false
	}
	return creds.AccessToken != ""
}

// GetAccessToken returns the stored access token or empty string.
func GetAccessToken() string {
	creds, err := LoadCredentials()
	if err != nil || creds == nil {
		return ""
	}
	return creds.AccessToken
}
