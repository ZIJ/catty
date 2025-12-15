package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"github.com/izalutski/catty/internal/db"
)

const (
	AnthropicAPIBase = "https://api.anthropic.com"
)

// Proxy is an Anthropic API proxy that counts tokens.
type Proxy struct {
	db           *db.Client
	anthropicKey string
	reverseProxy *httputil.ReverseProxy
	logger       *slog.Logger
}

// NewProxy creates a new Anthropic API proxy.
func NewProxy(dbClient *db.Client, anthropicKey string, logger *slog.Logger) (*Proxy, error) {
	target, err := url.Parse(AnthropicAPIBase)
	if err != nil {
		return nil, fmt.Errorf("parse anthropic URL: %w", err)
	}

	proxy := &Proxy{
		db:           dbClient,
		anthropicKey: anthropicKey,
		logger:       logger,
	}

	// Create reverse proxy
	rp := httputil.NewSingleHostReverseProxy(target)
	rp.Director = proxy.director
	rp.ModifyResponse = proxy.modifyResponse
	proxy.reverseProxy = rp

	return proxy, nil
}

// director modifies the outgoing request to Anthropic.
func (p *Proxy) director(req *http.Request) {
	target, _ := url.Parse(AnthropicAPIBase)
	req.URL.Scheme = target.Scheme
	req.URL.Host = target.Host
	req.Host = target.Host

	// Replace the API key with ours
	req.Header.Set("x-api-key", p.anthropicKey)

	// Remove our custom auth header
	req.Header.Del("Authorization")
}

// modifyResponse processes the response from Anthropic to count tokens.
func (p *Proxy) modifyResponse(resp *http.Response) error {
	// Only process successful message responses
	if resp.StatusCode != http.StatusOK {
		return nil
	}

	// Check if this is a messages endpoint
	if !strings.Contains(resp.Request.URL.Path, "/messages") {
		return nil
	}

	// Get session info from context (set in ServeHTTP)
	session := SessionFromContext(resp.Request.Context())
	if session == nil {
		return nil
	}

	// Check if this is a streaming response
	contentType := resp.Header.Get("Content-Type")
	if strings.Contains(contentType, "text/event-stream") {
		// Wrap the response body to intercept SSE events
		resp.Body = &sseUsageReader{
			reader:  resp.Body,
			proxy:   p,
			session: session,
		}
		return nil
	}

	// Non-streaming: read and parse JSON response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		p.logger.Error("failed to read response body", "error", err)
		return nil
	}
	resp.Body.Close()

	// Parse usage from response
	var messageResp struct {
		Usage struct {
			InputTokens  int64 `json:"input_tokens"`
			OutputTokens int64 `json:"output_tokens"`
		} `json:"usage"`
	}

	if err := json.Unmarshal(body, &messageResp); err != nil {
		p.logger.Debug("failed to parse response for usage", "error", err)
	} else if messageResp.Usage.InputTokens > 0 || messageResp.Usage.OutputTokens > 0 {
		if err := p.db.RecordUsage(session.UserID, session.ID, messageResp.Usage.InputTokens, messageResp.Usage.OutputTokens); err != nil {
			p.logger.Error("failed to record usage", "error", err, "session_id", session.ID)
		} else {
			p.logger.Info("recorded usage",
				"session_id", session.ID,
				"input_tokens", messageResp.Usage.InputTokens,
				"output_tokens", messageResp.Usage.OutputTokens)
		}
	}

	// Restore the response body
	resp.Body = io.NopCloser(bytes.NewReader(body))
	resp.ContentLength = int64(len(body))

	return nil
}

// sseUsageReader wraps an SSE response body to extract usage information.
type sseUsageReader struct {
	reader       io.ReadCloser
	proxy        *Proxy
	session      *db.Session
	buffer       []byte
	inputTokens  int64
	outputTokens int64
}

func (r *sseUsageReader) Read(p []byte) (n int, err error) {
	n, err = r.reader.Read(p)
	if n > 0 {
		// Append to buffer for parsing
		r.buffer = append(r.buffer, p[:n]...)
		r.parseSSEEvents()
	}
	// Record usage on EOF (in case Close() isn't called)
	if err == io.EOF {
		r.recordUsageOnce()
	}
	return n, err
}

// recorded tracks if we've already recorded usage for this stream
func (r *sseUsageReader) recordUsageOnce() {
	if r.inputTokens > 0 || r.outputTokens > 0 {
		if err := r.proxy.db.RecordUsage(r.session.UserID, r.session.ID, r.inputTokens, r.outputTokens); err != nil {
			r.proxy.logger.Error("failed to record usage", "error", err, "session_id", r.session.ID)
		} else {
			r.proxy.logger.Info("recorded usage",
				"session_id", r.session.ID,
				"input_tokens", r.inputTokens,
				"output_tokens", r.outputTokens)
		}
		// Clear to prevent duplicate recording
		r.inputTokens = 0
		r.outputTokens = 0
	}
}

func (r *sseUsageReader) Close() error {
	r.recordUsageOnce()
	return r.reader.Close()
}

// parseSSEEvents extracts usage from SSE events in the buffer.
func (r *sseUsageReader) parseSSEEvents() {
	// SSE format: "event: type\ndata: {json}\n\n" or just "data: {json}\n\n"
	for {
		// Look for double newline (end of SSE event)
		idx := bytes.Index(r.buffer, []byte("\n\n"))
		if idx == -1 {
			// Also try \r\n\r\n
			idx = bytes.Index(r.buffer, []byte("\r\n\r\n"))
			if idx == -1 {
				break
			}
			// Adjust for \r\n\r\n
			event := r.buffer[:idx]
			r.buffer = r.buffer[idx+4:]
			r.parseSSEEvent(event)
			continue
		}

		event := r.buffer[:idx]
		r.buffer = r.buffer[idx+2:]
		r.parseSSEEvent(event)
	}
}

// parseSSEEvent parses a single SSE event block
func (r *sseUsageReader) parseSSEEvent(event []byte) {
	// Split by newlines and find data line
	lines := bytes.Split(event, []byte("\n"))
	for _, line := range lines {
		// Handle \r if present
		line = bytes.TrimSuffix(line, []byte("\r"))

		if bytes.HasPrefix(line, []byte("data: ")) {
			data := line[6:]
			// Skip [DONE] marker
			if bytes.Equal(data, []byte("[DONE]")) {
				continue
			}
			r.parseSSEData(data)
		}
	}
}

// parseSSEData extracts usage from an SSE data payload.
func (r *sseUsageReader) parseSSEData(data []byte) {
	// First, just get the type
	var typeOnly struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &typeOnly); err != nil {
		return
	}

	switch typeOnly.Type {
	case "message_start":
		var messageStart struct {
			Message struct {
				Usage struct {
					InputTokens int64 `json:"input_tokens"`
				} `json:"usage"`
			} `json:"message"`
		}
		if err := json.Unmarshal(data, &messageStart); err == nil {
			r.inputTokens = messageStart.Message.Usage.InputTokens
		}

	case "message_delta":
		var messageDelta struct {
			Usage struct {
				OutputTokens int64 `json:"output_tokens"`
			} `json:"usage"`
		}
		if err := json.Unmarshal(data, &messageDelta); err == nil && messageDelta.Usage.OutputTokens > 0 {
			r.outputTokens = messageDelta.Usage.OutputTokens
		}
	}
}

type contextKey string

const sessionContextKey contextKey = "session"

// ContextWithSession adds a session to the context.
func ContextWithSession(ctx context.Context, session *db.Session) context.Context {
	return context.WithValue(ctx, sessionContextKey, session)
}

// SessionFromContext retrieves a session from the context.
func SessionFromContext(ctx context.Context) *db.Session {
	if session, ok := ctx.Value(sessionContextKey).(*db.Session); ok {
		return session
	}
	return nil
}

// ServeHTTP handles incoming proxy requests.
// Expected path format: /s/{label}/v1/messages
func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Extract session label from path: /s/{label}/v1/...
	path := r.URL.Path
	if !strings.HasPrefix(path, "/s/") {
		p.logger.Warn("invalid path format", "path", path)
		http.Error(w, `{"error":"invalid path format, expected /s/{label}/..."}`, http.StatusBadRequest)
		return
	}

	// Parse label from path
	rest := strings.TrimPrefix(path, "/s/")
	parts := strings.SplitN(rest, "/", 2)
	if len(parts) < 2 {
		p.logger.Warn("invalid path format", "path", path)
		http.Error(w, `{"error":"invalid path format, expected /s/{label}/v1/..."}`, http.StatusBadRequest)
		return
	}
	label := parts[0]
	apiPath := "/" + parts[1] // e.g., /v1/messages

	p.logger.Info("received request", "label", label, "api_path", apiPath)

	// Look up session by label
	session, err := p.db.GetSessionByLabelAnyUser(label)
	if err != nil {
		p.logger.Warn("session not found", "error", err, "label", label)
		http.Error(w, `{"error":"session not found"}`, http.StatusUnauthorized)
		return
	}

	// Check quota
	allowed, remaining, err := p.db.CheckQuota(session.UserID)
	if err != nil {
		p.logger.Error("failed to check quota", "error", err)
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}

	if !allowed {
		p.logger.Warn("quota exceeded", "user_id", session.UserID)
		http.Error(w, `{"error":"quota exceeded - upgrade to pro for unlimited usage"}`, http.StatusPaymentRequired)
		return
	}

	p.logger.Debug("proxying request",
		"session_id", session.ID,
		"user_id", session.UserID,
		"remaining_tokens", remaining,
		"api_path", apiPath)

	// Store session in context for modifyResponse
	r = r.WithContext(ContextWithSession(r.Context(), session))

	// Rewrite URL path to remove session prefix before forwarding
	r.URL.Path = apiPath

	// Forward to Anthropic
	p.reverseProxy.ServeHTTP(w, r)
}
