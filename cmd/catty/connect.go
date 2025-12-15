package main

import (
	"fmt"
	"os"

	"github.com/izalutski/catty/internal/cli"
	"github.com/spf13/cobra"
)

var connectCmd = &cobra.Command{
	Use:   "connect <label>",
	Short: "Reconnect to an existing session",
	Long:  "Reconnect to an existing session by its label (e.g., brave-tiger-1234)",
	Args:  cobra.ExactArgs(1),
	RunE:  runConnect,
}

func runConnect(cmd *cobra.Command, args []string) error {
	// Check if logged in
	if !cli.IsLoggedIn() {
		fmt.Fprintln(os.Stderr, "Not logged in. Please run 'catty login' first.")
		return fmt.Errorf("authentication required")
	}

	opts := &cli.ConnectOptions{
		SessionLabel: args[0],
		APIAddr:      getAPIAddr(),
	}

	return cli.Connect(opts)
}
