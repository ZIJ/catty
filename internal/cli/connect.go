package cli

import (
	"fmt"
	"net/http"
)

// ConnectOptions are the options for the connect command.
type ConnectOptions struct {
	SessionLabel string
	APIAddr      string
}

// Connect reconnects to an existing session by label or ID.
func Connect(opts *ConnectOptions) error {
	client := NewAPIClient(opts.APIAddr)

	// Get session info (with connect token)
	fmt.Printf("Looking up session %s...\n", opts.SessionLabel)
	session, err := client.GetSession(opts.SessionLabel, true)
	if err != nil {
		return fmt.Errorf("failed to get session: %w", err)
	}

	// Check status
	if session.Status == "stopped" {
		return fmt.Errorf("session %s is stopped", session.Label)
	}

	// Check machine state if available
	if session.MachineState != "" && session.MachineState != "started" {
		return fmt.Errorf("machine is not running (state: %s)", session.MachineState)
	}

	fmt.Printf("Reconnecting to %s...\n", session.Label)

	// Build a CreateSessionResponse-like struct for connect()
	connectInfo := &CreateSessionResponse{
		SessionID:    session.SessionID,
		Label:        session.Label,
		MachineID:    session.MachineID,
		ConnectURL:   session.ConnectURL,
		ConnectToken: session.ConnectToken,
		Headers: map[string]string{
			"fly-force-instance-id": session.MachineID,
		},
	}

	return connect(connectInfo)
}

// ConnectWithHeaders connects to a session using provided connection details.
func ConnectWithHeaders(connectURL, connectToken, machineID string) error {
	headers := http.Header{}
	headers.Set("fly-force-instance-id", machineID)
	headers.Set("Authorization", "Bearer "+connectToken)

	connectInfo := &CreateSessionResponse{
		ConnectURL:   connectURL,
		ConnectToken: connectToken,
		Headers: map[string]string{
			"fly-force-instance-id": machineID,
		},
	}

	return connect(connectInfo)
}
