package cli

import "fmt"

// StopOptions are the options for the stop command.
type StopOptions struct {
	SessionID string
	Delete    bool
	APIAddr   string
}

// Stop stops a session.
func Stop(opts *StopOptions) error {
	client := NewAPIClient(opts.APIAddr)

	if err := client.StopSession(opts.SessionID, opts.Delete); err != nil {
		return fmt.Errorf("failed to stop session: %w", err)
	}

	if opts.Delete {
		fmt.Printf("Session %s stopped and deleted\n", opts.SessionID)
	} else {
		fmt.Printf("Session %s stopped\n", opts.SessionID)
	}

	return nil
}
