package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"runtime"
	"time"

	"github.com/izalutski/catty/internal/cli"
	"github.com/spf13/cobra"
)

var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "Log in to Catty",
	Long:  "Authenticate with Catty to start remote sessions",
	RunE:  runLogin,
}

// DeviceAuthResponse from API
type DeviceAuthResponse struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
}

// DeviceTokenResponse from API
type DeviceTokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	User        *struct {
		ID    string `json:"id"`
		Email string `json:"email"`
	} `json:"user,omitempty"`
	Pending bool   `json:"pending,omitempty"`
	Error   string `json:"error,omitempty"`
}

func runLogin(cmd *cobra.Command, args []string) error {
	// Check if already logged in
	if cli.IsLoggedIn() {
		creds, _ := cli.LoadCredentials()
		fmt.Printf("Already logged in as %s\n", creds.Email)
		fmt.Println("Run 'catty logout' to log out first")
		return nil
	}

	apiAddr := getAPIAddr()

	// Step 1: Start device auth flow
	fmt.Println("Starting login...")

	resp, err := http.Post(apiAddr+"/v1/auth/device", "application/json", bytes.NewReader([]byte("{}")))
	if err != nil {
		return fmt.Errorf("failed to start auth: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("auth failed: %s", string(body))
	}

	var authResp DeviceAuthResponse
	if err := json.NewDecoder(resp.Body).Decode(&authResp); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	// Step 2: Show user the code and URL
	fmt.Println()
	fmt.Println("Your confirmation code:")
	fmt.Println()
	fmt.Printf("    %s\n", authResp.UserCode)
	fmt.Println()
	fmt.Printf("Opening %s\n", authResp.VerificationURIComplete)
	fmt.Println()

	// Try to open browser
	if err := openBrowser(authResp.VerificationURIComplete); err != nil {
		fmt.Println("Please open this URL in your browser:")
		fmt.Printf("  %s\n", authResp.VerificationURIComplete)
	}

	fmt.Println("Waiting for authentication...")

	// Step 3: Poll for token
	interval := time.Duration(authResp.Interval) * time.Second
	if interval < time.Second {
		interval = 5 * time.Second
	}

	deadline := time.Now().Add(time.Duration(authResp.ExpiresIn) * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(interval)

		tokenResp, err := pollToken(apiAddr, authResp.DeviceCode)
		if err != nil {
			return fmt.Errorf("failed to poll token: %w", err)
		}

		if tokenResp.Pending {
			continue // Still waiting for user
		}

		if tokenResp.Error != "" {
			return fmt.Errorf("authentication failed: %s", tokenResp.Error)
		}

		if tokenResp.AccessToken != "" {
			// Save credentials
			creds := &cli.Credentials{
				AccessToken: tokenResp.AccessToken,
			}
			if tokenResp.User != nil {
				creds.UserID = tokenResp.User.ID
				creds.Email = tokenResp.User.Email
			}

			if err := cli.SaveCredentials(creds); err != nil {
				return fmt.Errorf("failed to save credentials: %w", err)
			}

			fmt.Println()
			fmt.Printf("Logged in as %s\n", creds.Email)
			fmt.Println("You can now run 'catty new' to start a session")
			return nil
		}
	}

	return fmt.Errorf("authentication timed out")
}

func pollToken(apiAddr, deviceCode string) (*DeviceTokenResponse, error) {
	reqBody, _ := json.Marshal(map[string]string{"device_code": deviceCode})

	resp, err := http.Post(apiAddr+"/v1/auth/device/token", "application/json", bytes.NewReader(reqBody))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var tokenResp DeviceTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, err
	}

	return &tokenResp, nil
}

func openBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		return fmt.Errorf("unsupported platform")
	}
	return cmd.Start()
}
