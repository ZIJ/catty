package main

import (
	"fmt"
	"os"

	"github.com/izalutski/catty/internal/cli"
	"github.com/spf13/cobra"
)

var (
	apiAddr string
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "catty",
		Short: "Catty - Remote agent terminal streaming",
		Long:  "Run AI agents remotely on Fly.io machines with local terminal feel",
	}

	// Global flags
	rootCmd.PersistentFlags().StringVar(&apiAddr, "api", "", "API server address (default: http://127.0.0.1:4815)")

	// Run command
	runCmd := &cobra.Command{
		Use:   "run [flags] [-- command args...]",
		Short: "Start a new remote session",
		Long:  "Create a new Fly machine and connect to it interactively",
		RunE:  runRun,
	}
	runCmd.Flags().String("agent", "", "Agent type (claude_code, codex, amp, custom)")
	runCmd.Flags().String("region", "auto", "Fly region (iad, sjc, ams, auto)")
	runCmd.Flags().Int("cpus", 1, "Number of CPUs")
	runCmd.Flags().Int("memory", 1024, "Memory in MB")
	runCmd.Flags().Int("ttl", 7200, "Session TTL in seconds")
	rootCmd.AddCommand(runCmd)

	// List command
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List all sessions",
		Aliases: []string{"ls"},
		RunE:  runList,
	}
	rootCmd.AddCommand(listCmd)

	// Stop command
	stopCmd := &cobra.Command{
		Use:   "stop <session-id>",
		Short: "Stop a session",
		Args:  cobra.ExactArgs(1),
		RunE:  runStop,
	}
	stopCmd.Flags().Bool("delete", false, "Delete the machine after stopping")
	rootCmd.AddCommand(stopCmd)

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func runRun(cmd *cobra.Command, args []string) error {
	agent, _ := cmd.Flags().GetString("agent")
	region, _ := cmd.Flags().GetString("region")
	cpus, _ := cmd.Flags().GetInt("cpus")
	memory, _ := cmd.Flags().GetInt("memory")
	ttl, _ := cmd.Flags().GetInt("ttl")

	// Command comes after "--"
	cmdArgs := args
	if len(cmdArgs) == 0 {
		cmdArgs = []string{"/bin/sh"}
	}

	opts := &cli.RunOptions{
		Agent:    agent,
		Cmd:      cmdArgs,
		Region:   region,
		CPUs:     cpus,
		MemoryMB: memory,
		TTLSec:   ttl,
		APIAddr:  apiAddr,
	}

	return cli.Run(opts)
}

func runList(cmd *cobra.Command, args []string) error {
	opts := &cli.ListOptions{
		APIAddr: apiAddr,
	}
	return cli.List(opts)
}

func runStop(cmd *cobra.Command, args []string) error {
	delete, _ := cmd.Flags().GetBool("delete")

	opts := &cli.StopOptions{
		SessionID: args[0],
		Delete:    delete,
		APIAddr:   apiAddr,
	}
	return cli.Stop(opts)
}
