#!/usr/bin/expect -f
# Wrapper script for Claude Code that auto-selects options during first run.
# Handles TUI-rendered prompts by matching partial text patterns.

# Remove ANSI escape codes from matching
remove_nulls -d 0

set timeout 3

# Spawn claude with all passed arguments
spawn /usr/local/bin/claude {*}$argv

# Handle various first-run prompts
# TUI frameworks render text with escape codes, so we match key phrases
expect {
    -re "light.*dark" {
        # Theme selection - press Enter for default or send number
        send "\r"
        exp_continue
    }
    -re "login method" {
        # Login method - select option 2 (API key)
        send "2\r"
        exp_continue
    }
    -re "trust the files" {
        # Trust folder prompt - press Enter for Yes (default)
        send "\r"
        exp_continue
    }
    -re "use this API key" {
        # Custom API key detected - select Yes (option 1)
        # TUI uses arrow keys for selection, up arrow moves to "Yes", then Enter
        send "\033\[A\r"
        exp_continue
    }
    -re "Yes, proceed" {
        # Another form of trust prompt - press Enter
        send "\r"
        exp_continue
    }
    timeout {
        # No more prompts detected, hand over to user
    }
    eof {
        # Process ended
        wait
        exit
    }
}

# Hand control to the user for interactive session
interact
