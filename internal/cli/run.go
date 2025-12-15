package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/coder/websocket"
	"github.com/izalutski/catty/internal/protocol"
)

// RunOptions are the options for the run command.
type RunOptions struct {
	Agent           string
	Cmd             []string
	Region          string
	CPUs            int
	MemoryMB        int
	TTLSec          int
	APIAddr         string
	UploadWorkspace bool
}

// Run starts a new session and connects to it.
func Run(opts *RunOptions) error {
	client := NewAPIClient(opts.APIAddr)

	// Create session
	fmt.Println("Creating session...")
	resp, err := client.CreateSession(&CreateSessionRequest{
		Agent:    opts.Agent,
		Cmd:      opts.Cmd,
		Region:   opts.Region,
		CPUs:     opts.CPUs,
		MemoryMB: opts.MemoryMB,
		TTLSec:   opts.TTLSec,
	})
	if err != nil {
		// Check for quota exceeded error
		if apiErr, ok := err.(*APIError); ok && apiErr.IsQuotaExceeded() {
			return handleQuotaExceeded(apiErr, client)
		}
		return fmt.Errorf("failed to create session: %w", err)
	}

	fmt.Printf("Session created: %s\n", resp.Label)
	fmt.Printf("  Reconnect with: catty connect %s\n", resp.Label)

	// Upload workspace if requested
	if opts.UploadWorkspace {
		fmt.Println("Uploading workspace...")
		machineID := resp.Headers["fly-force-instance-id"]
		uploadURL := buildUploadURL(resp.ConnectURL)
		if err := UploadWorkspace(uploadURL, resp.ConnectToken, machineID); err != nil {
			return fmt.Errorf("failed to upload workspace: %w", err)
		}
		fmt.Println("Workspace uploaded.")
	}

	fmt.Printf("Connecting to %s...\n", resp.ConnectURL)

	// Connect to executor
	return connect(resp)
}

// buildUploadURL converts the WebSocket connect URL to an HTTP upload URL.
func buildUploadURL(connectURL string) string {
	// Convert wss://app.fly.dev/connect to https://app.fly.dev/upload
	url := connectURL
	url = strings.Replace(url, "wss://", "https://", 1)
	url = strings.Replace(url, "ws://", "http://", 1)
	url = strings.Replace(url, "/connect", "/upload", 1)
	return url
}

// connect establishes a WebSocket connection to the executor.
func connect(session *CreateSessionResponse) error {
	// Build headers
	headers := http.Header{}
	for k, v := range session.Headers {
		headers.Set(k, v)
	}
	headers.Set("Authorization", "Bearer "+session.ConnectToken)

	// Connect WebSocket
	ctx := context.Background()
	conn, _, err := websocket.Dial(ctx, session.ConnectURL, &websocket.DialOptions{
		HTTPHeader: headers,
	})
	if err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	// Setup terminal
	term := NewTerminal()
	if !term.IsTerminal() {
		return fmt.Errorf("stdin is not a terminal")
	}

	if err := term.MakeRaw(); err != nil {
		return fmt.Errorf("failed to set raw mode: %w", err)
	}
	defer term.Restore()

	// Send initial resize
	cols, rows, err := term.GetSize()
	if err == nil {
		sendResize(conn, uint16(cols), uint16(rows))
	}

	// Setup signal handlers
	resizeCh := ResizeHandler()
	defer StopResizeHandler(resizeCh)

	// Create channels for coordination
	done := make(chan error, 2)

	// Relay stdin -> WebSocket
	go func() {
		buf := make([]byte, 1024)
		for {
			n, err := os.Stdin.Read(buf)
			if err != nil {
				if err != io.EOF {
					done <- err
				}
				return
			}
			if n > 0 {
				if err := conn.Write(ctx, websocket.MessageBinary, buf[:n]); err != nil {
					done <- err
					return
				}
			}
		}
	}()

	// Relay WebSocket -> stdout + handle control messages
	go func() {
		for {
			msgType, data, err := conn.Read(ctx)
			if err != nil {
				done <- err
				return
			}

			switch msgType {
			case websocket.MessageBinary:
				os.Stdout.Write(data)
			case websocket.MessageText:
				msg, err := protocol.ParseMessage(data)
				if err != nil {
					continue
				}
				switch m := msg.(type) {
				case *protocol.ExitMessage:
					done <- nil
					fmt.Fprintf(os.Stderr, "\r\nProcess exited with code %d\r\n", m.Code)
					return
				case *protocol.ErrorMessage:
					fmt.Fprintf(os.Stderr, "\r\nError: %s\r\n", m.Message)
				case *protocol.PingMessage:
					sendPong(conn)
				}
			}
		}
	}()

	// Handle resize signals
	go func() {
		for range resizeCh {
			cols, rows, err := term.GetSize()
			if err == nil {
				sendResize(conn, uint16(cols), uint16(rows))
			}
		}
	}()

	// Wait for completion
	return <-done
}

func sendResize(conn *websocket.Conn, cols, rows uint16) {
	msg := protocol.NewResizeMessage(cols, rows)
	data, _ := json.Marshal(msg)
	conn.Write(context.Background(), websocket.MessageText, data)
}

func sendPong(conn *websocket.Conn) {
	msg := protocol.NewPongMessage()
	data, _ := json.Marshal(msg)
	conn.Write(context.Background(), websocket.MessageText, data)
}

// handleQuotaExceeded displays a friendly message and opens the upgrade page.
func handleQuotaExceeded(apiErr *APIError, client *APIClient) error {
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Fprintln(os.Stderr, "  Free tier quota exceeded (1M tokens/month)")
	fmt.Fprintln(os.Stderr, "  Upgrade to Pro for unlimited usage.")
	fmt.Fprintln(os.Stderr, "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Fprintln(os.Stderr, "")

	// Call the checkout endpoint to get a Stripe checkout URL
	checkoutURL, err := client.CreateCheckoutSession()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create checkout session: %v\n", err)
		fmt.Fprintln(os.Stderr, "Please visit https://catty.dev to upgrade.")
		return fmt.Errorf("quota exceeded")
	}

	fmt.Fprintln(os.Stderr, "Opening upgrade page in your browser...")
	openBrowser(checkoutURL)

	return fmt.Errorf("quota exceeded")
}

// openBrowser opens the specified URL in the default browser.
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
