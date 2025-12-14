# Catty - Remote Agent Streaming

Runs Claude Code sessions remotely, streaming PTY to the user so it feels like working with a local agent.

## Project Status

**Milestone 1: COMPLETE**

The following is implemented and working:
- `catty` CLI distributed via npm (`@izalutski/catty`)
- `catty-api` hosted in the cloud (no local server needed)
- `catty-exec-runtime` executor that runs inside cloud machines
- Claude Code integration with automatic API key approval
- WebSocket-based PTY streaming with local terminal feel
- **Workspace sync**: Automatically uploads current directory to remote session

---

## Quick Start

### Installation

```bash
# Install globally via npm
npm install -g @izalutski/catty

# Or use directly with npx
npx @izalutski/catty new
```

### Usage

```bash
catty new                    # Start Claude Code, uploads current directory
catty new --agent codex      # Use Codex instead
catty new --no-upload        # Don't upload current directory
catty list                   # List active sessions
catty stop <session-id>      # Stop a session
```

### For Development (Local API)

If running the API locally:
```bash
# Terminal 1 - Start local API server
export FLY_API_TOKEN=...
export ANTHROPIC_API_KEY=...
./bin/catty-api

# Terminal 2 - Use local API
catty new --api http://127.0.0.1:4815
```

---

## Architecture

### Components

```
┌─────────────┐     ┌─────────────────────┐     ┌──────────────────────┐
│   catty     │────▶│     catty-api       │────▶│  Fly Machines API    │
│   (CLI)     │     │ (catty-api.fly.dev) │     │   (internal)         │
└──────┬──────┘     └─────────────────────┘     └──────────────────────┘
       │
       │ HTTP (upload) + WebSocket (terminal)
       ▼
┌──────────────────────────────────────────────────────────────┐
│                     Fly Machine (catty-exec)                 │
│  ┌─────────────────────┐    ┌─────────────────────────────┐  │
│  │ catty-exec-runtime  │───▶│  claude-wrapper + claude    │  │
│  │    (WS server)      │    │       (PTY process)         │  │
│  └─────────────────────┘    └─────────────────────────────┘  │
└──────────────────────────────────────────────────────────────┘
```

### Data Flow

1. `catty new` calls hosted API (`POST /v1/sessions` on catty-api.fly.dev)
2. API creates Fly Machine with connect token and command
3. API returns connection details to CLI
4. CLI zips current directory (respecting .gitignore) and uploads to executor via HTTP
5. Executor extracts zip to `/workspace` directory
6. CLI connects directly to machine via WebSocket with:
   - `fly-force-instance-id: <machine_id>` header
   - `Authorization: Bearer <connect_token>` header
7. Executor validates token, spawns PTY in `/workspace`, relays bytes bidirectionally
8. CLI enters raw terminal mode, streams stdin/stdout

---

## Project Structure

```
catty/
├── cmd/
│   ├── catty/                  # CLI binary
│   │   ├── main.go
│   │   ├── new.go              # 'new' command - start session
│   │   ├── list.go             # 'list' command - list sessions
│   │   ├── stop.go             # 'stop' command - stop session
│   │   └── stopall.go          # 'stop-all-sessions-dangerously'
│   ├── catty-api/              # API server binary (deployed to Fly)
│   │   └── main.go
│   └── catty-exec-runtime/     # Executor (runs in Fly Machine)
│       └── main.go
├── internal/
│   ├── api/                    # API server logic
│   │   ├── server.go
│   │   └── handlers.go
│   ├── cli/                    # CLI logic
│   │   ├── run.go              # Session connection logic
│   │   ├── terminal.go         # Raw terminal handling
│   │   └── workspace.go        # Workspace zip creation and upload
│   ├── executor/               # Executor runtime logic
│   │   ├── server.go           # HTTP/WS server
│   │   ├── pty.go              # PTY management
│   │   └── relay.go            # WebSocket relay
│   ├── fly/                    # Fly Machines API client
│   │   ├── client.go
│   │   └── machines.go
│   └── protocol/               # Shared types
│       └── messages.go         # WS message types
├── scripts/
│   └── claude-wrapper.sh       # Pre-approves API key before launching claude
├── npm/                        # npm package for CLI distribution
│   ├── package.json
│   ├── scripts/install.js      # Downloads platform-specific binary
│   └── README.md
├── Dockerfile                  # For catty-exec-runtime
├── Dockerfile.api              # For catty-api
├── fly.toml                    # Fly config for catty-exec
├── fly.api.toml                # Fly config for catty-api
├── Makefile                    # Build and release commands
├── go.mod
└── AGENTS.md                   # This file
```

---

## Configuration

### Environment Variables

**catty CLI:**
| Variable | Description | Default |
|----------|-------------|---------|
| `CATTY_API_ADDR` | Override API URL | `https://catty-api.fly.dev` |
| `ANTHROPIC_API_KEY` | Passed to remote sessions | Required for Claude |

**catty-api (hosted on Fly):**
| Variable | Description | Default |
|----------|-------------|---------|
| `FLY_API_TOKEN` | Fly.io API token | Required (set as secret) |
| `FLY_MACHINES_API_BASE` | Machines API URL | `http://_api.internal:4280` |
| `CATTY_EXEC_APP` | Fly app name for executor | `catty-exec` |
| `CATTY_API_ADDR` | API listen address | `0.0.0.0:8080` |
| `ANTHROPIC_API_KEY` | Passed to machines for Claude | Required (set as secret) |

**catty-exec-runtime (in Fly Machine):**
| Variable | Description |
|----------|-------------|
| `CONNECT_TOKEN` | Session capability token (set by API) |
| `CATTY_CMD` | Command to run in PTY (set by API) |
| `ANTHROPIC_API_KEY` | For Claude Code (set by API) |
| `CATTY_DEBUG` | Set to `1` for debug logging |

---

## Workspace Sync

By default, `catty new` uploads your current working directory to the remote session so Claude can work with your files.

### How it Works

1. CLI creates a zip file of the current directory
2. Respects `.gitignore` patterns (plus default ignores for node_modules, .git, etc.)
3. Uploads via HTTP POST to executor's `/upload` endpoint
4. Executor extracts to `/workspace` directory
5. PTY process starts with `/workspace` as working directory

### Default Ignore Patterns

The following are always ignored:
- `.git`, `.git/**`
- `node_modules`, `node_modules/**`
- `__pycache__`, `*.pyc`
- `.venv`, `venv`
- `.env`
- `.DS_Store`
- `*.log`

Plus all patterns from `.gitignore` if present.

### Upload Limits

- Maximum upload size: 100MB
- Only one upload per session (subsequent uploads return 409 Conflict)

### Disabling Upload

Use `--no-upload` to skip workspace upload:
```bash
./bin/catty new --no-upload
```

---

## Claude Code Integration

### How it Works

Claude Code requires several first-run prompts to be handled:
1. Theme selection (light/dark)
2. Login method selection
3. Directory trust confirmation
4. API key approval

We handle these by pre-configuring `~/.claude.json` in the Docker image and using a wrapper script.

### Pre-configured Settings (Dockerfile)

```dockerfile
# Pre-populate claude.json to skip onboarding prompts
RUN echo '{"numStartups":1,"installMethod":"npm","autoUpdates":false,"hasCompletedOnboarding":true,"lastOnboardingVersion":"1.0.0","projects":{"/":{"allowedTools":[],"hasTrustDialogAccepted":true,"hasClaudeMdExternalIncludesApproved":true},"/workspace":{"allowedTools":[],"hasTrustDialogAccepted":true,"hasClaudeMdExternalIncludesApproved":true}}}' > /root/.claude.json
```

This sets:
- `hasCompletedOnboarding: true` - Skips onboarding wizard
- `projects["/"].hasTrustDialogAccepted: true` - Pre-trusts root directory
- `projects["/workspace"].hasTrustDialogAccepted: true` - Pre-trusts workspace directory
- `numStartups > 0` - Signals this isn't a fresh install

### API Key Auto-Approval (claude-wrapper.sh)

The wrapper script runs before claude and pre-approves the API key:

```bash
#!/bin/sh
# Extract last 20 chars of API key (the suffix Claude uses for tracking)
KEY_SUFFIX=$(echo "$ANTHROPIC_API_KEY" | tail -c 21)

# Add to approved list in claude.json using jq
jq --arg suffix "$KEY_SUFFIX" \
  '.customApiKeyResponses.approved = (.customApiKeyResponses.approved // []) + [$suffix] | .customApiKeyResponses.approved |= unique' \
  /root/.claude.json > /tmp/claude.json && mv /tmp/claude.json /root/.claude.json

exec /usr/local/bin/claude "$@"
```

This adds the API key suffix to `customApiKeyResponses.approved`, which Claude Code checks to skip the "Do you want to use this API key?" prompt.

---

## Docker Image

The executor runs in a Debian-based image (`node:22-bookworm`) with a full set of development tools.

### Why Debian (not Alpine)

Alpine Linux was initially used for smaller image size, but Claude Code needs many tools to function properly:
- Shell tools (bash, ls, cat, grep, find, etc.)
- ripgrep (`rg`) for Claude's Grep tool
- Git for version control
- Build tools for installing packages

Alpine required manually installing each tool, leading to missing dependencies. Debian includes these by default.

### Installed Tools

The Dockerfile installs:
- `build-essential` - gcc, make, etc.
- `curl`, `wget` - HTTP clients
- `git` - Version control
- `jq` - JSON processing (used by wrapper script)
- `ripgrep` - Fast grep (required by Claude Code)
- `vim` - Text editor
- `tree` - Directory visualization
- `procps` - ps, top, etc.
- `openssh-client` - SSH tools
- `zip`, `unzip` - Archive tools

### Image Size

The full Debian image is ~1GB (vs ~200MB for Alpine), but provides a complete development environment.

---

## Deployment

### Initial Setup (One-time)

```bash
# Create the Fly apps
fly apps create catty-exec
fly apps create catty-api

# Allocate shared IPv4 for executor (required for direct WS connections)
fly ips allocate-v4 --shared -a catty-exec

# Set secrets for API (required for creating machines and Claude)
fly secrets set FLY_API_TOKEN=... -a catty-api
fly secrets set ANTHROPIC_API_KEY=... -a catty-api

# Deploy both services
make deploy-api   # or: fly deploy -c fly.api.toml
make deploy-exec  # or: fly deploy
```

### Updating Services

```bash
# Update executor (catty-exec)
make deploy-exec
# or: fly deploy --app catty-exec

# Update API (catty-api)
make deploy-api
# or: fly deploy -c fly.api.toml
```

### Viewing Logs

```bash
# Executor logs
fly logs -a catty-exec

# API logs
fly logs -a catty-api
```

### Getting Current Image

The API automatically fetches the current deployed image from existing machines. It looks for machines with `fly_process_group: app` metadata (set by `fly deploy`).

---

## npm Distribution

The CLI is distributed via npm for easy installation.

### Building Releases

```bash
# Build binaries for all platforms
make release

# This creates:
# dist/catty-darwin-amd64
# dist/catty-darwin-arm64
# dist/catty-linux-amd64
# dist/catty-linux-arm64
# dist/catty-windows-amd64.exe
# dist/catty-windows-arm64.exe
```

### Creating a GitHub Release

1. Tag the release:
   ```bash
   git tag v0.1.0
   git push origin v0.1.0
   ```

2. Upload binaries to GitHub release (manually or via CI)

3. Update version in `npm/package.json` and `npm/scripts/install.js`

### Publishing to npm

```bash
cd npm
npm publish
```

### How It Works

The npm package uses a postinstall script (`scripts/install.js`) that:
1. Detects the user's platform (darwin/linux/windows) and architecture (amd64/arm64)
2. Downloads the matching binary from GitHub releases
3. Places it in `node_modules/catty-cli/bin/catty`
4. The `bin` field in `package.json` creates the `catty` command

---

## WebSocket Protocol

### Binary Frames
- **Client → Server**: Raw stdin bytes
- **Server → Client**: Raw PTY output bytes

### Text Frames (JSON Control Messages)

**Client → Server:**
```json
{"type":"resize","cols":120,"rows":40}
{"type":"signal","name":"SIGINT"}
{"type":"ping"}
```

**Server → Client:**
```json
{"type":"ready"}
{"type":"exit","code":0,"signal":null}
{"type":"pong"}
{"type":"error","message":"..."}
```

### Keepalive
Ping/pong every 25 seconds to prevent idle disconnects.

---

## API Endpoints

### `POST /v1/sessions`

Create a new session.

Request:
```json
{
  "agent": "claude",
  "cmd": ["claude-wrapper"],
  "region": "iad",
  "cpus": 1,
  "memory_mb": 1024,
  "ttl_sec": 7200
}
```

Response:
```json
{
  "session_id": "uuid",
  "machine_id": "...",
  "connect_url": "wss://catty-exec.fly.dev/connect",
  "connect_token": "base64url",
  "headers": {
    "fly-force-instance-id": "..."
  }
}
```

### `GET /v1/sessions`
List sessions from local storage.

### `GET /v1/sessions/{id}`
Get session details.

### `POST /v1/sessions/{id}/stop`
Stop and delete a session's machine.

### `POST /v1/sessions/stop-all`
Stop all machines in the app (dangerous).

---

## Executor Endpoints

These endpoints are on the executor (Fly Machine), not the local API.

### `GET /healthz`
Health check, returns `200 OK`.

### `POST /upload`
Upload workspace zip file. Requires `Authorization: Bearer <token>` header.
- Content-Type: `application/zip`
- Max size: 100MB
- Extracts to `/workspace`
- Returns 409 if already uploaded

### `GET /connect` (WebSocket)
WebSocket connection for PTY streaming. Requires `Authorization: Bearer <token>` header.

---

## Logging

Uses `log/slog` for structured logging.

- **Info level**: Default, shows key events
- **Debug level**: Enable with `CATTY_DEBUG=1`, shows detailed operation info

Example output:
```
time=... level=INFO msg="executor starting" command="[claude-wrapper]"
time=... level=INFO msg="client connected, starting relay"
time=... level=DEBUG msg="creating PTY" command=claude-wrapper anthropic_key_present=true
```

---

## Troubleshooting

### "manifest unknown" error when creating session
The Fly image tag changed after deployment. The API fetches the current image from existing `fly deploy` machines. Make sure at least one machine from `fly deploy` exists.

### Claude Code shows login prompt
The `~/.claude.json` pre-configuration may be missing. Check that:
1. `hasCompletedOnboarding: true` is set
2. `projects["/"].hasTrustDialogAccepted: true` is set

### Claude Code shows API key prompt
The wrapper script should pre-approve the key. Check that:
1. `jq` is installed in the image
2. `ANTHROPIC_API_KEY` is passed to the machine
3. The wrapper is being used (`claude-wrapper` not `claude`)

### Claude Code can't find files or tools
If Claude reports missing tools (ls, grep, rg, etc.) or can't explore directories:
1. Make sure you're using the Debian-based image (`node:22-bookworm`), not Alpine
2. Verify the image was deployed: `fly deploy --app catty-exec`
3. Check that workspace upload succeeded (look for "Workspace uploaded" message)

### Workspace files not appearing
If the upload says successful but Claude doesn't see files:
1. Check fly logs: `fly logs -a catty-exec`
2. Look for "received workspace upload" and "workspace extracted" messages
3. Verify the `/workspace` directory is being used as the working directory

### Connection drops after ~60s idle
Keepalive ping/pong should prevent this. Check that ping messages are being sent every 25 seconds.

---

## Future Milestones

### Milestone 2: Multi-tenancy
- Add user authentication
- Add quotas and billing
- Replace capability tokens with signed JWTs

### Milestone 3: Enhanced Features
- Session resume (reconnect to existing session)
- Download workspace changes back to local
- Multiple concurrent sessions per user
- Session timeout warnings

---

## Key Implementation Notes

### Claude Code Configuration Discovery

Claude Code stores its configuration in several locations:
- `~/.claude.json` - Main config (onboarding state, project trust, API key approvals)
- `~/.claude/settings.json` - User settings
- `~/.claude/` directory - Projects, todos, statsig cache, etc.

Key fields in `~/.claude.json`:
- `numStartups` - Startup counter (>0 signals not a fresh install)
- `hasCompletedOnboarding` - Skip onboarding wizard
- `lastOnboardingVersion` - Version that completed onboarding
- `projects` - Per-directory settings including `hasTrustDialogAccepted`
- `customApiKeyResponses.approved` - Array of approved API key suffixes (last 20 chars)

### API Key Approval Mechanism

Claude Code tracks approved API keys by their suffix (last 20 characters). The wrapper script:
1. Extracts the suffix: `echo "$ANTHROPIC_API_KEY" | tail -c 21`
2. Adds it to `customApiKeyResponses.approved` array using `jq`
3. This bypasses the "Do you want to use this API key?" prompt

### Fly Machine Routing

To route HTTP/WebSocket requests to a specific machine:
- Use header: `fly-force-instance-id: <machine_id>`
- This works for both the upload endpoint and WebSocket connection

### Expect Script Limitations

Initially tried using `expect` to auto-answer Claude's TUI prompts, but:
- Claude Code uses a React-based TUI (Ink) with escape sequences
- `expect` pattern matching doesn't work well with TUI-rendered prompts
- Solution: Pre-configure everything in `~/.claude.json` instead

---

## Dependencies

```
github.com/spf13/cobra v1.8.1          # CLI framework
github.com/go-chi/chi/v5 v5.1.0        # HTTP router
github.com/coder/websocket v1.8.12     # WebSocket
github.com/creack/pty v1.1.21          # PTY handling
golang.org/x/term v0.25.0              # Terminal raw mode
github.com/google/uuid v1.6.0          # UUID generation
```
