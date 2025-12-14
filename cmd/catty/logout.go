package main

import (
	"fmt"

	"github.com/izalutski/catty/internal/cli"
	"github.com/spf13/cobra"
)

var logoutCmd = &cobra.Command{
	Use:   "logout",
	Short: "Log out of Catty",
	Long:  "Remove stored credentials and log out",
	RunE:  runLogout,
}

func runLogout(cmd *cobra.Command, args []string) error {
	if !cli.IsLoggedIn() {
		fmt.Println("Not logged in")
		return nil
	}

	// Get email before deleting
	creds, _ := cli.LoadCredentials()
	email := ""
	if creds != nil {
		email = creds.Email
	}

	if err := cli.DeleteCredentials(); err != nil {
		return fmt.Errorf("failed to log out: %w", err)
	}

	if email != "" {
		fmt.Printf("Logged out from %s\n", email)
	} else {
		fmt.Println("Logged out")
	}
	return nil
}
