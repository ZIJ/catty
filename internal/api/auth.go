package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"time"
)

// WorkOS API endpoints
const (
	workosBaseURL        = "https://api.workos.com"
	workosDeviceAuthPath = "/user_management/authorize/device"
	workosTokenPath      = "/user_management/authenticate"
	workosUserPath       = "/user_management/users"
)

// DeviceAuthRequest is the request to start device auth flow.
type DeviceAuthRequest struct {
	// No fields needed from client
}

// DeviceAuthResponse is the response with device code and verification URL.
type DeviceAuthResponse struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
}

// DeviceTokenRequest is the request to exchange device code for token.
type DeviceTokenRequest struct {
	DeviceCode string `json:"device_code"`
}

// DeviceTokenResponse is the response with access token.
type DeviceTokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
	User        *User  `json:"user,omitempty"`
	// Pending state
	Pending bool   `json:"pending,omitempty"`
	Error   string `json:"error,omitempty"`
}

// User represents an authenticated user.
type User struct {
	ID        string `json:"id"`
	Email     string `json:"email"`
	FirstName string `json:"first_name,omitempty"`
	LastName  string `json:"last_name,omitempty"`
}

// AuthHandlers contains authentication HTTP handlers.
type AuthHandlers struct {
	clientID string
	apiKey   string

	// In-memory token validation cache
	tokenCache   map[string]*tokenCacheEntry
	tokenCacheMu sync.RWMutex
}

type tokenCacheEntry struct {
	user      *User
	expiresAt time.Time
}

// NewAuthHandlers creates new authentication handlers.
func NewAuthHandlers() (*AuthHandlers, error) {
	clientID := os.Getenv("WORKOS_CLIENT_ID")
	apiKey := os.Getenv("WORKOS_API_KEY")

	if clientID == "" || apiKey == "" {
		return nil, fmt.Errorf("WORKOS_CLIENT_ID and WORKOS_API_KEY environment variables are required")
	}

	return &AuthHandlers{
		clientID:   clientID,
		apiKey:     apiKey,
		tokenCache: make(map[string]*tokenCacheEntry),
	}, nil
}

// StartDeviceAuth handles POST /v1/auth/device - initiates device auth flow.
func (h *AuthHandlers) StartDeviceAuth(w http.ResponseWriter, r *http.Request) {
	// Call WorkOS device authorization endpoint
	reqBody := map[string]string{
		"client_id": h.clientID,
	}
	body, _ := json.Marshal(reqBody)

	req, err := http.NewRequestWithContext(r.Context(), "POST", workosBaseURL+workosDeviceAuthPath, bytes.NewReader(body))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create request: "+err.Error())
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+h.apiKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to call WorkOS: "+err.Error())
		return
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("WorkOS error (%d): %s", resp.StatusCode, string(respBody)))
		return
	}

	var workosResp DeviceAuthResponse
	if err := json.Unmarshal(respBody, &workosResp); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to parse WorkOS response: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, &workosResp)
}

// PollDeviceToken handles POST /v1/auth/device/token - polls for token.
func (h *AuthHandlers) PollDeviceToken(w http.ResponseWriter, r *http.Request) {
	var req DeviceTokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	if req.DeviceCode == "" {
		writeError(w, http.StatusBadRequest, "device_code is required")
		return
	}

	// Call WorkOS token endpoint
	reqBody := map[string]string{
		"client_id":   h.clientID,
		"device_code": req.DeviceCode,
		"grant_type":  "urn:ietf:params:oauth:grant-type:device_code",
	}
	body, _ := json.Marshal(reqBody)

	httpReq, err := http.NewRequestWithContext(r.Context(), "POST", workosBaseURL+workosTokenPath, bytes.NewReader(body))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create request: "+err.Error())
		return
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+h.apiKey)

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to call WorkOS: "+err.Error())
		return
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	// Handle authorization_pending (user hasn't completed auth yet)
	if resp.StatusCode == http.StatusBadRequest {
		var errResp struct {
			Error string `json:"error"`
		}
		json.Unmarshal(respBody, &errResp)
		if errResp.Error == "authorization_pending" {
			writeJSON(w, http.StatusOK, &DeviceTokenResponse{Pending: true})
			return
		}
		writeError(w, http.StatusBadRequest, fmt.Sprintf("WorkOS error: %s", errResp.Error))
		return
	}

	if resp.StatusCode != http.StatusOK {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("WorkOS error (%d): %s", resp.StatusCode, string(respBody)))
		return
	}

	var workosResp struct {
		AccessToken string `json:"access_token"`
		User        *User  `json:"user"`
	}
	if err := json.Unmarshal(respBody, &workosResp); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to parse WorkOS response: "+err.Error())
		return
	}

	// Cache the token for validation
	h.tokenCacheMu.Lock()
	h.tokenCache[workosResp.AccessToken] = &tokenCacheEntry{
		user:      workosResp.User,
		expiresAt: time.Now().Add(24 * time.Hour), // Cache for 24 hours
	}
	h.tokenCacheMu.Unlock()

	writeJSON(w, http.StatusOK, &DeviceTokenResponse{
		AccessToken: workosResp.AccessToken,
		TokenType:   "Bearer",
		User:        workosResp.User,
	})
}

// ValidateToken validates an access token and returns the user.
func (h *AuthHandlers) ValidateToken(token string) (*User, error) {
	// Check cache first
	h.tokenCacheMu.RLock()
	if entry, ok := h.tokenCache[token]; ok && time.Now().Before(entry.expiresAt) {
		h.tokenCacheMu.RUnlock()
		return entry.user, nil
	}
	h.tokenCacheMu.RUnlock()

	// Validate with WorkOS by fetching user info
	req, err := http.NewRequest("GET", workosBaseURL+"/user_management/users/me", nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call WorkOS: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("invalid token")
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("WorkOS error (%d): %s", resp.StatusCode, string(body))
	}

	var user User
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	// Cache the result
	h.tokenCacheMu.Lock()
	h.tokenCache[token] = &tokenCacheEntry{
		user:      &user,
		expiresAt: time.Now().Add(24 * time.Hour),
	}
	h.tokenCacheMu.Unlock()

	return &user, nil
}

// AuthMiddleware returns middleware that requires authentication.
func (h *AuthHandlers) AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := extractBearerToken(r)
		if token == "" {
			writeError(w, http.StatusUnauthorized, "missing authorization header")
			return
		}

		user, err := h.ValidateToken(token)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "invalid token: "+err.Error())
			return
		}

		// Add user to context
		ctx := r.Context()
		ctx = WithUser(ctx, user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// extractBearerToken extracts the Bearer token from Authorization header.
func extractBearerToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if len(auth) > 7 && auth[:7] == "Bearer " {
		return auth[7:]
	}
	return ""
}

// Context key for user
type contextKey string

const userContextKey contextKey = "user"

// WithUser adds user to context.
func WithUser(ctx context.Context, user *User) context.Context {
	return context.WithValue(ctx, userContextKey, user)
}

// UserFromContext gets user from context.
func UserFromContext(ctx context.Context) *User {
	user, _ := ctx.Value(userContextKey).(*User)
	return user
}
