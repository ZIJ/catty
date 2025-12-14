package main

import (
	"fmt"

	"github.com/izalutski/catty/internal/cli"
	"github.com/spf13/cobra"
)

var stopAllCmd = &cobra.Command{
	Use:    "stop-all-sessions-dangerously",
	Short:  "Stop and delete ALL sessions",
	Hidden: true,
	RunE:   runStopAll,
}

func init() {
	stopAllCmd.Flags().Bool("yes-i-mean-it", false, "Confirm you want to stop all sessions")
}

func runStopAll(cmd *cobra.Command, args []string) error {
	confirmed, _ := cmd.Flags().GetBool("yes-i-mean-it")
	if !confirmed {
		return fmt.Errorf("must pass --yes-i-mean-it to confirm")
	}

	client := cli.NewAPIClient(apiAddr)

	sessions, err := client.ListSessions()
	if err != nil {
		return fmt.Errorf("failed to list sessions: %w", err)
	}

	if len(sessions) == 0 {
		fmt.Println("No sessions to stop")
		return nil
	}

	fmt.Printf("Stopping %d sessions...\n", len(sessions))

	for _, s := range sessions {
		fmt.Printf("  Stopping %s... ", s.SessionID)
		if err := client.StopSession(s.SessionID, true); err != nil {
			fmt.Printf("ERROR: %v\n", err)
		} else {
			fmt.Println("done")
		}
	}

	return nil
}
