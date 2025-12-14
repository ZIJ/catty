package main

import (
	"github.com/izalutski/catty/internal/cli"
	"github.com/spf13/cobra"
)

var stopCmd = &cobra.Command{
	Use:   "stop <session-id>",
	Short: "Stop a session",
	Args:  cobra.ExactArgs(1),
	RunE:  runStop,
}

func init() {
	stopCmd.Flags().Bool("delete", false, "Delete the machine after stopping")
}

func runStop(cmd *cobra.Command, args []string) error {
	deleteMachine, _ := cmd.Flags().GetBool("delete")

	opts := &cli.StopOptions{
		SessionID: args[0],
		Delete:    deleteMachine,
		APIAddr:   apiAddr,
	}
	return cli.Stop(opts)
}
