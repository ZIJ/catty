package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

const (
	// DefaultAPIAddr is the default API server address
	DefaultAPIAddr = "https://catty-api.fly.dev"
)

var apiAddr string

func main() {
	rootCmd := &cobra.Command{
		Use:   "catty",
		Short: "Catty - Remote AI agent sessions",
		Long:  "Run Claude Code sessions remotely",
	}

	rootCmd.PersistentFlags().StringVar(&apiAddr, "api", "", fmt.Sprintf("API server address (default: %s)", DefaultAPIAddr))

	rootCmd.AddCommand(newCmd)
	rootCmd.AddCommand(listCmd)
	rootCmd.AddCommand(stopCmd)
	rootCmd.AddCommand(stopAllCmd)
	rootCmd.AddCommand(loginCmd)
	rootCmd.AddCommand(logoutCmd)

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// getAPIAddr returns the API address to use.
func getAPIAddr() string {
	if apiAddr != "" {
		return apiAddr
	}
	if env := os.Getenv("CATTY_API_ADDR"); env != "" {
		return env
	}
	return DefaultAPIAddr
}
