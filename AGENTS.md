# Catty - Remote Agent Streaming

Runs Claude Code (and potentially other agents like Codex) remotely on Fly.io machines, streaming PTY to the user so it feels like working with a local agent.

## Project Status

**Milestone 0: COMPLETE**

The following is implemented and working:
- `catty` CLI for starting and managing sessions
- `catty-api` local API server that creates Fly machines
- `catty-exec-runtime` executor that runs inside Fly machines
- Claude Code integration with automatic API key approval
- WebSocket-based PTY streaming with local terminal feel
- **Workspace sync**: Automatically uploads current directory to remote session

---

## Quick Start

### Prerequisites

1. Fly.io account with `FLY_API_TOKEN` set
2. `ANTHROPIC_API_KEY` for Claude Code
3. The `catty-exec` Fly app deployed (see Deployment section)

### Running

Terminal 1 - Start the API server:
```bash
export FLY_API_TOKEN=...
export ANTHROPIC_API_KEY=...
./bin/catty-api
```

Terminal 2 - Start a new session:
```bash
./bin/catty new                    # Default: Claude Code, uploads current directory
./bin/catty new --agent codex      # Or use Codex
./bin/catty new --no-upload        # Don't upload current directory
```

### Other Commands

```bash
./bin/catty list                                    # List active sessions
./bin/catty stop <session-id>                       # Stop a session
./bin/catty stop-all-sessions-dangerously --yes-i-mean-it  # Stop all sessions
```

---

## Architecture

### Components

```
┌─────────────┐     ┌─────────────┐     ┌──────────────────────┐
│   catty     │────▶│  catty-api  │────▶│  Fly Machines API    │
│   (CLI)     │     │ (localhost) │     │                      │
└──────┬──────┘     └─────────────┘     └──────────────────────┘
       │
       │ WebSocket (direct)
       ▼
┌──────────────────────────────────────────────────────────────┐
│                     Fly Machine                              │
│  ┌─────────────────────┐    ┌─────────────────────────────┐  │
│  │ catty-exec-runtime  │───▶│  claude-wrapper + claude    │  │
│  │    (WS server)      │    │       (PTY process)         │  │
│  └─────────────────────┘    └─────────────────────────────┘  │
└──────────────────────────────────────────────────────────────┘
```

### Data Flow

1. `catty new` calls local API (`POST /v1/sessions`)
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
│   ├── catty-api/              # Local API server binary
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
├── Dockerfile                  # For catty-exec-runtime
├── fly.toml                    # Fly app config
├── go.mod
└── AGENTS.md                   # This file
```

---

## Configuration

### Environment Variables

**catty-api (local):**
| Variable | Description | Default |
|----------|-------------|---------|
| `FLY_API_TOKEN` | Fly.io API token | Required |
| `FLY_MACHINES_API_BASE` | Machines API URL | `https://api.machines.dev` |
| `CATTY_EXEC_APP` | Fly app name for executor | `catty-exec` |
| `CATTY_API_ADDR` | API listen address | `127.0.0.1:4815` |
| `ANTHROPIC_API_KEY` | Passed to machines for Claude | Required for Claude |

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
RUN echo '{"numStartups":1,"installMethod":"npm","autoUpdates":false,"hasCompletedOnboarding":true,"lastOnboardingVersion":"1.0.0","projects":{"/":{"allowedTools":[],"hasTrustDialogAccepted":true,"hasClaudeMdExternalIncludesApproved":true}}}' > /root/.claude.json
```

This sets:
- `hasCompletedOnboarding: true` - Skips onboarding wizard
- `projects["/"].hasTrustDialogAccepted: true` - Pre-trusts root directory
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

## Deployment

### Initial Setup (One-time)

```bash
# Create the Fly app
fly apps create catty-exec

# Allocate shared IPv4 (required for public services via Machines API)
fly ips allocate-v4 --shared -a catty-exec

# Deploy the executor image
fly deploy --app catty-exec
```

### Updating the Executor

```bash
fly deploy --app catty-exec
```

### Getting Current Image

The API automatically fetches the current deployed image from existing machines. It looks for machines with `fly_process_group: app` metadata (set by `fly deploy`).

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

### Connection drops after ~60s idle
Keepalive ping/pong should prevent this. Check that ping messages are being sent every 25 seconds.

---

## Future Milestones

### Milestone 1: Deploy API to Fly
- Switch Machines API to internal endpoint: `http://_api.internal:4280`
- Use machine metadata for session storage instead of local file

### Milestone 2: Multi-tenancy
- Add user authentication
- Add quotas and billing
- Replace capability tokens with signed JWTs

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
