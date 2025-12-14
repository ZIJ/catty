package cli

import (
	"fmt"
	"os"
	"text/tabwriter"
	"time"
)

// ListOptions are the options for the list command.
type ListOptions struct {
	APIAddr string
}

// List shows all sessions.
func List(opts *ListOptions) error {
	client := NewAPIClient(opts.APIAddr)

	sessions, err := client.ListSessions()
	if err != nil {
		return fmt.Errorf("failed to list sessions: %w", err)
	}

	if len(sessions) == 0 {
		fmt.Println("No sessions found")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "SESSION ID\tMACHINE ID\tREGION\tCREATED")
	for _, s := range sessions {
		age := formatAge(s.CreatedAt)
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", s.SessionID, s.MachineID, s.Region, age)
	}
	w.Flush()

	return nil
}

func formatAge(t time.Time) string {
	d := time.Since(t)
	if d < time.Minute {
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
	return fmt.Sprintf("%dd ago", int(d.Hours()/24))
}
