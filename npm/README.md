# catty-cli

Run AI agents (Claude Code, Codex) remotely on Fly.io with local terminal feel.

## Installation

```bash
npm install -g catty-cli
```

Or use directly with npx:

```bash
npx catty-cli new
```

## Usage

### Start a new Claude Code session

```bash
catty new
```

This will:
1. Upload your current directory to a remote Fly.io machine
2. Start Claude Code in the remote environment
3. Connect you to an interactive terminal session

### Options

```bash
catty new                    # Start Claude Code (default)
catty new --agent codex      # Start Codex instead
catty new --no-upload        # Don't upload current directory
```

### Other commands

```bash
catty list                   # List active sessions
catty stop <session-id>      # Stop a session
```

## Requirements

- Node.js 16+
- An Anthropic API key (set as `ANTHROPIC_API_KEY` environment variable)

## How it works

Catty creates isolated Fly.io machines on-demand, uploads your workspace, and streams the terminal to you via WebSocket. Each session is isolated and secure.

## License

MIT
