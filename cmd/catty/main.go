package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var apiAddr string

func main() {
	rootCmd := &cobra.Command{
		Use:   "catty",
		Short: "Catty - Remote AI agent sessions",
		Long:  "Run AI agents remotely on Fly.io with local terminal feel",
	}

	rootCmd.PersistentFlags().StringVar(&apiAddr, "api", "", "API server address (default: http://127.0.0.1:4815)")

	rootCmd.AddCommand(newCmd)
	rootCmd.AddCommand(listCmd)
	rootCmd.AddCommand(stopCmd)
	rootCmd.AddCommand(stopAllCmd)

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
