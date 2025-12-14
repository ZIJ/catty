# Catty

Run Claude Code sessions remotely.

## Installation

```bash
npm install -g @izalutski/catty
```

Or use directly with npx:

```bash
npx @izalutski/catty new
```

## Usage

### Start a new Claude Code session

```bash
catty new
```

This will:
1. Upload your current directory to a remote machine
2. Start Claude Code in the remote environment
3. Connect you to an interactive terminal session

### Options

```bash
catty new                    # Start Claude Code (default)
catty new --no-upload        # Don't upload current directory
catty list                   # List active sessions
catty stop <session-id>      # Stop a session
```

## Requirements

- Node.js 16+
- An Anthropic API key (set as `ANTHROPIC_API_KEY` environment variable)

## How it works

Catty creates isolated machines on-demand, uploads your workspace, and streams the terminal to you. Each session is isolated and secure.

## License

MIT
