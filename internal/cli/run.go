package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/coder/websocket"
	"github.com/izalutski/catty/internal/protocol"
)

// RunOptions are the options for the run command.
type RunOptions struct {
	Agent    string
	Cmd      []string
	Region   string
	CPUs     int
	MemoryMB int
	TTLSec   int
	APIAddr  string
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
		return fmt.Errorf("failed to create session: %w", err)
	}

	fmt.Printf("Session created: %s (machine: %s)\n", resp.SessionID, resp.MachineID)
	fmt.Printf("Connecting to %s...\n", resp.ConnectURL)

	// Connect to executor
	return connect(resp)
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
