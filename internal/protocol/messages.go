// Package protocol defines WebSocket message types for the TUI streaming protocol.
package protocol

import "encoding/json"

// Message types for text frame JSON messages
const (
	TypeResize = "resize"
	TypeSignal = "signal"
	TypePing   = "ping"
	TypePong   = "pong"
	TypeReady  = "ready"
	TypeExit   = "exit"
	TypeError  = "error"
)

// BaseMessage is used to determine the message type before full parsing.
type BaseMessage struct {
	Type string `json:"type"`
}

// ResizeMessage is sent from client to server to resize the PTY.
type ResizeMessage struct {
	Type string `json:"type"` // "resize"
	Cols uint16 `json:"cols"`
	Rows uint16 `json:"rows"`
}

// SignalMessage is sent from client to server to send a signal to the process.
type SignalMessage struct {
	Type string `json:"type"` // "signal"
	Name string `json:"name"` // e.g., "SIGINT", "SIGTERM"
}

// PingMessage is sent to check connection liveness.
type PingMessage struct {
	Type string `json:"type"` // "ping"
}

// PongMessage is the response to a ping.
type PongMessage struct {
	Type string `json:"type"` // "pong"
}

// ReadyMessage is sent from server to client when the PTY is ready.
type ReadyMessage struct {
	Type string `json:"type"` // "ready"
}

// ExitMessage is sent from server to client when the process exits.
type ExitMessage struct {
	Type   string  `json:"type"`   // "exit"
	Code   int     `json:"code"`   // Exit code
	Signal *string `json:"signal"` // Signal name if killed by signal, null otherwise
}

// ErrorMessage is sent from server to client on errors.
type ErrorMessage struct {
	Type    string `json:"type"`    // "error"
	Message string `json:"message"` // Error description
}

// ParseMessage parses a JSON message and returns the appropriate type.
func ParseMessage(data []byte) (any, error) {
	var base BaseMessage
	if err := json.Unmarshal(data, &base); err != nil {
		return nil, err
	}

	switch base.Type {
	case TypeResize:
		var msg ResizeMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			return nil, err
		}
		return &msg, nil
	case TypeSignal:
		var msg SignalMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			return nil, err
		}
		return &msg, nil
	case TypePing:
		return &PingMessage{Type: TypePing}, nil
	case TypePong:
		return &PongMessage{Type: TypePong}, nil
	case TypeReady:
		return &ReadyMessage{Type: TypeReady}, nil
	case TypeExit:
		var msg ExitMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			return nil, err
		}
		return &msg, nil
	case TypeError:
		var msg ErrorMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			return nil, err
		}
		return &msg, nil
	default:
		return &base, nil
	}
}

// NewResizeMessage creates a new resize message.
func NewResizeMessage(cols, rows uint16) *ResizeMessage {
	return &ResizeMessage{Type: TypeResize, Cols: cols, Rows: rows}
}

// NewSignalMessage creates a new signal message.
func NewSignalMessage(name string) *SignalMessage {
	return &SignalMessage{Type: TypeSignal, Name: name}
}

// NewPingMessage creates a new ping message.
func NewPingMessage() *PingMessage {
	return &PingMessage{Type: TypePing}
}

// NewPongMessage creates a new pong message.
func NewPongMessage() *PongMessage {
	return &PongMessage{Type: TypePong}
}

// NewReadyMessage creates a new ready message.
func NewReadyMessage() *ReadyMessage {
	return &ReadyMessage{Type: TypeReady}
}

// NewExitMessage creates a new exit message.
func NewExitMessage(code int, signal *string) *ExitMessage {
	return &ExitMessage{Type: TypeExit, Code: code, Signal: signal}
}

// NewErrorMessage creates a new error message.
func NewErrorMessage(message string) *ErrorMessage {
	return &ErrorMessage{Type: TypeError, Message: message}
}
