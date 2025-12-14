package main

import (
	"fmt"
	"os"

	"github.com/izalutski/catty/internal/cli"
	"github.com/spf13/cobra"
)

var newCmd = &cobra.Command{
	Use:   "new",
	Short: "Start a new remote agent session",
	Long:  "Create a new Fly machine and connect to a remote AI agent interactively",
	RunE:  runNew,
}

func init() {
	newCmd.Flags().String("agent", "claude", "Agent to use: claude or codex")
	newCmd.Flags().Bool("no-upload", false, "Don't upload current directory to the remote session")
}

func runNew(cmd *cobra.Command, args []string) error {
	agent, _ := cmd.Flags().GetString("agent")
	noUpload, _ := cmd.Flags().GetBool("no-upload")

	var cmdArgs []string

	switch agent {
	case "claude":
		cmdArgs = []string{"claude-wrapper"}
	case "codex":
		cmdArgs = []string{"codex"}
	default:
		return fmt.Errorf("unknown agent: %s (must be 'claude' or 'codex')", agent)
	}

	fmt.Fprintf(os.Stderr, "Starting %s session...\n", agent)

	opts := &cli.RunOptions{
		Agent:           agent,
		Cmd:             cmdArgs,
		Region:          "iad",
		CPUs:            1,
		MemoryMB:        1024,
		TTLSec:          7200,
		APIAddr:         apiAddr,
		UploadWorkspace: !noUpload,
	}

	return cli.Run(opts)
}
