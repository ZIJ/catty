package main

import (
	"github.com/izalutski/catty/internal/cli"
	"github.com/spf13/cobra"
)

var listCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "List all sessions",
	RunE:    runList,
}

func runList(cmd *cobra.Command, args []string) error {
	opts := &cli.ListOptions{
		APIAddr: getAPIAddr(),
	}
	return cli.List(opts)
}
