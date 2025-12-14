package executor

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"sync"
	"syscall"
	"time"

	"github.com/coder/websocket"
	"github.com/izalutski/catty/internal/protocol"
)

const (
	// pingInterval is how often to send pings.
	pingInterval = 25 * time.Second

	// readBufferSize is the buffer size for reading from PTY.
	readBufferSize = 32 * 1024
)

// Relay handles bidirectional streaming between WebSocket and PTY.
type Relay struct {
	conn *websocket.Conn
	pty  *PTY
	mu   sync.Mutex
}

// NewRelay creates a new relay.
func NewRelay(conn *websocket.Conn, pty *PTY) *Relay {
	return &Relay{
		conn: conn,
		pty:  pty,
	}
}

// Run starts the relay and blocks until the connection closes or the process exits.
func (r *Relay) Run(ctx context.Context) error {
	// Send ready message
	if err := r.sendControl(protocol.NewReadyMessage()); err != nil {
		return err
	}

	// Start goroutines
	errCh := make(chan error, 3)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// PTY -> WebSocket
	go func() {
		errCh <- r.relayPTYToWS(ctx)
	}()

	// WebSocket -> PTY
	go func() {
		errCh <- r.relayWSToPTY(ctx)
	}()

	// Ping loop
	go func() {
		errCh <- r.pingLoop(ctx)
	}()

	// Wait for process exit or error
	select {
	case <-r.pty.ExitCh():
		// Process exited, send exit message
		exitCode := r.pty.ExitCode()
		r.sendControl(protocol.NewExitMessage(exitCode, nil))
		return nil
	case err := <-errCh:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// relayPTYToWS reads from PTY and writes to WebSocket.
func (r *Relay) relayPTYToWS(ctx context.Context) error {
	buf := make([]byte, readBufferSize)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		n, err := r.pty.Read(buf)
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}

		if n > 0 {
			if err := r.sendBinary(ctx, buf[:n]); err != nil {
				return err
			}
		}
	}
}

// relayWSToPTY reads from WebSocket and writes to PTY.
func (r *Relay) relayWSToPTY(ctx context.Context) error {
	for {
		msgType, data, err := r.conn.Read(ctx)
		if err != nil {
			return err
		}

		switch msgType {
		case websocket.MessageBinary:
			// Raw input bytes
			if _, err := r.pty.Write(data); err != nil {
				return err
			}
		case websocket.MessageText:
			// Control message
			if err := r.handleControl(data); err != nil {
				slog.Warn("control message error", "error", err)
			}
		}
	}
}

// handleControl processes a control message.
func (r *Relay) handleControl(data []byte) error {
	msg, err := protocol.ParseMessage(data)
	if err != nil {
		return err
	}

	switch m := msg.(type) {
	case *protocol.ResizeMessage:
		return r.pty.Resize(m.Cols, m.Rows)
	case *protocol.SignalMessage:
		return r.handleSignal(m.Name)
	case *protocol.PingMessage:
		return r.sendControl(protocol.NewPongMessage())
	}

	return nil
}

// handleSignal sends a signal to the process.
func (r *Relay) handleSignal(name string) error {
	var sig syscall.Signal
	switch name {
	case "SIGINT":
		sig = syscall.SIGINT
	case "SIGTERM":
		sig = syscall.SIGTERM
	case "SIGKILL":
		sig = syscall.SIGKILL
	case "SIGHUP":
		sig = syscall.SIGHUP
	default:
		return nil
	}
	return r.pty.Signal(sig)
}

// pingLoop sends periodic pings.
func (r *Relay) pingLoop(ctx context.Context) error {
	ticker := time.NewTicker(pingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := r.sendControl(protocol.NewPingMessage()); err != nil {
				return err
			}
		}
	}
}

// sendBinary sends binary data over WebSocket.
func (r *Relay) sendBinary(ctx context.Context, data []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.conn.Write(ctx, websocket.MessageBinary, data)
}

// sendControl sends a control message over WebSocket.
func (r *Relay) sendControl(msg any) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.conn.Write(context.Background(), websocket.MessageText, data)
}
