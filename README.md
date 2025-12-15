# Catty

Run Claude Code sessions remotely.

Catty spins up isolated machines on-demand, syncs your local workspace, and gives you a seamless terminal experience - as if Claude Code was running locally.

## Quick Start

```bash
# Install (macOS only for now)
npm install -g @izalutski/catty

# Log in (required once)
catty login

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
catty login                  # Authenticate (required before first use)
catty logout                 # Remove stored credentials
catty new                    # Start Claude Code session (uploads current directory)
catty new --no-upload        # Start without uploading workspace
catty connect <label>        # Reconnect to an existing session (WIP)
catty list                   # List your sessions (shows labels)
catty stop <label>           # Stop a session by label
catty version                # Print version number
```

## Requirements

- macOS (Intel or Apple Silicon)
- Node.js 16+

## What Gets Uploaded

When you run `catty new`, your current directory is zipped and uploaded. The following are automatically excluded:

- `.git/` directory
- `node_modules/`
- Python virtual environments (`.venv`, `venv`)
- `.env` files
- Anything in your `.gitignore`

Maximum upload size: 100MB

## How It Works

1. `catty login` authenticates you via browser (one-time)
2. `catty new` creates an isolated machine
3. Your current directory is zipped (respecting `.gitignore`) and uploaded
4. Claude Code starts with your workspace
5. Terminal I/O is streamed over WebSocket - you interact as if it's local
6. When done, `catty stop` or Ctrl+C terminates the session

## Troubleshooting

**"Not logged in" error**: Run `catty login` first.

**Session won't start**: Check your internet connection and try again. If the problem persists, try `catty logout` then `catty login`.

**Files not appearing**: Check that your workspace is under 100MB and files aren't gitignored.

## Roadmap

- ~~**Custom domain** - Move away from *.fly.dev URLs~~ ✓
- ~~**Persistent storage** - PostgreSQL for sessions and users~~ ✓
- ~~**Usage metering** - Token counting via proxy~~ ✓
- ~~**Stripe billing** - Free tier (1M tokens/month) + Pro subscription for unlimited~~ ✓
- **Session reconnect** - Reconnect to existing sessions via `catty connect <label>` (WIP - DB done, reconnect buggy)
- **Progress indicators** - Progress bars for uploads and other long operations
- **Workspace sync-back** - Stream file changes from remote session back to local
- **Documentation site** - Comprehensive docs with Mintlify
- **Multi-key support** - Pool of API keys for handling load spikes

## Development

See [AGENTS.md](AGENTS.md) for architecture details, deployment instructions, and contribution guidelines.

## License

MIT
