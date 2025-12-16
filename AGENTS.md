# Catty - Remote Agent Streaming

Runs Claude Code sessions remotely, streaming PTY to the user so it feels like working with a local agent.

## Instructions for AI Agents

When instructed to "add a task" or "add to roadmap":
1. Add a brief bullet point to the **Roadmap** section in `README.md`
2. Add a more detailed subsection under **Roadmap** in this file (`AGENTS.md`) with implementation notes

Commit messages:
- **ONE-LINERS ONLY** - no extended descriptions, no multi-line messages, no co-author footers
- Example: `Fix upload timeout, add progress indicators to roadmap`
- Bad: Multi-paragraph commit messages with "Generated with Claude" footers

---

## Project Status

**Milestone 1: COMPLETE**

The following is implemented and working:
- `catty` CLI distributed via npm (`@izalutski/catty`)
- `catty-api` hosted in the cloud (no local server needed)
- `catty-exec-runtime` executor that runs inside cloud machines
- Claude Code integration with automatic API key approval
- WebSocket-based PTY streaming with local terminal feel
- **Workspace sync**: Automatically uploads current directory to remote session
- **User authentication**: WorkOS-based login via device flow
- **Custom domains**: `api.catty.dev` and `exec.catty.dev`
- **PostgreSQL storage**: Sessions and users persisted in database
- **Session labels**: Memorable labels like "brave-tiger-1234" for easy identification

**In Progress:**
- Session reconnect (`catty connect <label>`) - DB done, reconnect buggy

**Token Counting/Metering: COMPLETE**
- `catty-proxy` routes all Claude API calls through a metering proxy
- Token counting for both streaming (SSE) and non-streaming responses
- Per-user quota checking before forwarding requests
- Usage records stored in PostgreSQL (`usage` table)

**Stripe Billing: COMPLETE**
- Stripe Checkout integration for upgrading to Pro subscription
- Webhook handling for subscription lifecycle events
- CLI displays friendly paywall message and opens browser to checkout
- Success/cancel pages served by API after checkout

**Next Up:**
- Session reconnect debugging
- Progress indicators for uploads

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
catty login                  # Authenticate (required once)
catty logout                 # Remove stored credentials
catty new                    # Start Claude Code, uploads current directory
catty new --agent codex      # Use Codex instead (experimental, not pre-installed)
catty new --no-upload        # Don't upload current directory
catty connect <label>        # Reconnect to existing session (WIP)
catty list                   # List sessions (shows labels, status)
catty stop <label>           # Stop a session by label
catty version                # Print version number
```

### For Development (Local API)

If running the API locally:
```bash
# Terminal 1 - Start local API server
export FLY_API_TOKEN=...
export ANTHROPIC_API_KEY=...
export DATABASE_URL=postgresql://user:password@host:5432/database
export WORKOS_CLIENT_ID=client_...
export WORKOS_API_KEY=sk_...
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
│   (CLI)     │     │  (api.catty.dev)    │     │   (internal)         │
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
       │
       │ Claude API calls (with ANTHROPIC_BASE_URL override)
       ▼
┌──────────────────────────────────────────────────────────────┐
│                   catty-proxy (proxy.catty.dev)              │
│  - Extracts session label from URL path                      │
│  - Looks up session, checks user quota                       │
│  - Forwards to Anthropic API                                 │
│  - Parses SSE responses for token usage                      │
│  - Records usage to PostgreSQL                               │
└──────────────────────────────────────────────────────────────┘
       │
       ▼
┌──────────────────────────────────────────────────────────────┐
│                  Anthropic API (api.anthropic.com)           │
└──────────────────────────────────────────────────────────────┘
```

### Data Flow

1. User runs `catty login` (one-time) → authenticates via WorkOS → token stored locally
2. `catty new` calls hosted API with auth token (`POST /v1/sessions`)
3. API validates token, creates Fly Machine with connect token and command
4. API sets `ANTHROPIC_BASE_URL=https://proxy.catty.dev/s/{label}` on the machine
5. API returns connection details to CLI
6. CLI zips current directory (respecting .gitignore) and uploads to executor via HTTP
7. Executor extracts zip to `/workspace` directory
8. CLI connects directly to machine via WebSocket with:
   - `fly-force-instance-id: <machine_id>` header
   - `Authorization: Bearer <connect_token>` header
9. Executor validates token, spawns PTY in `/workspace`, relays bytes bidirectionally
10. CLI enters raw terminal mode, streams stdin/stdout

### API Call Flow (Billing/Metering)

When Claude Code makes an API call inside the executor:

1. Claude Code sends request to `ANTHROPIC_BASE_URL` (e.g., `https://proxy.catty.dev/s/brave-tiger-1234/v1/messages`)
2. Proxy extracts session label (`brave-tiger-1234`) from URL path
3. Proxy looks up session in PostgreSQL by label → gets `user_id`, `session_id`
4. Proxy checks user's quota (free tier limit)
5. If quota OK, proxy strips `/s/{label}` prefix and forwards to `https://api.anthropic.com/v1/messages`
6. Proxy passes through the real `x-api-key` header (Anthropic API key)
7. Anthropic returns response (streaming SSE or JSON)
8. Proxy intercepts response:
   - **Non-streaming**: Parse JSON, extract `usage.input_tokens` and `usage.output_tokens`
   - **Streaming (SSE)**: Wrap response body, parse events for `message_start` (input_tokens) and `message_delta` (output_tokens)
9. Proxy records usage to PostgreSQL: `(user_id, session_id, input_tokens, output_tokens)`
10. Response flows back to Claude Code

---

## Project Structure

```
catty/
├── cmd/
│   ├── catty/                  # CLI binary
│   │   ├── main.go
│   │   ├── new.go              # 'new' command - start session
│   │   ├── connect.go          # 'connect' command - reconnect to session
│   │   ├── list.go             # 'list' command - list sessions
│   │   ├── stop.go             # 'stop' command - stop session
│   │   ├── stopall.go          # 'stop-all-sessions-dangerously'
│   │   ├── login.go            # 'login' command - authenticate
│   │   ├── logout.go           # 'logout' command - remove credentials
│   │   └── version.go          # 'version' command - print version
│   ├── catty-api/              # API server binary (deployed to Fly)
│   │   └── main.go
│   ├── catty-proxy/            # Anthropic API proxy (metering/billing)
│   │   └── main.go
│   └── catty-exec-runtime/     # Executor (runs in Fly Machine)
│       └── main.go
├── internal/
│   ├── api/                    # API server logic
│   │   ├── server.go           # HTTP server setup and routing
│   │   ├── handlers.go         # Session CRUD handlers
│   │   ├── auth.go             # WorkOS authentication
│   │   └── billing.go          # Stripe checkout/webhook handlers
│   ├── db/                     # Database layer
│   │   ├── postgres.go         # PostgreSQL client (pgx)
│   │   ├── billing.go          # Stripe customer ID helpers
│   │   └── labels.go           # Memorable label generation
│   ├── cli/                    # CLI logic
│   │   ├── client.go           # API client with auth
│   │   ├── run.go              # Session creation and connection
│   │   ├── connect.go          # Reconnect to existing session
│   │   ├── list.go             # List sessions
│   │   ├── terminal.go         # Raw terminal handling
│   │   ├── workspace.go        # Workspace zip creation and upload
│   │   └── auth.go             # Credentials storage
│   ├── executor/               # Executor runtime logic
│   │   ├── server.go           # HTTP/WS server
│   │   ├── pty.go              # PTY management
│   │   └── relay.go            # WebSocket relay
│   ├── fly/                    # Fly Machines API client
│   │   ├── client.go
│   │   └── machines.go
│   ├── proxy/                  # Anthropic API proxy logic
│   │   └── proxy.go            # Reverse proxy with SSE token counting
│   └── protocol/               # Shared types
│       └── messages.go         # WS message types
├── scripts/
│   └── claude-wrapper.sh       # Pre-approves API key before launching claude
├── npm/                        # npm package for CLI distribution
│   ├── package.json
│   ├── scripts/
│   │   ├── install.js          # Downloads platform-specific binary (postinstall)
│   │   └── release.js          # Automated release script
│   └── README.md
├── Dockerfile.exec             # For catty-exec-runtime (executor)
├── Dockerfile.api              # For catty-api
├── Dockerfile.proxy            # For catty-proxy
├── fly.exec.toml               # Fly config for catty-exec (executor)
├── fly.api.toml                # Fly config for catty-api
├── fly.proxy.toml              # Fly config for catty-proxy
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
| `CATTY_API_ADDR` | Override API URL | `https://api.catty.dev` |

**catty-api (hosted on Fly):**
| Variable | Description | Default |
|----------|-------------|---------|
| `FLY_API_TOKEN` | Fly.io API token | Required (set as secret) |
| `FLY_MACHINES_API_BASE` | Machines API URL | `http://_api.internal:4280` |
| `CATTY_EXEC_APP` | Fly app name for executor | `catty-exec` |
| `CATTY_EXEC_HOST` | Hostname for executor WebSocket connections | `exec.catty.dev` |
| `CATTY_API_ADDR` | API listen address | `0.0.0.0:8080` |
| `CATTY_API_HOST` | API hostname for redirect URLs | `api.catty.dev` |
| `CATTY_PROXY_HOST` | Hostname of the billing proxy | `proxy.catty.dev` |
| `ANTHROPIC_API_KEY` | Passed to machines for Claude | Required (set as secret) |
| `WORKOS_CLIENT_ID` | WorkOS application client ID | Required (set as secret) |
| `WORKOS_API_KEY` | WorkOS API key | Required (set as secret) |
| `DATABASE_URL` | PostgreSQL connection string | Required (set as secret) |
| `STRIPE_SECRET_KEY` | Stripe API secret key | Required for billing |
| `STRIPE_WEBHOOK_SECRET` | Stripe webhook signing secret | Required for billing |
| `STRIPE_PRICE_ID` | Stripe Price ID for Pro subscription | Required for billing |

**catty-proxy (hosted on Fly):**
| Variable | Description | Default |
|----------|-------------|---------|
| `ANTHROPIC_API_KEY` | Master API key for Anthropic | Required (set as secret) |
| `DATABASE_URL` | PostgreSQL connection string | Required (set as secret) |
| `CATTY_PROXY_ADDR` | Proxy listen address | `0.0.0.0:8081` |
| `CATTY_DEBUG` | Set to `1` for debug logging | `0` |

**catty-exec-runtime (in Fly Machine):**
| Variable | Description |
|----------|-------------|
| `CONNECT_TOKEN` | Session capability token (set by API) |
| `CATTY_CMD` | Command to run in PTY (set by API) |
| `ANTHROPIC_API_KEY` | For Claude Code (set by API) |
| `ANTHROPIC_BASE_URL` | Points to proxy with session label embedded (set by API) |
| `CATTY_DEBUG` | Set to `1` for debug logging |

---

## Authentication

Catty uses WorkOS for user authentication via OAuth 2.0 Device Authorization Grant (RFC 8628).

### User Flow

1. User runs `catty login`
2. CLI calls API to start device auth flow
3. CLI displays verification URL and code, opens browser automatically
4. User authenticates in browser (email, Google, SSO, etc.)
5. CLI polls for token until authentication completes
6. Credentials are stored in `~/.catty/credentials.json`
7. All subsequent API calls include the access token

### CLI Commands

```bash
catty login     # Authenticate with Catty
catty logout    # Remove stored credentials
```

### Credentials Storage

Credentials are stored locally at `~/.catty/credentials.json`:
```json
{
  "access_token": "...",
  "user_id": "user_...",
  "email": "user@example.com"
}
```

The file has restricted permissions (0600) and is never transmitted except to the API.

### API Endpoints

**Public (no auth required):**
- `POST /v1/auth/device` - Start device authorization flow
- `POST /v1/auth/device/token` - Poll for access token

**Protected (requires `Authorization: Bearer <token>`):**
- All `/v1/sessions/*` endpoints

### WorkOS Setup

1. Create a WorkOS account at https://workos.com
2. Create an application and enable User Management
3. Enable authentication methods (email, Google, etc.)
4. Get your Client ID and API Key
5. Set as Fly secrets:
   ```bash
   fly secrets set WORKOS_CLIENT_ID=client_... -a catty-api
   fly secrets set WORKOS_API_KEY=sk_... -a catty-api
   ```

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
catty new --no-upload
```

---

## Billing Proxy (Token Counting)

The billing proxy intercepts all Anthropic API calls from Claude Code sessions to track token usage per user.

### How It Works

1. **URL Rewriting**: When API creates a session, it sets `ANTHROPIC_BASE_URL=https://proxy.catty.dev/s/{label}` on the executor machine
2. **Path-Based Session ID**: The session label is encoded in the URL path, allowing the proxy to identify which session made the request without requiring additional authentication
3. **Passthrough Authentication**: The real Anthropic API key (`x-api-key` header) is passed through to Anthropic
4. **Response Interception**: The proxy wraps the response body to extract token usage

### SSE Streaming Response Parsing

Claude Code uses streaming (SSE) responses. The proxy handles this by wrapping the response body in `sseUsageReader`:

```go
// sseUsageReader wraps an SSE response body to extract usage information
type sseUsageReader struct {
    reader       io.ReadCloser
    proxy        *Proxy
    session      *db.Session
    buffer       []byte
    inputTokens  int64
    outputTokens int64
}
```

**Token extraction from SSE events:**
- `message_start` event contains `message.usage.input_tokens`
- `message_delta` event contains `usage.output_tokens` (final count)

Example SSE events:
```
event: message_start
data: {"type":"message_start","message":{"usage":{"input_tokens":1234}}}

event: message_delta
data: {"type":"message_delta","usage":{"output_tokens":567}}
```

Usage is recorded when:
- The response body reaches EOF (`Read()` returns `io.EOF`)
- The response body is closed (`Close()` is called)

### Proxy Endpoints

**`/s/{label}/v1/messages`** (and other Anthropic API paths)
- Extracts session label from path
- Looks up session in PostgreSQL
- Checks user quota (returns 402 if exceeded)
- Forwards to `https://api.anthropic.com/v1/messages`
- Records usage to database

**`/healthz`**
- Returns 200 OK for health checks

### Database Schema

```sql
-- Usage records for billing
CREATE TABLE usage (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id),
    session_id UUID NOT NULL REFERENCES sessions(id),
    input_tokens BIGINT NOT NULL,
    output_tokens BIGINT NOT NULL,
    created_at TIMESTAMP DEFAULT NOW()
);

CREATE INDEX idx_usage_user_id ON usage(user_id);
CREATE INDEX idx_usage_session_id ON usage(session_id);
CREATE INDEX idx_usage_created_at ON usage(created_at);
```

### Key Implementation Files

- `internal/proxy/proxy.go` - Main proxy logic, SSE parsing, usage recording
- `internal/db/postgres.go` - `RecordUsage()` and `CheckQuota()` methods
- `cmd/catty-proxy/main.go` - HTTP server setup with chi router
- `internal/api/handlers.go` - Sets `ANTHROPIC_BASE_URL` on executor machines

### Debugging

Enable debug logging:
```bash
fly secrets set CATTY_DEBUG=1 -a catty-proxy
```

Check proxy logs:
```bash
fly logs -a catty-proxy
```

Look for:
- `"received request"` - Incoming request with label and path
- `"proxying request"` - Request being forwarded (includes remaining quota)
- `"recorded usage"` - Usage successfully written to database

---

## Stripe Billing

The API integrates with Stripe to handle subscription billing for Pro users.

### User Flow

1. User runs `catty new` and quota is exceeded
2. API returns HTTP 402 with `{"error":"quota_exceeded","upgrade_url":"..."}`
3. CLI detects 402, calls `/v1/billing/checkout` to get Stripe Checkout URL
4. CLI opens browser to Stripe Checkout
5. User completes payment on Stripe's hosted page
6. Stripe sends webhook to API (`customer.subscription.created`)
7. API updates user's plan to "pro" in database
8. User runs `catty new` again - session creates successfully

### API Endpoints

**`POST /v1/billing/checkout`** (requires auth)
- Creates a Stripe Checkout session for the authenticated user
- Gets or creates a Stripe customer ID for the user
- Returns `{"checkout_url": "https://checkout.stripe.com/..."}`
- Also supports `GET` for direct browser redirect

**`POST /v1/billing/webhook`** (public, verified by signature)
- Handles Stripe webhook events
- Events processed:
  - `checkout.session.completed` - Upgrades user to pro
  - `customer.subscription.created` - Upgrades user to pro (backup for checkout)
  - `customer.subscription.deleted` - Downgrades user to free
  - `customer.subscription.updated` - Updates subscription period dates

**`GET /billing/success`** (public)
- HTML success page shown after successful checkout
- Instructs user to return to terminal

**`GET /billing/cancel`** (public)
- HTML cancel page shown when user cancels checkout
- Confirms no charges were made

### Webhook Signature Verification

Stripe webhooks are verified using the signing secret:

```go
event, err := webhook.ConstructEventWithOptions(payload, sigHeader, h.webhookSecret, webhook.ConstructEventOptions{
    IgnoreAPIVersionMismatch: true,  // Important: SDK may differ from webhook API version
})
```

**Note:** `IgnoreAPIVersionMismatch: true` is required because Stripe webhooks may use a different API version than the Go SDK expects.

### Database Schema

```sql
-- Subscriptions table (extended for Stripe)
CREATE TABLE subscriptions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id),
    plan VARCHAR(50) NOT NULL DEFAULT 'free',           -- 'free' or 'pro'
    stripe_customer_id VARCHAR(255),                     -- cus_xxxxx
    stripe_subscription_id VARCHAR(255),                 -- sub_xxxxx
    current_period_start TIMESTAMP,
    current_period_end TIMESTAMP,
    created_at TIMESTAMP DEFAULT NOW(),
    updated_at TIMESTAMP DEFAULT NOW()
);
```

### Key Implementation Files

- `internal/api/billing.go` - Checkout session creation, webhook handling, HTML pages
- `internal/api/handlers.go` - Quota check before session creation (returns 402)
- `internal/db/billing.go` - Stripe customer ID helpers (`SetStripeCustomerID`, `GetUserByStripeCustomerID`)
- `internal/cli/client.go` - `APIError` type with `IsQuotaExceeded()`, `CreateCheckoutSession()` method
- `internal/cli/run.go` - `handleQuotaExceeded()` function, opens browser to checkout

### CLI Quota Exceeded Handling

When the API returns 402, the CLI:
1. Displays a friendly ASCII box message about quota exceeded
2. Calls the checkout endpoint to get a Stripe Checkout URL
3. Opens the URL in the default browser
4. Returns an error so the user knows to retry after payment

```go
func handleQuotaExceeded(apiErr *APIError, client *APIClient) error {
    fmt.Fprintln(os.Stderr, "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
    fmt.Fprintln(os.Stderr, "  Free tier quota exceeded (1M tokens/month)")
    fmt.Fprintln(os.Stderr, "  Upgrade to Pro for unlimited usage.")
    fmt.Fprintln(os.Stderr, "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

    checkoutURL, err := client.CreateCheckoutSession()
    // ... open browser to checkoutURL
}
```

### Stripe Setup

1. Create a Stripe account and product with monthly price
2. Create a webhook endpoint pointing to `https://api.catty.dev/v1/billing/webhook`
3. Select events: `checkout.session.completed`, `customer.subscription.created`, `customer.subscription.deleted`, `customer.subscription.updated`
4. Set secrets on Fly:
   ```bash
   fly secrets set STRIPE_SECRET_KEY=sk_live_... -a catty-api
   fly secrets set STRIPE_WEBHOOK_SECRET=whsec_... -a catty-api
   fly secrets set STRIPE_PRICE_ID=price_... -a catty-api
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
fly apps create catty-proxy

# Allocate IPs for each app
fly ips allocate-v4 --shared -a catty-exec
fly ips allocate-v6 -a catty-exec
fly ips allocate-v4 --shared -a catty-api
fly ips allocate-v6 -a catty-api
fly ips allocate-v4 --shared -a catty-proxy
fly ips allocate-v6 -a catty-proxy

# Set secrets for API
fly secrets set FLY_API_TOKEN=... -a catty-api
fly secrets set DATABASE_URL=... -a catty-api
fly secrets set WORKOS_CLIENT_ID=... -a catty-api
fly secrets set WORKOS_API_KEY=... -a catty-api
fly secrets set CATTY_PROXY_HOST=proxy.catty.dev -a catty-api

# Set secrets for proxy
fly secrets set ANTHROPIC_API_KEY=... -a catty-proxy
fly secrets set DATABASE_URL=... -a catty-proxy

# Deploy all services
make deploy-api    # or: fly deploy -c fly.api.toml
make deploy-exec   # or: fly deploy -c fly.exec.toml
make deploy-proxy  # or: fly deploy -c fly.proxy.toml
```

### Custom Domain Setup

For each app, get the IPs and configure DNS:

```bash
# Get IPs for an app
fly ips list -a <app-name>
```

Then add DNS records at your provider (e.g., Namecheap):

| Type | Host | Value |
|------|------|-------|
| A | api | `<IPv4 address>` |
| AAAA | api | `<IPv6 address>` |

**Important:** Both A (IPv4) and AAAA (IPv6) records are required for Fly.io certificate validation.

After DNS is configured, add the certificate:

```bash
fly certs add api.catty.dev -a catty-api
fly certs add exec.catty.dev -a catty-exec
fly certs add proxy.catty.dev -a catty-proxy
```

Current domains:
- `api.catty.dev` → catty-api
- `exec.catty.dev` → catty-exec
- `proxy.catty.dev` → catty-proxy

### Updating Services

```bash
# Update executor (catty-exec)
make deploy-exec
# or: fly deploy -c fly.exec.toml

# Update API (catty-api)
make deploy-api
# or: fly deploy -c fly.api.toml

# Update proxy (catty-proxy)
make deploy-proxy
# or: fly deploy -c fly.proxy.toml
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

### Releasing (Automated)

Use the release script from the `npm` directory:

```bash
cd npm
npm run release          # patch release (default)
npm run release:patch    # patch release
npm run release:minor    # minor release
npm run release:major    # major release
```

This automatically:
1. Bumps version in `package.json`
2. Builds macOS binaries via `make release VERSION=x.x.x` (injects version into binary)
3. Creates GitHub release with binaries
4. Publishes to npm

### Manual Release

```bash
# 1. Bump version
cd npm
npm version patch

# 2. Build binaries
cd ..
make release

# 3. Create GitHub release
gh release create v0.x.x dist/* --title "v0.x.x" --notes "Release notes"

# 4. Publish to npm
cd npm
npm publish --access public
```

### How It Works

The npm package uses a postinstall script (`scripts/install.js`) that:
1. Reads the version from `package.json`
2. Detects the user's platform (darwin) and architecture (amd64/arm64)
3. Downloads the matching binary from GitHub releases
4. Places it in `node_modules/@izalutski/catty/bin/catty`
5. The `bin` field in `package.json` creates the `catty` command

### Currently Supported Platforms

- macOS (darwin) - amd64, arm64

Linux and Windows support can be added by updating the `Makefile` release target.

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
  "label": "brave-tiger-1234",
  "machine_id": "...",
  "connect_url": "wss://exec.catty.dev/connect",
  "connect_token": "base64url",
  "headers": {
    "fly-force-instance-id": "..."
  }
}
```

### `GET /v1/sessions`
List sessions for authenticated user (from PostgreSQL).

### `GET /v1/sessions/{id}`
Get session details. Accepts session ID (UUID) or label (e.g., "brave-tiger-1234").

### `POST /v1/sessions/{id}/stop`
Stop a session's machine. Accepts session ID or label. Add `?delete=true` to also delete the machine.

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

### "Not logged in" error
Run `catty login` to authenticate. Credentials are stored in `~/.catty/credentials.json`.

### "missing authorization header" error
The CLI isn't sending the auth token. Check that:
1. You're logged in: `catty login`
2. Credentials file exists: `cat ~/.catty/credentials.json`
3. You're using the latest CLI version (npm may have cached an old binary)

### Login fails with WorkOS error
Check that WorkOS secrets are set on the API:
```bash
fly secrets list -a catty-api
# Should show WORKOS_CLIENT_ID and WORKOS_API_KEY
```

### 502 errors or auth issues after API redeploy
After redeploying `catty-api`, existing login sessions may become invalid. Run `catty logout` then `catty login` to re-authenticate. (TODO: investigate root cause - may be related to token validation or machine state)

---

## Roadmap

### Custom Domain ✓
Custom domains configured:
- `api.catty.dev` - API server
- `exec.catty.dev` - Executor WebSocket connections

CLI default updated to use `api.catty.dev`. WebSocket URLs use `CATTY_EXEC_HOST` env var.

### Persistent Storage (PostgreSQL) ✓
Migrated from in-memory session storage to PostgreSQL:
- **Users table**: id, email, workos_id, created_at
- **Sessions table**: id, user_id, machine_id, label, connect_token, connect_url, region, created_at, ended_at, status
- **Session labels**: Memorable labels (e.g., "brave-tiger-1734") for easy identification
- **Environment**: `DATABASE_URL` secret (standard PostgreSQL connection string)

### Session Reconnect (WIP)
Partially implemented - DB and labels work, but reconnect has bugs:
- **Done**: Sessions stored in PostgreSQL with memorable labels (e.g., "brave-tiger-1234")
- **Done**: `catty list` shows label, status, region, created_at
- **Done**: `catty new` displays label: "Session created: brave-tiger-1234"
- **Done**: API looks up sessions by ID or label, validates ownership
- **TODO**: Fix `catty connect <label>` - currently buggy, needs debugging

### Progress Indicators
Add progress bars for long-running CLI operations (similar to Docker layer pushing):
- **Workspace upload**: Show upload progress with bytes transferred / total, transfer rate
- **Session creation**: Show spinner or status updates while waiting for machine to start
- **Implementation**: Consider using a library like `github.com/schollz/progressbar/v3` or `github.com/vbauerster/mpb`
- Keep output clean - progress should update in place, not spam terminal

### Workspace Sync-Back
Stream file changes from the remote executor back to the local machine in real-time:
- **File watching**: Use `fsnotify` or similar in executor to watch `/workspace` for changes
- **Change protocol**: Send file change events over WebSocket (new text message type)
  - `{"type":"file_change","path":"src/main.go","action":"write","content":"base64..."}`
  - `{"type":"file_change","path":"old.txt","action":"delete"}`
- **CLI handling**: Apply changes to local directory as they arrive
- **Conflict handling**: Decide policy - overwrite local, prompt user, or skip
- **Security**: Validate paths to prevent directory traversal attacks
- **Performance**: Debounce rapid changes, batch small updates, skip large binary files
- **Optional**: Make sync-back opt-in via `--sync` flag to avoid unexpected local changes

### Documentation Site (Mintlify)
Create comprehensive documentation hosted on Mintlify (e.g., `docs.catty.dev`):
- **Getting Started**: Installation, login, first session
- **Commands Reference**: All CLI commands with examples
- **How It Works**: Architecture overview, data flow, security model
- **Configuration**: Environment variables, workspace sync options
- **Troubleshooting**: Common issues and solutions
- **API Reference**: REST endpoints for programmatic access (if needed)

Setup:
1. Create `docs/` directory with Mintlify structure
2. `mint.json` config with navigation, colors, logo
3. Content pages in MDX format
4. Deploy via Mintlify (connects to GitHub repo)
5. Configure `docs.catty.dev` subdomain

### Usage Metering ✓
Token counting is complete:
- **Proxy**: `catty-proxy` intercepts all Anthropic API calls via `ANTHROPIC_BASE_URL` override
- **Path-based routing**: Session label encoded in URL path (`/s/{label}/v1/messages`)
- **SSE parsing**: Handles streaming responses, extracts tokens from `message_start` and `message_delta` events
- **Database**: Usage records stored in PostgreSQL with user_id, session_id, input_tokens, output_tokens
- **Quota checking**: `CheckQuota()` called before each request, returns 402 if exceeded

### Billing (Stripe Integration) ✓
Stripe billing is complete:
- **Stripe Checkout**: Redirects users to Stripe-hosted checkout page for Pro subscription
- **Pricing model**: Free tier (1M tokens/month) + Pro subscription for unlimited
- **Webhook handling**: Handles `checkout.session.completed`, `customer.subscription.created`, `customer.subscription.deleted`, `customer.subscription.updated` events
- **User tier storage**: `plan` field in subscriptions table, `stripe_customer_id` for linking
- **CLI integration**: Friendly paywall message with browser redirect to checkout
- **Success/cancel pages**: API serves HTML pages for post-checkout redirect

### Multi-Key API Pool
Handle load spikes by rotating through multiple Anthropic API keys:
- Store keys in database (PostgreSQL)
- Round-robin or least-recently-used selection when spawning sessions
- Key health tracking (rate limits, errors)
- Admin interface to add/remove keys

### Stop All Sessions Bug
`catty stop-all-sessions-dangerously` only works on the second try. Needs investigation.

### Future Enhancements
- Multiple concurrent sessions per user
- Session timeout warnings
- Linux/Windows CLI support

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
github.com/stripe/stripe-go/v76        # Stripe API client
```
