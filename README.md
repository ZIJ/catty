# Catty

Run Claude Code sessions remotely.

Catty spins up isolated machines on-demand, syncs your local workspace, and gives you a seamless terminal experience - as if Claude Code was running locally.

## Quick Start

```bash
# Install
npm install -g @izalutski/catty

# Set your Anthropic API key
export ANTHROPIC_API_KEY=sk-ant-...

# Start a session in your project directory
cd your-project
catty new
```

That's it. Your files are uploaded and Claude Code starts with full access to your codebase.

## Why Catty?

- **No local setup** - Claude Code runs in a pre-configured environment with all the tools it needs
- **Workspace sync** - Your current directory is automatically uploaded so Claude can work with your files
- **Native terminal feel** - Full PTY streaming means colors, vim, interactive prompts all work perfectly
- **Isolated sessions** - Each session runs in its own machine, fully isolated

## Commands

```bash
catty new                    # Start Claude Code session (uploads current directory)
catty new --no-upload        # Start without uploading workspace
catty list                   # List active sessions
catty stop <session-id>      # Stop a session
```

## Requirements

- Node.js 16+
- Anthropic API key (`ANTHROPIC_API_KEY` environment variable)

## How It Works

1. `catty new` creates an isolated machine
2. Your current directory is zipped (respecting `.gitignore`) and uploaded
3. Claude Code starts with your workspace
4. Terminal I/O is streamed over WebSocket - you interact as if it's local
5. When done, `catty stop` terminates the machine

## License

MIT
